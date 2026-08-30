package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var (
	ErrWriterFull        = errors.New("persistence queue full")
	ErrWriterUnavailable = errors.New("persistence writer unavailable")
)

type applyResult uint8

const (
	applySucceeded applyResult = iota
	applyRetryLater
	applyPermanentFailure
)

type WriterStats struct {
	Accepted  uint64
	Dropped   uint64
	Batches   uint64
	Written   uint64
	Failed    uint64
	Coalesced uint64
	LastSize  int
	Latency   time.Duration
}

type Writer struct {
	sink        BatchSink
	queue       chan WriteEvent
	batchMax    int
	flush       time.Duration
	rollupFlush time.Duration
	retryBudget time.Duration

	observerMu sync.RWMutex
	observer   func(error)
	degraded   atomic.Bool
	disabled   atomic.Bool

	accepted  atomic.Uint64
	dropped   atomic.Uint64
	batches   atomic.Uint64
	written   atomic.Uint64
	failed    atomic.Uint64
	coalesced atomic.Uint64
	lastSize  atomic.Int64
	latency   atomic.Int64
}

func NewWriter(sink BatchSink, capacity, batchMax int, flush time.Duration) *Writer {
	return &Writer{sink: sink, queue: make(chan WriteEvent, capacity), batchMax: batchMax, flush: flush, rollupFlush: 15 * time.Second, retryBudget: 2 * time.Second}
}

func (writer *Writer) SetObserver(observer func(error)) {
	writer.observerMu.Lock()
	writer.observer = observer
	writer.observerMu.Unlock()
}

func (writer *Writer) Enqueue(event WriteEvent) error {
	if writer.disabled.Load() {
		writer.dropped.Add(1)
		return ErrWriterUnavailable
	}
	select {
	case writer.queue <- event:
		writer.accepted.Add(1)
		return nil
	default:
		writer.dropped.Add(1)
		return ErrWriterFull
	}
}

func (writer *Writer) Run(ctx context.Context) error {
	ticker := time.NewTicker(writer.flush)
	defer ticker.Stop()
	rollupTicker := time.NewTicker(writer.rollupFlush)
	defer rollupTicker.Stop()
	batch := make([]WriteEvent, 0, writer.batchMax)
	rollups := make(map[reportRollupKey]ReportRollup)
	for {
		if !writer.disabled.Load() && (len(batch) >= writer.batchMax || len(rollups) >= writer.batchMax) {
			if len(batch) >= writer.batchMax {
				switch writer.applyUntil(ctx, batch) {
				case applySucceeded, applyPermanentFailure:
					batch = batch[:0]
				case applyRetryLater:
				}
			} else {
				switch writer.applyUntil(ctx, rollupEvents(rollups)) {
				case applySucceeded, applyPermanentFailure:
					clear(rollups)
				case applyRetryLater:
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			flushContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			writer.drainInto(&batch, rollups)
			_ = writer.applyUntil(flushContext, batch)
			_ = writer.applyUntil(flushContext, rollupEvents(rollups))
			return nil
		case event := <-writer.queue:
			if writer.disabled.Load() {
				writer.dropped.Add(1)
				continue
			}
			writer.addEvent(&batch, rollups, event)
		case <-ticker.C:
			switch writer.applyUntil(ctx, batch) {
			case applySucceeded, applyPermanentFailure:
				batch = batch[:0]
			}
		case <-rollupTicker.C:
			events := rollupEvents(rollups)
			switch writer.applyUntil(ctx, events) {
			case applySucceeded, applyPermanentFailure:
				clear(rollups)
			}
		}
	}
}

func (writer *Writer) Stats() WriterStats {
	return WriterStats{Accepted: writer.accepted.Load(), Dropped: writer.dropped.Load(), Batches: writer.batches.Load(), Written: writer.written.Load(), Failed: writer.failed.Load(), Coalesced: writer.coalesced.Load(), LastSize: int(writer.lastSize.Load()), Latency: time.Duration(writer.latency.Load())}
}

func (writer *Writer) Depth() int { return len(writer.queue) }

func (writer *Writer) drainInto(batch *[]WriteEvent, rollups map[reportRollupKey]ReportRollup) {
	for {
		select {
		case event := <-writer.queue:
			writer.addEvent(batch, rollups, event)
		default:
			return
		}
	}
}

type reportRollupKey struct {
	guildID uint64
	scope   domain.FeederID
	bucket  int64
}

func (writer *Writer) addEvent(batch *[]WriteEvent, rollups map[reportRollupKey]ReportRollup, event WriteEvent) {
	if event.Kind != WriteReportRollup {
		*batch = append(*batch, event)
		return
	}
	value := event.Rollup
	value.BucketStart = value.BucketStart.UTC().Truncate(time.Hour)
	key := reportRollupKey{guildID: value.GuildID, scope: value.FeederScope, bucket: value.BucketStart.Unix()}
	if current, exists := rollups[key]; exists {
		current.AircraftObservations += value.AircraftObservations
		current.Messages += value.Messages
		current.EmergencyObservations += value.EmergencyObservations
		current.EmergencyEvents += value.EmergencyEvents
		current.MaximumRange = max(current.MaximumRange, value.MaximumRange)
		current.PeakTracked = max(current.PeakTracked, value.PeakTracked)
		rollups[key] = current
		writer.coalesced.Add(1)
		return
	}
	rollups[key] = value
}

func rollupEvents(rollups map[reportRollupKey]ReportRollup) []WriteEvent {
	events := make([]WriteEvent, 0, len(rollups))
	for _, value := range rollups {
		events = append(events, WriteEvent{Kind: WriteReportRollup, Rollup: value})
	}
	return events
}

func (writer *Writer) applyUntil(ctx context.Context, batch []WriteEvent) applyResult {
	if len(batch) == 0 {
		return applySucceeded
	}
	delay := 50 * time.Millisecond
	deadline := time.Now().Add(writer.retryBudget)
	for {
		if err := writer.apply(ctx, batch); err == nil {
			writer.notify(nil)
			return applySucceeded
		} else {
			writer.notify(err)
			if !transientWriteError(err) {
				writer.disabled.Store(true)
				return applyPermanentFailure
			}
		}
		if time.Now().Add(delay).After(deadline) {
			return applyRetryLater
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return applyRetryLater
		case <-timer.C:
		}
		delay = min(delay*2, time.Second)
	}
}

func transientWriteError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	type temporary interface{ Temporary() bool }
	var value temporary
	if errors.As(err, &value) && value.Temporary() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database locked") ||
		strings.Contains(message, "database is busy") || strings.Contains(message, "database busy") ||
		strings.Contains(message, "sqlite_busy") || strings.Contains(message, "sqlite_locked")
}

func (writer *Writer) notify(err error) {
	changed := false
	if err != nil {
		changed = writer.degraded.CompareAndSwap(false, true)
	} else {
		changed = writer.degraded.CompareAndSwap(true, false)
	}
	if !changed {
		return
	}
	writer.observerMu.RLock()
	observer := writer.observer
	writer.observerMu.RUnlock()
	if observer != nil {
		observer(err)
	}
}

func (writer *Writer) apply(ctx context.Context, batch []WriteEvent) error {
	if len(batch) == 0 {
		return nil
	}
	writer.batches.Add(1)
	started := time.Now()
	writer.lastSize.Store(int64(len(batch)))
	if err := writer.sink.ApplyBatch(ctx, batch); err != nil {
		writer.latency.Store(time.Since(started).Nanoseconds())
		writer.failed.Add(uint64(len(batch)))
		return err
	}
	writer.latency.Store(time.Since(started).Nanoseconds())
	writer.written.Add(uint64(len(batch)))
	return nil
}
