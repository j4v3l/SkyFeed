package telemetry

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

type sourceMetrics struct {
	requests        atomic.Uint64
	errors          atomic.Uint64
	bytes           atomic.Uint64
	latencyNanos    atomic.Int64
	lastSuccessUnix atomic.Int64
}

type Metrics struct {
	started             time.Time
	sources             [3]sourceMetrics
	aircraft            atomic.Int64
	snapshotAgeNanos    atomic.Int64
	ruleDurationNanos   atomic.Int64
	ruleMatches         atomic.Uint64
	alertEmergencyDepth atomic.Int64
	alertNormalDepth    atomic.Int64
	alertDrops          atomic.Uint64
	persistenceDepth    atomic.Int64
	enrichmentCache     atomic.Int64
	interactionAckNanos atomic.Int64
	discordSucceeded    atomic.Uint64
	discordFailed       atomic.Uint64
	discordRetried      atomic.Uint64
	discordDropped      atomic.Uint64
	discordCoalesced    atomic.Uint64
	adsbdbHits          atomic.Uint64
	adsbdbMisses        atomic.Uint64
	adsbdbRequests      atomic.Uint64
	adsbdbFailures      atomic.Uint64
	adsbdbCircuitReject atomic.Uint64
	sqliteBatchSize     atomic.Int64
	sqliteLatencyNanos  atomic.Int64
	sqliteFailures      atomic.Uint64
}

func NewMetrics(now time.Time) *Metrics { return &Metrics{started: now} }

func (metrics *Metrics) ObserveSource(source string, duration time.Duration, bytes int, success bool, at time.Time) {
	index := sourceIndex(source)
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

func (metrics *Metrics) SetSQLite(batchSize int, latency time.Duration, failures uint64) {
	metrics.sqliteBatchSize.Store(int64(batchSize))
	metrics.sqliteLatencyNanos.Store(latency.Nanoseconds())
	metrics.sqliteFailures.Store(failures)
}

func (metrics *Metrics) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	for index, name := range []string{"aircraft", "receiver", "stats"} {
		item := &metrics.sources[index]
		_, _ = fmt.Fprintf(writer, "skyfeed_source_requests_total{source=\"%s\"} %d\n", name, item.requests.Load())
		_, _ = fmt.Fprintf(writer, "skyfeed_source_errors_total{source=\"%s\"} %d\n", name, item.errors.Load())
		_, _ = fmt.Fprintf(writer, "skyfeed_source_payload_bytes_total{source=\"%s\"} %d\n", name, item.bytes.Load())
		_, _ = fmt.Fprintf(writer, "skyfeed_source_request_duration_seconds{source=\"%s\"} %.6f\n", name, float64(item.latencyNanos.Load())/float64(time.Second))
		_, _ = fmt.Fprintf(writer, "skyfeed_source_last_success_timestamp_seconds{source=\"%s\"} %d\n", name, item.lastSuccessUnix.Load())
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

func sourceIndex(source string) int {
	switch source {
	case "receiver":
		return 1
	case "stats":
		return 2
	default:
		return 0
	}
}
