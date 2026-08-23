package telemetry

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var metricProviders = [...]domain.ProviderID{domain.ProviderReadsb, domain.ProviderAirplanesLive}
var metricCapabilities = [...]domain.Capability{domain.CapabilityAircraft, domain.CapabilityReceiver, domain.CapabilityStatistics}

type sourceMetrics struct {
	requests        atomic.Uint64
	errors          atomic.Uint64
	bytes           atomic.Uint64
	latencyNanos    atomic.Int64
	lastSuccessUnix atomic.Int64
}

type Metrics struct {
	started              time.Time
	sources              [len(metricProviders) * len(metricCapabilities)]sourceMetrics
	sourceSupported      [len(metricProviders) * len(metricCapabilities)]atomic.Bool
	sourceHealth         [len(metricProviders) * len(metricCapabilities)]atomic.Int64
	activeProviders      [len(metricProviders)]atomic.Bool
	aircraft             atomic.Int64
	snapshotAgeNanos     atomic.Int64
	ruleDurationNanos    atomic.Int64
	ruleMatches          atomic.Uint64
	alertEmergencyDepth  atomic.Int64
	alertNormalDepth     atomic.Int64
	alertDrops           atomic.Uint64
	persistenceDepth     atomic.Int64
	enrichmentCache      atomic.Int64
	interactionAckNanos  atomic.Int64
	discordSucceeded     atomic.Uint64
	discordFailed        atomic.Uint64
	discordRetried       atomic.Uint64
	discordDropped       atomic.Uint64
	discordCoalesced     atomic.Uint64
	adsbdbHits           atomic.Uint64
	adsbdbMisses         atomic.Uint64
	adsbdbRequests       atomic.Uint64
	adsbdbFailures       atomic.Uint64
	adsbdbCircuitReject  atomic.Uint64
	adsblolHits          atomic.Uint64
	adsblolMisses        atomic.Uint64
	adsblolRequests      atomic.Uint64
	adsblolFailures      atomic.Uint64
	adsblolCircuitReject atomic.Uint64
	adsblolDrops         atomic.Uint64
	adsblolBatches       atomic.Uint64
	adsblolRouteCache    atomic.Int64
	adsblolAirportCache  atomic.Int64
	sqliteBatchSize      atomic.Int64
	sqliteLatencyNanos   atomic.Int64
	sqliteFailures       atomic.Uint64
}

func NewMetrics(now time.Time) *Metrics { return &Metrics{started: now} }

func (metrics *Metrics) ObserveSource(provider domain.ProviderID, capability domain.Capability, duration time.Duration, bytes int, success bool, at time.Time) {
	index, known := sourceIndex(provider, capability)
	if !known {
		return
	}
	item := &metrics.sources[index]
	item.requests.Add(1)
	item.bytes.Add(uint64(max(bytes, 0)))
	item.latencyNanos.Store(duration.Nanoseconds())
	if success {
		item.lastSuccessUnix.Store(at.Unix())
	} else {
		item.errors.Add(1)
	}
}

func (metrics *Metrics) SetProviderCapabilities(provider domain.ProviderID, capabilities domain.Capabilities) {
	for _, capability := range metricCapabilities {
		if index, known := sourceIndex(provider, capability); known {
			metrics.sourceSupported[index].Store(capabilities.Supports(capability))
		}
	}
}

func (metrics *Metrics) SetActiveAircraftProvider(provider domain.ProviderID) {
	for index, knownProvider := range metricProviders {
		metrics.activeProviders[index].Store(provider == knownProvider)
	}
}

func (metrics *Metrics) SetSourceHealth(health domain.Health) {
	values := [...]struct {
		capability domain.Capability
		health     domain.SourceHealth
	}{
		{capability: domain.CapabilityAircraft, health: health.Aircraft},
		{capability: domain.CapabilityReceiver, health: health.Receiver},
		{capability: domain.CapabilityStatistics, health: health.Stats},
	}
	for _, value := range values {
		for _, provider := range metricProviders {
			if index, known := sourceIndex(provider, value.capability); known {
				metrics.sourceHealth[index].Store(0)
			}
		}
		if index, known := sourceIndex(value.health.Provider, value.capability); known {
			metrics.sourceHealth[index].Store(healthValue(value.health.Status))
		}
	}
}

func (metrics *Metrics) ObserveSnapshot(aircraft int, age time.Duration) {
	metrics.aircraft.Store(int64(aircraft))
	metrics.snapshotAgeNanos.Store(max(age.Nanoseconds(), 0))
}

func (metrics *Metrics) ObserveRules(duration time.Duration, matches int) {
	metrics.ruleDurationNanos.Store(duration.Nanoseconds())
	metrics.ruleMatches.Add(uint64(max(matches, 0)))
}

func (metrics *Metrics) SetQueues(emergency, normal int, drops uint64, persistence, enrichment int) {
	metrics.alertEmergencyDepth.Store(int64(emergency))
	metrics.alertNormalDepth.Store(int64(normal))
	metrics.alertDrops.Store(drops)
	metrics.persistenceDepth.Store(int64(persistence))
	metrics.enrichmentCache.Store(int64(enrichment))
}

func (metrics *Metrics) ObserveInteraction(duration time.Duration) {
	metrics.interactionAckNanos.Store(duration.Nanoseconds())
}

func (metrics *Metrics) SetDiscord(succeeded, failed, retried, dropped, coalesced uint64) {
	metrics.discordSucceeded.Store(succeeded)
	metrics.discordFailed.Store(failed)
	metrics.discordRetried.Store(retried)
	metrics.discordDropped.Store(dropped)
	metrics.discordCoalesced.Store(coalesced)
}

func (metrics *Metrics) SetEnrichment(hits, misses, requests, failures, circuitRejects uint64) {
	metrics.adsbdbHits.Store(hits)
	metrics.adsbdbMisses.Store(misses)
	metrics.adsbdbRequests.Store(requests)
	metrics.adsbdbFailures.Store(failures)
	metrics.adsbdbCircuitReject.Store(circuitRejects)
}

func (metrics *Metrics) SetRouteEnrichment(hits, misses, requests, failures, circuitRejects, drops, batches uint64, routeEntries, airportEntries int) {
	metrics.adsblolHits.Store(hits)
	metrics.adsblolMisses.Store(misses)
	metrics.adsblolRequests.Store(requests)
	metrics.adsblolFailures.Store(failures)
	metrics.adsblolCircuitReject.Store(circuitRejects)
	metrics.adsblolDrops.Store(drops)
	metrics.adsblolBatches.Store(batches)
	metrics.adsblolRouteCache.Store(int64(routeEntries))
	metrics.adsblolAirportCache.Store(int64(airportEntries))
}

func (metrics *Metrics) SetSQLite(batchSize int, latency time.Duration, failures uint64) {
	metrics.sqliteBatchSize.Store(int64(batchSize))
	metrics.sqliteLatencyNanos.Store(latency.Nanoseconds())
	metrics.sqliteFailures.Store(failures)
}

func (metrics *Metrics) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	for providerIndex, provider := range metricProviders {
		active := 0
		if metrics.activeProviders[providerIndex].Load() {
			active = 1
		}
		_, _ = fmt.Fprintf(writer, "skyfeed_aircraft_provider_active{provider=\"%s\"} %d\n", provider, active)
		for _, capability := range metricCapabilities {
			index, _ := sourceIndex(provider, capability)
			item := &metrics.sources[index]
			supported := 0
			if metrics.sourceSupported[index].Load() {
				supported = 1
			}
			_, _ = fmt.Fprintf(writer, "skyfeed_source_capability_supported{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, supported)
			_, _ = fmt.Fprintf(writer, "skyfeed_source_health{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, metrics.sourceHealth[index].Load())
			_, _ = fmt.Fprintf(writer, "skyfeed_source_requests_total{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, item.requests.Load())
			_, _ = fmt.Fprintf(writer, "skyfeed_source_errors_total{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, item.errors.Load())
			_, _ = fmt.Fprintf(writer, "skyfeed_source_payload_bytes_total{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, item.bytes.Load())
			_, _ = fmt.Fprintf(writer, "skyfeed_source_request_duration_seconds{provider=\"%s\",capability=\"%s\"} %.6f\n", provider, capability, float64(item.latencyNanos.Load())/float64(time.Second))
			_, _ = fmt.Fprintf(writer, "skyfeed_source_last_success_timestamp_seconds{provider=\"%s\",capability=\"%s\"} %d\n", provider, capability, item.lastSuccessUnix.Load())
		}
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	_, _ = fmt.Fprintf(writer, "skyfeed_snapshot_aircraft %d\n", metrics.aircraft.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_snapshot_age_seconds %.6f\n", float64(metrics.snapshotAgeNanos.Load())/float64(time.Second))
	_, _ = fmt.Fprintf(writer, "skyfeed_rule_evaluation_duration_seconds %.6f\n", float64(metrics.ruleDurationNanos.Load())/float64(time.Second))
	_, _ = fmt.Fprintf(writer, "skyfeed_rule_matches_total %d\n", metrics.ruleMatches.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_alert_queue_depth{priority=\"emergency\"} %d\n", metrics.alertEmergencyDepth.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_alert_queue_depth{priority=\"normal\"} %d\n", metrics.alertNormalDepth.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_alert_queue_drops_total %d\n", metrics.alertDrops.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_persistence_queue_depth %d\n", metrics.persistenceDepth.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_enrichment_cache_entries %d\n", metrics.enrichmentCache.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_interaction_acknowledge_duration_seconds %.6f\n", float64(metrics.interactionAckNanos.Load())/float64(time.Second))
	_, _ = fmt.Fprintf(writer, "skyfeed_discord_requests_total{result=\"success\"} %d\n", metrics.discordSucceeded.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_discord_requests_total{result=\"failure\"} %d\n", metrics.discordFailed.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_discord_retries_total %d\n", metrics.discordRetried.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_discord_queue_drops_total %d\n", metrics.discordDropped.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_discord_queue_coalesces_total %d\n", metrics.discordCoalesced.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsbdb_cache_total{result=\"hit\"} %d\n", metrics.adsbdbHits.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsbdb_cache_total{result=\"miss\"} %d\n", metrics.adsbdbMisses.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsbdb_requests_total %d\n", metrics.adsbdbRequests.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsbdb_failures_total %d\n", metrics.adsbdbFailures.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsbdb_circuit_rejects_total %d\n", metrics.adsbdbCircuitReject.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_cache_total{result=\"hit\"} %d\n", metrics.adsblolHits.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_cache_total{result=\"miss\"} %d\n", metrics.adsblolMisses.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_requests_total %d\n", metrics.adsblolRequests.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_failures_total %d\n", metrics.adsblolFailures.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_circuit_rejects_total %d\n", metrics.adsblolCircuitReject.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_queue_drops_total %d\n", metrics.adsblolDrops.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_batches_total %d\n", metrics.adsblolBatches.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_cache_entries{kind=\"route\"} %d\n", metrics.adsblolRouteCache.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_adsblol_cache_entries{kind=\"airport\"} %d\n", metrics.adsblolAirportCache.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_sqlite_batch_size %d\n", metrics.sqliteBatchSize.Load())
	_, _ = fmt.Fprintf(writer, "skyfeed_sqlite_batch_duration_seconds %.6f\n", float64(metrics.sqliteLatencyNanos.Load())/float64(time.Second))
	_, _ = fmt.Fprintf(writer, "skyfeed_sqlite_failures_total %d\n", metrics.sqliteFailures.Load())
	_, _ = fmt.Fprintf(writer, "go_goroutines %d\n", runtime.NumGoroutine())
	_, _ = fmt.Fprintf(writer, "go_memstats_alloc_bytes %d\n", memory.Alloc)
	if cpu, ok := processCPUSeconds(); ok {
		_, _ = fmt.Fprintf(writer, "process_cpu_seconds_total %.6f\n", cpu)
	}
	if descriptors, ok := processOpenFileDescriptors(); ok {
		_, _ = fmt.Fprintf(writer, "process_open_fds %d\n", descriptors)
	}
	_, _ = fmt.Fprintf(writer, "process_start_time_seconds %d\n", metrics.started.Unix())
}

func processCPUSeconds() (float64, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}
	seconds := func(value syscall.Timeval) float64 {
		return float64(value.Sec) + float64(value.Usec)/1_000_000
	}
	return seconds(usage.Utime) + seconds(usage.Stime), true
}

func processOpenFileDescriptors() (int, bool) {
	for _, directory := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(directory)
		if err == nil {
			return len(entries), true
		}
	}
	return 0, false
}

func sourceIndex(provider domain.ProviderID, capability domain.Capability) (int, bool) {
	providerIndex := -1
	for index, known := range metricProviders {
		if provider == known {
			providerIndex = index
			break
		}
	}
	if providerIndex < 0 {
		return 0, false
	}
	for index, known := range metricCapabilities {
		if capability == known {
			return providerIndex*len(metricCapabilities) + index, true
		}
	}
	return 0, false
}

func healthValue(status domain.HealthStatus) int64 {
	switch status {
	case domain.HealthHealthy:
		return 1
	case domain.HealthDegraded:
		return 2
	case domain.HealthStale:
		return 3
	case domain.HealthOffline:
		return 4
	case domain.HealthDisabled:
		return 5
	default:
		return 0
	}
}
