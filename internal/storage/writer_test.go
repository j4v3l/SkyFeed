package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type batchSinkStub struct {
	mu     sync.Mutex
	events []WriteEvent
}

type permanentBatchSink struct{}

func (permanentBatchSink) ApplyBatch(context.Context, []WriteEvent) error {
	return errors.New("database is corrupt")
}

func TestWriterNeverCoalescesDifferentFeederScopes(t *testing.T) {
	sink := &retryBatchSink{}
	writer := NewWriter(sink, 8, 8, time.Hour)
	writer.rollupFlush = time.Millisecond
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, scope := range []domain.FeederID{"east", "west"} {
		if err := writer.Enqueue(WriteEvent{Kind: WriteReportRollup, Rollup: ReportRollup{GuildID: 1, FeederScope: scope, BucketStart: now, AircraftObservations: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for writer.Stats().Written < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if writer.Stats().Written != 2 || writer.Stats().Coalesced != 0 {
		t.Fatalf("writer stats = %+v", writer.Stats())
	}
}

type retryBatchSink struct {
	mu       sync.Mutex
	failures int
	batches  [][]WriteEvent
}

func (sink *retryBatchSink) ApplyBatch(_ context.Context, events []WriteEvent) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.failures > 0 {
		sink.failures--
		return errors.New("database busy")
	}
	sink.batches = append(sink.batches, append([]WriteEvent(nil), events...))
	return nil
}

func (sink *batchSinkStub) ApplyBatch(_ context.Context, events []WriteEvent) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.events = append(sink.events, events...)
	return nil
}

func TestWriterIsBoundedAndFlushes(t *testing.T) {
	sink := &batchSinkStub{}
	writer := NewWriter(sink, 2, 2, time.Hour)
	if err := writer.Enqueue(WriteEvent{Kind: WriteFeederEvent}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Enqueue(WriteEvent{Kind: WriteReportRollup}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Enqueue(WriteEvent{Kind: WriteAlertState}); err != ErrWriterFull {
		t.Fatalf("expected full queue, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for writer.Stats().Written < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stats := writer.Stats(); stats.Written != 2 || stats.Dropped != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestWriterRetriesSameBatchAndCoalescesRollups(t *testing.T) {
	sink := &retryBatchSink{failures: 2}
	writer := NewWriter(sink, 16, 4, time.Millisecond)
	writer.rollupFlush = 5 * time.Millisecond
	now := time.Unix(1_700_000_000, 0).UTC()
	for index := 0; index < 3; index++ {
		if err := writer.Enqueue(WriteEvent{Kind: WriteReportRollup, Rollup: ReportRollup{GuildID: 1, BucketStart: now, AircraftObservations: 2, EmergencyEvents: int64(index % 2), PeakTracked: int64(index + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for writer.Stats().Written == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.batches) != 1 || len(sink.batches[0]) != 1 {
		t.Fatalf("batches = %#v", sink.batches)
	}
	rollup := sink.batches[0][0].Rollup
	if rollup.AircraftObservations != 6 || rollup.EmergencyEvents != 1 || rollup.PeakTracked != 3 {
		t.Fatalf("rollup = %+v", rollup)
	}
	if stats := writer.Stats(); stats.Failed != 2 || stats.Coalesced != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestWriterDisablesDurabilityAfterPermanentFailure(t *testing.T) {
	writer := NewWriter(permanentBatchSink{}, 4, 1, time.Millisecond)
	if err := writer.Enqueue(WriteEvent{Kind: WriteFeederEvent}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for !writer.disabled.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !writer.disabled.Load() {
		t.Fatal("writer did not disable after permanent failure")
	}
	if err := writer.Enqueue(WriteEvent{Kind: WriteAlertState}); !errors.Is(err, ErrWriterUnavailable) {
		t.Fatalf("enqueue error = %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
