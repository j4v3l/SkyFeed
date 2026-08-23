package state

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

type PublishFunc func(*domain.Snapshot)

type Engine struct {
	current atomic.Pointer[domain.Snapshot]
	mu      sync.Mutex
	now     func() time.Time
	publish PublishFunc

	sequence          uint64
	batch             domain.AircraftBatch
	fetched           time.Time
	receiver          domain.Receiver
	stats             domain.Statistics
	health            domain.Health
	capabilities      domain.Capabilities
	providerChangedAt time.Time
}

func NewEngine(publish PublishFunc) *Engine {
	engine := &Engine{
		now:     time.Now,
		publish: publish,
		batch:   domain.AircraftBatch{Provider: domain.ProviderUnknown},
	}
	engine.current.Store(&domain.Snapshot{
		ActiveProvider: domain.ProviderUnknown,
		Aircraft:       []domain.Aircraft{},
		ByICAO:         map[string]int{},
		Search:         []domain.AircraftKey{},
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
			Receiver: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
			Stats:    domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
		},
	})
	return engine
}

func (engine *Engine) Current() *domain.Snapshot {
	return engine.current.Load()
}

func (engine *Engine) Run(ctx context.Context, upstream source.Set, aircraftPoll, metadataPoll time.Duration) error {
	engine.configureSources(upstream)
	aircraftEnabled := source.Supports(upstream.Aircraft, domain.CapabilityAircraft)
	receiverEnabled := source.Supports(upstream.Receiver, domain.CapabilityReceiver)
	statsEnabled := source.Supports(upstream.Stats, domain.CapabilityStatistics)

	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(2)
	started := 0
	if aircraftEnabled {
		started++
		group.Go(func() error {
			return poll(groupContext, aircraftPoll, func(pollContext context.Context) {
				frame, err := upstream.Aircraft.FetchAircraft(pollContext)
				if err != nil {
					if pollContext.Err() == nil {
						engine.aircraftFailure(err, aircraftPoll)
					}
					return
				}
				engine.applyAircraft(frame, aircraftPoll)
			})
		})
	}
	if receiverEnabled || statsEnabled {
		started++
		group.Go(func() error {
			return poll(groupContext, metadataPoll, func(pollContext context.Context) {
				if receiverEnabled {
					receiverFrame, err := upstream.Receiver.FetchReceiver(pollContext)
					if err != nil {
						if pollContext.Err() == nil {
							engine.receiverFailure(err, metadataPoll)
						}
					} else {
						engine.applyReceiver(receiverFrame, metadataPoll)
					}
				}

				if pollContext.Err() != nil || !statsEnabled {
					return
				}
				statsFrame, err := upstream.Stats.FetchStats(pollContext)
				if err != nil {
					if pollContext.Err() == nil {
						engine.statsFailure(err, metadataPoll)
					}
					return
				}
				engine.applyStats(statsFrame, metadataPoll)
			})
		})
	}
	if started == 0 {
		<-ctx.Done()
		return nil
	}
	return group.Wait()
}

func (engine *Engine) configureSources(upstream source.Set) {
	engine.mu.Lock()
	engine.capabilities = upstream.Capabilities()
	engine.health = domain.Health{
		Aircraft: configuredHealth(upstream.Aircraft, domain.CapabilityAircraft),
		Receiver: configuredHealth(upstream.Receiver, domain.CapabilityReceiver),
		Stats:    configuredHealth(upstream.Stats, domain.CapabilityStatistics),
	}
	snapshot := engine.buildLocked()
	engine.mu.Unlock()
	engine.store(snapshot)
}

func configuredHealth(provider source.Provider, capability domain.Capability) domain.SourceHealth {
	if provider == nil {
		return domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthDisabled}
	}
	health := domain.SourceHealth{Provider: provider.ProviderID(), Status: domain.HealthUnknown}
	if !provider.Capabilities().Supports(capability) {
		health.Status = domain.HealthDisabled
	}
	return health
}

func poll(ctx context.Context, interval time.Duration, operation func(context.Context)) error {
	for {
		operation(ctx)
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (engine *Engine) applyAircraft(frame source.Frame[domain.AircraftBatch], interval time.Duration) {
	engine.mu.Lock()
	engine.batch = frame.Value
	provider := frame.Provider
	if provider == domain.ProviderUnknown || provider == "" {
		provider = engine.batch.Provider
	}
	if provider == domain.ProviderUnknown || provider == "" {
		provider = engine.health.Aircraft.Provider
	}
	previous := engine.batch.Provider
	engine.batch.Provider = provider
	for index := range engine.batch.Aircraft {
		engine.batch.Aircraft[index].Provider = provider
	}
	if previous.Known() && previous != provider {
		engine.providerChangedAt = frame.FetchedAt
		if engine.providerChangedAt.IsZero() {
			engine.providerChangedAt = engine.now()
		}
	} else if engine.providerChangedAt.IsZero() && provider.Known() {
		engine.providerChangedAt = frame.FetchedAt
		if engine.providerChangedAt.IsZero() {
			engine.providerChangedAt = engine.now()
		}
	}
	engine.fetched = frame.FetchedAt
	engine.health.Aircraft = successHealth(engine.health.Aircraft, frame.FetchedAt, frame.Value.GeneratedAt, interval)
	engine.health.Aircraft.Provider = provider
	snapshot := engine.buildLocked()
	engine.mu.Unlock()
	engine.store(snapshot)
}

func (engine *Engine) applyReceiver(frame source.Frame[domain.Receiver], interval time.Duration) {
	engine.mu.Lock()
	engine.receiver = frame.Value
	engine.health.Receiver = successHealth(engine.health.Receiver, frame.FetchedAt, frame.FetchedAt, interval)
	if frame.Provider != domain.ProviderUnknown && frame.Provider != "" {
		engine.health.Receiver.Provider = frame.Provider
	}
	snapshot := engine.buildLocked()
	engine.mu.Unlock()
	engine.store(snapshot)
}

func (engine *Engine) applyStats(frame source.Frame[domain.Statistics], interval time.Duration) {
	engine.mu.Lock()
	engine.stats = frame.Value
	engine.health.Stats = successHealth(engine.health.Stats, frame.FetchedAt, frame.Value.WindowEnd, interval)
	if frame.Provider != domain.ProviderUnknown && frame.Provider != "" {
		engine.health.Stats.Provider = frame.Provider
	}
	snapshot := engine.buildLocked()
	engine.mu.Unlock()
	engine.store(snapshot)
}

func (engine *Engine) aircraftFailure(err error, interval time.Duration) {
	engine.failure(&engine.health.Aircraft, err, interval)
}

func (engine *Engine) receiverFailure(err error, interval time.Duration) {
	engine.failure(&engine.health.Receiver, err, interval)
}

func (engine *Engine) statsFailure(err error, interval time.Duration) {
	engine.failure(&engine.health.Stats, err, interval)
}

func (engine *Engine) failure(target *domain.SourceHealth, err error, interval time.Duration) {
	engine.mu.Lock()
	now := engine.now()
	*target = failureHealth(*target, err, now, interval)
	snapshot := engine.buildLocked()
	engine.mu.Unlock()
	engine.store(snapshot)
}

func (engine *Engine) buildLocked() *domain.Snapshot {
	engine.sequence++
	aircraft := make([]domain.Aircraft, len(engine.batch.Aircraft))
	copy(aircraft, engine.batch.Aircraft)
	if engine.receiver.HasPosition {
		for index := range aircraft {
			if !aircraft[index].HasPosition {
				continue
			}
			aircraft[index].DistanceNM, aircraft[index].BearingDegrees = DistanceBearing(
				engine.receiver.Latitude,
				engine.receiver.Longitude,
				aircraft[index].Latitude,
				aircraft[index].Longitude,
			)
			aircraft[index].HasDistance = true
		}
	}
	sort.Slice(aircraft, func(left, right int) bool {
		return aircraft[left].ICAO < aircraft[right].ICAO
	})
	byICAO := make(map[string]int, len(aircraft))
	search := make([]domain.AircraftKey, 0, len(aircraft))
	for index := range aircraft {
		byICAO[aircraft[index].ICAO] = index
		search = append(search, domain.AircraftKey{
			ICAO:         aircraft[index].ICAO,
			Callsign:     aircraft[index].Callsign,
			Registration: aircraft[index].Registration,
		})
	}
	sort.Slice(search, func(left, right int) bool {
		leftKey := strings.Join([]string{search[left].Callsign, search[left].Registration, search[left].ICAO}, "\x00")
		rightKey := strings.Join([]string{search[right].Callsign, search[right].Registration, search[right].ICAO}, "\x00")
		return leftKey < rightKey
	})
	return &domain.Snapshot{
		Sequence:            engine.sequence,
		ActiveProvider:      engine.batch.Provider,
		ProviderChangedAt:   engine.providerChangedAt,
		Capabilities:        engine.capabilities,
		SourceGeneratedAt:   engine.batch.GeneratedAt,
		FetchedAt:           engine.fetched,
		PublishedAt:         engine.now(),
		Receiver:            engine.receiver,
		Statistics:          engine.stats,
		ReceiverMessages:    engine.batch.Messages,
		MessageCounterValid: engine.batch.MessageCounterValid,
		Aircraft:            aircraft,
		ByICAO:              byICAO,
		Search:              search,
		Health:              engine.health,
	}
}

func (engine *Engine) store(snapshot *domain.Snapshot) {
	engine.current.Store(snapshot)
	if engine.publish != nil {
		engine.publish(snapshot)
	}
}

func successHealth(previous domain.SourceHealth, attemptedAt, generatedAt time.Time, interval time.Duration) domain.SourceHealth {
	status := domain.HealthHealthy
	errorClass := ""
	if !generatedAt.IsZero() {
		skew := attemptedAt.Sub(generatedAt)
		switch {
		case skew < -5*time.Minute:
			status = domain.HealthDegraded
			errorClass = "source_clock_future"
		case skew > staleAfter(interval):
			status = domain.HealthStale
			errorClass = "source_data_stale"
		}
	}
	return domain.SourceHealth{
		Provider:    previous.Provider,
		Status:      status,
		LastAttempt: attemptedAt,
		LastSuccess: attemptedAt,
		ErrorClass:  errorClass,
	}
}

func failureHealth(previous domain.SourceHealth, err error, attemptedAt time.Time, interval time.Duration) domain.SourceHealth {
	previous.LastAttempt = attemptedAt
	previous.ConsecutiveFailures++
	previous.ErrorClass = string(source.ClassifyError(err))
	if previous.LastSuccess.IsZero() {
		if previous.ConsecutiveFailures >= 3 {
			previous.Status = domain.HealthOffline
		} else {
			previous.Status = domain.HealthDegraded
		}
		return previous
	}
	age := attemptedAt.Sub(previous.LastSuccess)
	switch {
	case age >= offlineAfter(interval):
		previous.Status = domain.HealthOffline
	case age >= staleAfter(interval):
		previous.Status = domain.HealthStale
	default:
		previous.Status = domain.HealthDegraded
	}
	return previous
}

func staleAfter(interval time.Duration) time.Duration {
	return max(3*interval, 5*time.Second)
}

func offlineAfter(interval time.Duration) time.Duration {
	return max(15*interval, 30*time.Second)
}
