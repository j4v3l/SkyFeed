package storage

import (
	"context"
	"sync"
	"testing"
	"time"
)

type batchSinkStub struct {
	mu     sync.Mutex
	events []WriteEvent
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
