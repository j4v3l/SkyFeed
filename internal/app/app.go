package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
	skydiscord "github.com/j4v3l/SkyFeed/internal/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/enrichment/adsbdb"
	"github.com/j4v3l/SkyFeed/internal/enrichment/adsblol"
	"github.com/j4v3l/SkyFeed/internal/health"
	"github.com/j4v3l/SkyFeed/internal/planealert"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/report"
	"github.com/j4v3l/SkyFeed/internal/rules"
	"github.com/j4v3l/SkyFeed/internal/source/airplaneslive"
	"github.com/j4v3l/SkyFeed/internal/source/readsb"
	"github.com/j4v3l/SkyFeed/internal/state"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
	"github.com/j4v3l/SkyFeed/internal/telemetry"
	"github.com/j4v3l/SkyFeed/internal/weather/aviationweather"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	startedAt := time.Now()
	healthState := health.NewState(startedAt)
	healthState.SetPrivacyDisclosure(privacyDisclosure(cfg))
	healthState.SetComponent("bootstrap", "healthy", "application initialized")
	healthState.SetReady(false)
	server := health.NewServer(cfg.HealthAddr, healthState, logger)
	metrics := telemetry.NewMetrics(startedAt)
	server.SetMetrics(metrics)
	var sourceKnown atomic.Bool
	var discordReady atomic.Bool
	var databaseReady atomic.Bool
	databaseReady.Store(cfg.DatabasePath == "")
	if cfg.Discord.ApplicationID == 0 {
		discordReady.Store(true)
		healthState.SetComponent("discord", "disabled", "not configured in this process")
	} else {
		healthState.SetComponent("discord", "starting", "Gateway connection not ready")
	}
	setReadiness := func() { healthState.SetReady(sourceKnown.Load() && discordReady.Load() && databaseReady.Load()) }

	var repository *sqlite.Store
	var persistence *storage.Writer
	var ruleEngine *rules.Engine
	if cfg.DatabasePath != "" {
		var err error
		repository, err = sqlite.Open(ctx, cfg.DatabasePath)
		if err != nil {
			return err
		}
		defer repository.Close()
		databaseReady.Store(true)
		healthState.SetComponent("database", "healthy", "SQLite initialized")
		if cfg.Discord.GuildID != 0 {
			if err := repository.EnsureGuild(ctx, cfg.Discord.GuildID); err != nil {
				return err
			}
			bootstrapRoleBindings(ctx, repository, cfg.Discord, logger)
		}
		persistence = storage.NewWriter(repository, 1_024, 64, 250*time.Millisecond)
		storedRules, err := repository.AllWatchRules(ctx, cfg.Discord.GuildID, 500)
		if err != nil {
			return err
		}
		storedStates, err := repository.AlertStates(ctx, 10_000)
		if err != nil {
			return err
		}
		ruleEngine = rules.NewEngine(storedRules, storedStates)
	} else {
		healthState.SetComponent("database", "disabled", "not configured in this process")
		ruleEngine = rules.NewEngine(nil, nil)
	}
	alertQueue := rules.NewQueue(64, 512)
	feederMonitor := rules.NewFeederMonitor()
	if repository != nil {
		recent, err := repository.RecentFeederEvents(ctx, cfg.Discord.GuildID, 20)
		if err != nil {
			return err
		}
		fingerprints := make([]string, 0, len(recent))
		for _, event := range recent {
			fingerprints = append(fingerprints, event.Kind)
		}
		feederMonitor.Restore(fingerprints)
	}
	reportAggregator := report.NewAggregator()
	var planeAlertIndex *planealert.Index
	interestingMonitor := rules.NewInterestingMonitor(nil)
	if cfg.PlaneAlert.Enabled && repository != nil {
		planeAlertURL := cfg.PlaneAlert.URL
		if planeAlertURL == "" {
			planeAlertURL = planealert.DefaultCSVURL
		}
		planeAlertIndex = planealert.NewIndex(planealert.NewLoader(planeAlertURL, cfg.PlaneAlert.Timeout), repository, cfg.PlaneAlert.Refresh, logger)
		interestingMonitor = rules.NewInterestingMonitor(planeAlertIndex.Lookup)
		restoreContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		seenICAOs, restoreErr := repository.InterestingSeenICAOs(restoreContext, cfg.Discord.GuildID)
		cancel()
		if restoreErr != nil {
			return restoreErr
		}
		interestingMonitor.Restore(seenICAOs)
		healthState.SetComponent("planealert", "healthy", "plane-alert-db interesting aircraft matching enabled")
	} else if !cfg.PlaneAlert.Enabled {
		healthState.SetComponent("planealert", "disabled", "interesting aircraft matching disabled")
	} else {
		healthState.SetComponent("planealert", "disabled", "database required for interesting aircraft matching")
	}
	var lastAircraftFetch atomic.Int64
	var enrichmentService *enrichment.Service
	var routeService *enrichment.RouteService
	if cfg.ADSBDB.Enabled {
		enrichmentConfig := enrichment.DefaultConfig()
		enrichmentConfig.Workers = cfg.ADSBDB.Workers
		enrichmentConfig.RouteEnabled = cfg.ADSBDB.RouteEnabled
		enrichmentConfig.AircraftTTL = cfg.ADSBDB.AircraftTTL
		enrichmentConfig.RouteTTL = cfg.ADSBDB.RouteTTL
		enrichmentConfig.NotFoundTTL = cfg.ADSBDB.NotFoundTTL
		enrichmentConfig.ErrorTTL = cfg.ADSBDB.ErrorTTL
		enrichmentConfig.StaleTTL = cfg.ADSBDB.StaleTTL
		enrichmentService = enrichment.NewService(adsbdb.NewClient(cfg.ADSBDB.BaseURL, cfg.ADSBDB.Timeout), enrichmentConfig)
		healthState.SetComponent("adsbdb", "healthy", "asynchronous enrichment enabled")
	} else {
		healthState.SetComponent("adsbdb", "disabled", "enrichment disabled")
	}
	if cfg.AdsbLol.Enabled {
		adsbClient, err := adsblol.NewClient(cfg.AdsbLol.BaseURL, cfg.AdsbLol.Timeout)
		if err != nil {
			return err
		}
		routeService = enrichment.NewRouteService(adsbClient, enrichment.DefaultRouteConfig())
		healthState.SetComponent("adsblol", "healthy", "route and airport enrichment enabled")
	} else {
		healthState.SetComponent("adsblol", "disabled", "route enrichment disabled")
	}
	lastCallsigns := make(map[string]string)
	var lastCallsignsMu sync.Mutex
	engine := state.NewEngine(func(snapshot *domain.Snapshot) {
		age := time.Duration(0)
		if !snapshot.FetchedAt.IsZero() {
			age = time.Since(snapshot.FetchedAt)
		}
		metrics.ObserveSnapshot(len(snapshot.Aircraft), age)
		metrics.SetActiveAircraftProvider(snapshot.ActiveProvider)
		metrics.SetSourceHealth(snapshot.Health)
		healthState.SetComponent("aircraft_source", string(snapshot.Health.Aircraft.Status), sourceHealthMessage(snapshot.Health.Aircraft))
		healthState.SetComponent("receiver_source", string(snapshot.Health.Receiver.Status), sourceHealthMessage(snapshot.Health.Receiver))
		healthState.SetComponent("stats_source", string(snapshot.Health.Stats.Status), sourceHealthMessage(snapshot.Health.Stats))
		sourceKnown.Store(sourcesInitialized(snapshot.Health))
		setReadiness()
		for _, alert := range feederMonitor.Evaluate(cfg.Discord.GuildID, snapshot) {
			enqueueContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			if err := alertQueue.Enqueue(enqueueContext, alert); err != nil {
				logger.Error("feeder alert queue admission failed", "component", "rules", "event", "alert_drop", "priority", alert.Priority, "error", err)
			}
			cancel()
			if persistence != nil {
				_ = persistence.Enqueue(storage.WriteEvent{Kind: storage.WriteFeederEvent, Feeder: storage.FeederEvent{GuildID: cfg.Discord.GuildID, Kind: alert.ConditionFingerprint, Status: alert.Title, Detail: alert.Description, Occurred: alert.ObservedAt}})
			}
		}
		fetched := snapshot.FetchedAt.UnixNano()
		if fetched == 0 || lastAircraftFetch.Swap(fetched) == fetched {
			return
		}
		rulesStarted := time.Now()
		alerts, stateUpdates := ruleEngine.Evaluate(snapshot)
		if interestingMonitor != nil && planeAlertIndex != nil {
			for _, alert := range interestingMonitor.Evaluate(cfg.Discord.GuildID, snapshot) {
				alerts = append(alerts, alert)
				if persistence != nil {
					if err := persistence.Enqueue(storage.WriteEvent{Kind: storage.WriteInterestingSeen, Interesting: storage.InterestingSeen{
						GuildID:     cfg.Discord.GuildID,
						ICAO:        alert.AircraftICAO,
						FirstSeenAt: alert.ObservedAt,
						Callsign:    alert.Callsign,
						FlightGroup: alert.InterestingGroup,
					}}); err != nil {
						logger.Error("interesting seen persistence failed", "component", "storage", "event", "write_drop", "error", err)
					}
				}
			}
		}
		metrics.ObserveRules(time.Since(rulesStarted), len(alerts))
		for _, alert := range alerts {
			if alert.Priority == domain.AlertEmergency && alert.GuildID == 0 {
				alert.GuildID = cfg.Discord.GuildID
			}
			enqueueContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			if err := alertQueue.Enqueue(enqueueContext, alert); err != nil {
				logger.Error("alert queue admission failed", "component", "rules", "event", "alert_drop", "priority", alert.Priority, "error", err)
			}
			cancel()
		}
		if persistence != nil {
			for _, update := range stateUpdates {
				if err := persistence.Enqueue(storage.WriteEvent{Kind: storage.WriteAlertState, AlertState: update}); err != nil {
					logger.Error("persistence queue admission failed", "component", "storage", "event", "write_drop", "kind", "alert_state", "error", err)
				}
			}
			if err := persistence.Enqueue(storage.WriteEvent{Kind: storage.WriteReportRollup, Rollup: reportAggregator.Observe(cfg.Discord.GuildID, snapshot)}); err != nil {
				logger.Error("persistence queue admission failed", "component", "storage", "event", "write_drop", "kind", "report_rollup", "error", err)
			}
		}
		if enrichmentService != nil {
			lastCallsignsMu.Lock()
			nextCallsigns := make(map[string]string, len(snapshot.Aircraft))
			for _, aircraft := range snapshot.Aircraft {
				if previous, exists := lastCallsigns[aircraft.ICAO]; !exists || previous != aircraft.Callsign {
					enrichmentService.Enqueue(aircraft.ICAO, aircraft.Callsign)
				}
				nextCallsigns[aircraft.ICAO] = aircraft.Callsign
			}
			lastCallsigns = nextCallsigns
			lastCallsignsMu.Unlock()
		}
		if routeService != nil {
			routeService.Prefetch(snapshot.Aircraft)
		}
		emergencyDepth, normalDepth := alertQueue.Depth()
		persistenceDepth, enrichmentCache := 0, 0
		if persistence != nil {
			persistenceDepth = persistence.Depth()
			writerStats := persistence.Stats()
			metrics.SetSQLite(writerStats.LastSize, writerStats.Latency, writerStats.Failed)
		}
		if enrichmentService != nil {
			enrichmentCache = enrichmentService.CacheLen()
			enrichmentStats := enrichmentService.Stats()
			metrics.SetEnrichment(enrichmentStats.Hits, enrichmentStats.Misses, enrichmentStats.Requests, enrichmentStats.Failures, enrichmentStats.CircuitRejects)
		}
		if routeService != nil {
			routeStats := routeService.Stats()
			metrics.SetRouteEnrichment(
				routeStats.Hits,
				routeStats.Misses,
				routeStats.Requests,
				routeStats.Failures,
				routeStats.CircuitRejects,
				routeStats.Dropped,
				routeStats.Batches,
				routeService.RouteCacheLen(),
				routeService.AirportCacheLen(),
			)
			enrichmentCache += routeService.RouteCacheLen() + routeService.AirportCacheLen()
		}
		metrics.SetQueues(emergencyDepth, normalDepth, alertQueue.Dropped(), persistenceDepth, enrichmentCache)
	})
	upstreams, sourceErr := configureSources(cfg)
	if sourceErr != nil {
		return sourceErr
	}
	for _, provider := range upstreams.AircraftChecks {
		metrics.SetProviderCapabilities(provider.ProviderID(), provider.Capabilities())
	}
	upstreams.Readsb.SetObserver(func(observation readsb.Observation) {
		metrics.ObserveSource(observation.Provider, observation.Capability, observation.Duration, observation.Bytes, observation.Success, observation.At)
	})
	if upstreams.AirplanesLive != nil {
		upstreams.AirplanesLive.SetObserver(func(observation airplaneslive.Observation) {
			metrics.ObserveSource(observation.Provider, observation.Capability, observation.Duration, observation.Bytes, observation.Success, observation.At)
		})
	}
	sessions := skydiscord.NewSessionManager(2_000, 20, 15*time.Minute)
	router := skydiscord.NewRouter(engine, sessions, cfg.Discord.GuildID, startedAt)
	router.SetPrivacyDisclosure(privacyDisclosure(cfg))
	ruleReload := make(chan struct{}, 1)
	if repository != nil {
		router.SetRepository(repository)
		router.SetRuleReload(func() {
			select {
			case ruleReload <- struct{}{}:
			default:
			}
		})
	}
	if enrichmentService != nil {
		router.SetEnrichment(enrichmentService)
		enrichmentService.SetObserver(func(value domain.Enrichment, lookupErr error) {
			if lookupErr != nil && !errors.Is(lookupErr, enrichment.ErrNotFound) {
				return
			}
			snapshot := engine.Current()
			aircraft, visible := snapshot.LookupICAO(value.ICAO)
			if !visible {
				return
			}
			started := time.Now()
			alerts, updates := ruleEngine.EvaluateEnrichment(value, aircraft, time.Now().UTC())
			metrics.ObserveRules(time.Since(started), len(alerts))
			for _, alert := range alerts {
				enqueueContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				if err := alertQueue.Enqueue(enqueueContext, alert); err != nil {
					logger.Error("enrichment alert queue admission failed", "component", "rules", "event", "alert_drop", "error", err)
				}
				cancel()
			}
			if persistence != nil {
				for _, update := range updates {
					if err := persistence.Enqueue(storage.WriteEvent{Kind: storage.WriteAlertState, AlertState: update}); err != nil {
						logger.Error("enrichment rule state persistence failed", "component", "storage", "event", "write_drop", "error", err)
					}
				}
			}
		})
	}
	if routeService != nil {
		router.SetRoutes(routeService)
	}
	weatherClient, weatherErr := aviationweather.NewClient(2 * time.Second)
	if weatherErr != nil {
		return weatherErr
	}
	router.SetWeather(weatherClient)
	healthState.SetComponent("weather", "healthy", "aviationweather.gov METAR/TAF")

	logger.Info("SkyFeed starting", "component", "app", "event", "start", "version", Version)
	services := []service{server.Run, func(serviceContext context.Context) error {
		return engine.Run(serviceContext, upstreams.Set, cfg.ADSB.AircraftPoll, cfg.ADSB.MetadataPoll)
	}, func(serviceContext context.Context) error {
		sessions.RunCleanup(serviceContext.Done(), time.Minute)
		return nil
	}}
	if cfg.PprofAddr != "" {
		services = append(services, func(serviceContext context.Context) error {
			return telemetry.RunPprof(serviceContext, cfg.PprofAddr, logger)
		})
	}
	if persistence != nil {
		services = append(services, persistence.Run, func(serviceContext context.Context) error {
			for {
				select {
				case <-serviceContext.Done():
					return nil
				case <-ruleReload:
					reloadContext, cancel := context.WithTimeout(serviceContext, 2*time.Second)
					storedRules, err := repository.AllWatchRules(reloadContext, cfg.Discord.GuildID, 500)
					cancel()
					if err != nil {
						logger.Error("watch rule reload failed", "component", "rules", "event", "reload_failure", "error", err)
						continue
					}
					ruleEngine.ReplaceRules(storedRules)
				}
			}
		})
	}
	if enrichmentService != nil {
		services = append(services, enrichmentService.Run)
	}
	if routeService != nil {
		services = append(services, routeService.Run)
	}
	if planeAlertIndex != nil {
		services = append(services, planeAlertIndex.Run)
	}
	if cfg.Discord.ApplicationID != 0 {
		gateway := skydiscord.NewGatewayService(cfg.Discord, router, logger, func(ready bool) {
			discordReady.Store(ready)
			if ready {
				healthState.SetComponent("discord", "healthy", "Gateway ready")
			} else {
				healthState.SetComponent("discord", "degraded", "Gateway unavailable")
			}
			setReadiness()
		})
		gateway.SetRepository(repository)
		gateway.SetDashboardInterval(cfg.DashboardInterval)
		gateway.SetInteractionObserver(metrics.ObserveInteraction)
		router.SetTestSender(gateway.SubmitDestinationTest)
		router.SetModeration(gateway)
		router.SetGuildMemberProvider(gateway)
		if repository != nil {
			router.SetDashboardReset(func(resetContext context.Context) error {
				if err := repository.DeleteMessageBinding(resetContext, cfg.Discord.GuildID, "dashboard"); err != nil {
					return err
				}
				gateway.EnqueueDashboard()
				return nil
			})
		}
		services = append(services, gateway.Run, func(serviceContext context.Context) error {
			for {
				alert, ok := alertQueue.Pop(serviceContext)
				if !ok {
					return nil
				}
				submitContext, cancel := context.WithTimeout(serviceContext, 100*time.Millisecond)
				err := gateway.SubmitAlert(submitContext, alert)
				cancel()
				if err != nil {
					logger.Error("Discord alert scheduling failed", "component", "discord", "event", "alert_schedule_failure", "priority", alert.Priority, "error", err)
				}
			}
		}, func(serviceContext context.Context) error {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-serviceContext.Done():
					return nil
				case <-ticker.C:
					stats := gateway.OutboundStats()
					metrics.SetDiscord(stats.Succeeded, stats.Failed, stats.Retried, stats.Dropped, stats.Coalesced)
				}
			}
		})
	}
	err := runServices(ctx, services...)
	healthState.SetReady(false)
	healthState.SetLive(false)
	logger.Info("SkyFeed stopped", "component", "app", "event", "stop")
	return err
}

func sourcesInitialized(value domain.Health) bool {
	return !value.Aircraft.LastSuccess.IsZero() &&
		sourceInitialized(value.Receiver) &&
		sourceInitialized(value.Stats)
}

func sourceInitialized(value domain.SourceHealth) bool {
	return value.Status == domain.HealthDisabled || !value.LastSuccess.IsZero()
}

func sourceHealthMessage(value domain.SourceHealth) string {
	message := string(value.Provider)
	if value.ErrorClass != "" {
		if message != "" {
			message += ": "
		}
		message += value.ErrorClass
	}
	return message
}

func privacyDisclosure(cfg config.Config) privacy.Disclosure {
	providers := []string{"readsb"}
	attribution := []privacy.Attribution{}
	publicAirportCode := ""
	radiusNM := 0
	if airplanesLiveConfigured(cfg) {
		providers = append(providers, "airplanes.live")
		publicAirportCode = cfg.AirplanesLive.PublicAirportCode
		radiusNM = cfg.AirplanesLive.RadiusNM
		attribution = append(attribution, privacy.Attribution{
			Provider: "airplanes.live",
			Notice:   airplaneslive.Attribution,
		})
	}
	if cfg.AdsbLol.Enabled {
		providers = append(providers, "adsb.lol")
		attribution = append(attribution, privacy.Attribution{Provider: "adsb.lol", Notice: adsblol.Attribution})
	}
	if cfg.ADSBDB.Enabled {
		providers = append(providers, "ADSBDB")
		attribution = append(attribution, privacy.Attribution{Provider: "ADSBDB", Notice: "Aircraft and route data provided by ADSBDB"})
	}
	providers = append(providers, "aviationweather.gov")
	attribution = append(attribution, privacy.Attribution{Provider: "aviationweather.gov", Notice: aviationweather.Attribution})
	if cfg.PlaneAlert.Enabled {
		attribution = append(attribution, privacy.Attribution{Provider: "plane-alert-db", Notice: planealert.Attribution})
	}
	return privacy.NewDisclosure(
		providers,
		publicAirportCode,
		radiusNM,
		[]privacy.Retention{
			{Category: "raw aircraft snapshots", Period: "memory only"},
			{Category: "route and airport enrichment", Period: "in-memory TTL cache only"},
			{Category: "plane-alert-db reference", Period: "SQLite reference table, refreshed periodically"},
			{Category: "interesting aircraft sightings", Period: "SQLite, one row per ICAO per guild"},
			{Category: "interaction sessions", Period: "15 minutes"},
			{Category: "moderation cases", Period: "365 days"},
		},
		attribution,
	)
}

func airplanesLiveConfigured(cfg config.Config) bool {
	for _, provider := range cfg.ADSB.ProviderOrder {
		if provider == domain.ProviderAirplanesLive {
			return true
		}
	}
	return false
}
