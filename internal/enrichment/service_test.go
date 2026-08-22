package enrichment

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type enricherStub struct{ calls atomic.Int64 }

func (stub *enricherStub) Lookup(_ context.Context, icao, callsign string) (domain.Enrichment, error) {
	stub.calls.Add(1)
	return domain.Enrichment{ICAO: icao, Callsign: callsign, Aircraft: &domain.AircraftMetadata{Registration: "N123SF"}, Found: true}, nil
}

func TestServiceCoalescesWorkersAndCaches(t *testing.T) {
	stub := &enricherStub{}
	config := DefaultConfig()
	config.Workers, config.RequestsPerSecond, config.Burst = 2, 1000, 10
	service := NewService(stub, config)
	for range 20 {
		service.Enqueue("abc123", "sky123")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for service.CacheLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("calls=%d", stub.calls.Load())
	}
	result, ok, err := service.Cached("ABC123", "SKY123")
	if err != nil || !ok || result.Aircraft == nil {
		t.Fatalf("result=%+v ok=%t err=%v", result, ok, err)
	}
	if service.Stats().Coalesced != 19 {
		t.Fatalf("stats=%+v", service.Stats())
	}
}

func BenchmarkADSBDBCache(b *testing.B) {
	cache := NewCache(100)
	cache.Set("ABC123|SKY123", domain.Enrichment{Found: true}, nil, time.Hour, time.Hour)
	for b.Loop() {
		_, _, _, _ = cache.Get("ABC123|SKY123")
	}
}
