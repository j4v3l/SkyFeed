package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type aircraftResult struct {
	frame Frame[domain.AircraftBatch]
	err   error
}

type aircraftStub struct {
	id      domain.ProviderID
	results []aircraftResult
	calls   int
	fetch   func(context.Context) (Frame[domain.AircraftBatch], error)
}

func (stub *aircraftStub) ProviderID() domain.ProviderID { return stub.id }

func (*aircraftStub) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityAircraft)
}

func (stub *aircraftStub) FetchAircraft(ctx context.Context) (Frame[domain.AircraftBatch], error) {
	stub.calls++
	if stub.fetch != nil {
		return stub.fetch(ctx)
	}
	result := stub.results[0]
	stub.results = stub.results[1:]
	return result.frame, result.err
}

func TestAircraftFailoverUsesOrderedProvidersAndStableFailback(t *testing.T) {
	primary := &aircraftStub{
		id: domain.ProviderReadsb,
		results: []aircraftResult{
			{err: errors.New("offline")},
			{frame: aircraftFrame("AAA001", 100)},
			{frame: aircraftFrame("AAA002", 110)},
		},
	}
	fallback := &aircraftStub{
		id: domain.ProviderAirplanesLive,
		results: []aircraftResult{
			{frame: aircraftFrame("BBB001", 0)},
			{frame: aircraftFrame("BBB002", 0)},
		},
	}
	failover, err := NewAircraftFailover(
		[]AircraftSource{primary, fallback},
		AircraftFailoverConfig{RecoverySuccesses: 2},
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := failover.FetchAircraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Provider != domain.ProviderAirplanesLive || first.Value.Aircraft[0].ICAO != "BBB001" {
		t.Fatalf("initial fallback frame = %+v", first)
	}
	if first.Value.Aircraft[0].Provider != domain.ProviderAirplanesLive {
		t.Fatalf("aircraft provider = %q", first.Value.Aircraft[0].Provider)
	}

	second, err := failover.FetchAircraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Provider != domain.ProviderAirplanesLive {
		t.Fatalf("provider flapped after one recovery = %q", second.Provider)
	}

	third, err := failover.FetchAircraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.Provider != domain.ProviderReadsb || third.Value.Aircraft[0].ICAO != "AAA002" {
		t.Fatalf("stable failback frame = %+v", third)
	}
	if primary.calls != 3 || fallback.calls != 2 {
		t.Fatalf("calls primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestAircraftFailoverDoesNotContinueAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	primary := &aircraftStub{id: domain.ProviderReadsb}
	primary.fetch = func(context.Context) (Frame[domain.AircraftBatch], error) {
		cancel()
		return Frame[domain.AircraftBatch]{}, context.Canceled
	}
	fallback := &aircraftStub{
		id:      domain.ProviderAirplanesLive,
		results: []aircraftResult{{frame: aircraftFrame("BBB001", 0)}},
	}
	failover, err := NewAircraftFailover([]AircraftSource{primary, fallback}, DefaultAircraftFailoverConfig())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := failover.FetchAircraft(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetch error = %v", err)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback called %d times after cancellation", fallback.calls)
	}
}

func TestAircraftFailoverRejectsUnsupportedProviders(t *testing.T) {
	unsupported := &unsupportedAircraftStub{}
	if _, err := NewAircraftFailover([]AircraftSource{unsupported}, DefaultAircraftFailoverConfig()); err == nil {
		t.Fatal("unsupported provider was accepted")
	}
}

type unsupportedAircraftStub struct{}

func (*unsupportedAircraftStub) ProviderID() domain.ProviderID { return domain.ProviderReadsb }
func (*unsupportedAircraftStub) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityReceiver)
}
func (*unsupportedAircraftStub) FetchAircraft(context.Context) (Frame[domain.AircraftBatch], error) {
	return Frame[domain.AircraftBatch]{}, nil
}

func aircraftFrame(icao string, messages uint64) Frame[domain.AircraftBatch] {
	now := time.Unix(1_700_000_000, 0)
	return Frame[domain.AircraftBatch]{
		FetchedAt: now,
		Value: domain.AircraftBatch{
			GeneratedAt:         now,
			Messages:            messages,
			MessageCounterValid: messages > 0,
			Aircraft:            []domain.Aircraft{{ICAO: icao}},
		},
	}
}
