package state

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

func TestEnginePublishesImmutableIndexedSnapshot(t *testing.T) {
	now := time.Unix(1787414400, 0)
	engine := NewEngine(nil)
	engine.now = func() time.Time { return now }
	engine.applyReceiver(source.Frame[domain.Receiver]{
		FetchedAt: now,
		Value: domain.Receiver{
			Latitude:    40,
			Longitude:   -75,
			HasPosition: true,
		},
	}, 30*time.Second)
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{
		FetchedAt: now,
		Provider:  domain.ProviderReadsb,
		Value: domain.AircraftBatch{
			GeneratedAt:         now,
			Messages:            100,
			MessageCounterValid: true,
			Aircraft: []domain.Aircraft{
				{ICAO: "DEF456", Callsign: "TEST45"},
				{ICAO: "ABC123", Callsign: "SKY123", Latitude: 41, Longitude: -75, HasPosition: true},
			},
		},
	}, time.Second)

	first := engine.Current()
	aircraft, ok := first.LookupICAO("ABC123")
	if !ok || aircraft.Callsign != "SKY123" || !aircraft.HasDistance {
		t.Fatalf("lookup = %#v, %v", aircraft, ok)
	}
	if first.Aircraft[0].ICAO != "ABC123" || first.Aircraft[1].ICAO != "DEF456" {
		t.Fatalf("aircraft are not stably sorted: %#v", first.Aircraft)
	}
	if first.ActiveProvider != domain.ProviderReadsb || !first.MessageCounterValid || first.Aircraft[0].Provider != domain.ProviderReadsb {
		t.Fatalf("provider state = provider %q counter_valid=%v aircraft=%q", first.ActiveProvider, first.MessageCounterValid, first.Aircraft[0].Provider)
	}

	engine.applyAircraft(source.Frame[domain.AircraftBatch]{
		FetchedAt: now.Add(time.Second),
		Value: domain.AircraftBatch{
			GeneratedAt: now.Add(time.Second),
			Aircraft:    []domain.Aircraft{{ICAO: "ABC123", Callsign: "CHANGED"}},
		},
	}, time.Second)
	if old, _ := first.LookupICAO("ABC123"); old.Callsign != "SKY123" {
		t.Fatalf("old snapshot mutated: %#v", old)
	}
	if current, _ := engine.Current().LookupICAO("ABC123"); current.Callsign != "CHANGED" {
		t.Fatalf("new snapshot missing update: %#v", current)
	}
}

func TestEngineDoesNotPollUnsupportedMetadata(t *testing.T) {
	upstream := &pollingSourceStub{aircraftFetched: make(chan struct{})}
	engine := NewEngine(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, source.Set{Aircraft: upstream, Receiver: upstream, Stats: upstream}, time.Hour, time.Hour)
	}()

	select {
	case <-upstream.aircraftFetched:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("aircraft source was not polled")
	}
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if upstream.receiverCalls.Load() != 0 || upstream.statsCalls.Load() != 0 {
		t.Fatalf("unsupported polls receiver=%d stats=%d", upstream.receiverCalls.Load(), upstream.statsCalls.Load())
	}
	snapshot := engine.Current()
	if snapshot.Health.Receiver.Status != domain.HealthDisabled || snapshot.Health.Stats.Status != domain.HealthDisabled {
		t.Fatalf("metadata health = %+v", snapshot.Health)
	}
	if !snapshot.Capabilities.Supports(domain.CapabilityAircraft) ||
		snapshot.Capabilities.Supports(domain.CapabilityReceiver) ||
		snapshot.Capabilities.Supports(domain.CapabilityStatistics) {
		t.Fatalf("capabilities = %08b", snapshot.Capabilities)
	}
}

func TestEngineTreatsFallbackSuccessAsHealthy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	primary := &aircraftProviderStub{
		id:  domain.ProviderReadsb,
		err: &source.FetchError{Endpoint: "aircraft.json", Class: source.ErrorNetwork, Err: errors.New("offline")},
	}
	fallback := &aircraftProviderStub{
		id: domain.ProviderAirplanesLive,
		frame: source.Frame[domain.AircraftBatch]{
			FetchedAt: now,
			Value: domain.AircraftBatch{
				GeneratedAt: now,
				Aircraft:    []domain.Aircraft{{ICAO: "ABC123"}},
			},
		},
	}
	failover, err := source.NewAircraftFailover(
		[]source.AircraftSource{primary, fallback},
		source.DefaultAircraftFailoverConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngine(func(snapshot *domain.Snapshot) {
		if !snapshot.FetchedAt.IsZero() {
			cancel()
		}
	})
	if err := engine.Run(ctx, source.Set{Aircraft: failover}, time.Hour, time.Hour); err != nil {
		t.Fatalf("run: %v", err)
	}
	snapshot := engine.Current()
	if snapshot.Health.Aircraft.Status != domain.HealthHealthy ||
		snapshot.Health.Aircraft.Provider != domain.ProviderAirplanesLive ||
		snapshot.ActiveProvider != domain.ProviderAirplanesLive {
		t.Fatalf("fallback health = %+v active=%q", snapshot.Health.Aircraft, snapshot.ActiveProvider)
	}
	if snapshot.Health.Receiver.Status != domain.HealthDisabled || snapshot.Health.Stats.Status != domain.HealthDisabled {
		t.Fatalf("unsupported metadata health = %+v", snapshot.Health)
	}
}

type aircraftProviderStub struct {
	id    domain.ProviderID
	frame source.Frame[domain.AircraftBatch]
	err   error
}

func (stub *aircraftProviderStub) ProviderID() domain.ProviderID { return stub.id }
func (*aircraftProviderStub) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityAircraft)
}
func (stub *aircraftProviderStub) FetchAircraft(context.Context) (source.Frame[domain.AircraftBatch], error) {
	return stub.frame, stub.err
}

type pollingSourceStub struct {
	once            sync.Once
	aircraftFetched chan struct{}
	receiverCalls   atomic.Int64
	statsCalls      atomic.Int64
}

func (*pollingSourceStub) ProviderID() domain.ProviderID { return domain.ProviderReadsb }
func (*pollingSourceStub) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityAircraft)
}
func (stub *pollingSourceStub) FetchAircraft(context.Context) (source.Frame[domain.AircraftBatch], error) {
	now := time.Now()
	stub.once.Do(func() { close(stub.aircraftFetched) })
	return source.Frame[domain.AircraftBatch]{
		FetchedAt: now,
		Provider:  domain.ProviderReadsb,
		Value: domain.AircraftBatch{
			Provider:            domain.ProviderReadsb,
			GeneratedAt:         now,
			MessageCounterValid: true,
		},
	}, nil
}
func (stub *pollingSourceStub) FetchReceiver(context.Context) (source.Frame[domain.Receiver], error) {
	stub.receiverCalls.Add(1)
	return source.Frame[domain.Receiver]{}, nil
}
func (stub *pollingSourceStub) FetchStats(context.Context) (source.Frame[domain.Statistics], error) {
	stub.statsCalls.Add(1)
	return source.Frame[domain.Statistics]{}, nil
}

func TestFailureRetainsLastGoodAircraft(t *testing.T) {
	now := time.Unix(1787414400, 0)
	engine := NewEngine(nil)
	engine.now = func() time.Time { return now }
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{
		FetchedAt: now,
		Value: domain.AircraftBatch{
			GeneratedAt: now,
			Aircraft:    []domain.Aircraft{{ICAO: "ABC123"}},
		},
	}, time.Second)

	now = now.Add(time.Second)
	engine.aircraftFailure(&source.FetchError{Endpoint: "aircraft.json", Class: source.ErrorPayload, Err: errors.New("bad JSON")}, time.Second)
	snapshot := engine.Current()
	if len(snapshot.Aircraft) != 1 || snapshot.Health.Aircraft.Status != domain.HealthDegraded || snapshot.Health.Aircraft.ErrorClass != string(source.ErrorPayload) {
		t.Fatalf("snapshot after failure: %#v", snapshot)
	}
}

func TestMetadataAndHealthPublicationsReuseImmutableAircraftIndexes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	engine := NewEngine(nil)
	engine.now = func() time.Time { return now }
	engine.applyReceiver(source.Frame[domain.Receiver]{FetchedAt: now, Value: domain.Receiver{Latitude: 40, Longitude: -75, HasPosition: true}}, 30*time.Second)
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Provider: domain.ProviderReadsb, Value: domain.AircraftBatch{GeneratedAt: now, Aircraft: []domain.Aircraft{{ICAO: "ABC123", Callsign: "SKY1", Latitude: 41, Longitude: -75, HasPosition: true}}}}, time.Second)
	first := engine.Current()
	aircraftAddress := &first.Aircraft[0]
	searchAddress := &first.Search[0]

	engine.applyStats(source.Frame[domain.Statistics]{FetchedAt: now, Value: domain.Statistics{WindowEnd: now, Messages: 10}}, 30*time.Second)
	afterStats := engine.Current()
	if &afterStats.Aircraft[0] != aircraftAddress || &afterStats.Search[0] != searchAddress || afterStats.Statistics.Messages != 10 {
		t.Fatal("stats publication rebuilt aircraft/index data")
	}
	engine.statsFailure(errors.New("timeout"), 30*time.Second)
	afterHealth := engine.Current()
	if &afterHealth.Aircraft[0] != aircraftAddress || &afterHealth.Search[0] != searchAddress {
		t.Fatal("health publication rebuilt aircraft/index data")
	}
	engine.applyReceiver(source.Frame[domain.Receiver]{FetchedAt: now.Add(time.Second), Value: domain.Receiver{Latitude: 41, Longitude: -75, HasPosition: true}}, 30*time.Second)
	if &engine.Current().Aircraft[0] == aircraftAddress {
		t.Fatal("receiver position change failed to rebuild derived distances")
	}
}

func TestProviderTransitionTimestampTracksActualChange(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	engine := NewEngine(nil)
	engine.now = func() time.Time { return now }
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Provider: domain.ProviderReadsb, Value: domain.AircraftBatch{GeneratedAt: now}}, time.Second)
	firstChange := engine.Current().ProviderChangedAt
	now = now.Add(time.Minute)
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Provider: domain.ProviderAirplanesLive, Value: domain.AircraftBatch{GeneratedAt: now}}, time.Second)
	if !engine.Current().ProviderChangedAt.Equal(now) || engine.Current().ProviderChangedAt.Equal(firstChange) {
		t.Fatalf("provider transition timestamp = %s", engine.Current().ProviderChangedAt)
	}
}

func TestAircraftPublicationNormalizesAndDeduplicatesICAO(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	engine := NewEngine(nil)
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Value: domain.AircraftBatch{Aircraft: []domain.Aircraft{
		{ICAO: " def456 ", Messages: 1},
		{ICAO: "abc123", Messages: 1},
		{ICAO: "ABC123", Messages: 2, HasPosition: true},
		{ICAO: ""},
	}}}, time.Second)
	snapshot := engine.Current()
	if len(snapshot.Aircraft) != 2 || snapshot.Aircraft[0].ICAO != "ABC123" || snapshot.Aircraft[1].ICAO != "DEF456" {
		t.Fatalf("aircraft = %+v", snapshot.Aircraft)
	}
	if snapshot.Aircraft[0].Messages != 2 || !snapshot.Aircraft[0].HasPosition {
		t.Fatalf("deduplicated observation = %+v", snapshot.Aircraft[0])
	}
}

func BenchmarkMetadataSnapshotReuse(b *testing.B) {
	now := time.Unix(1_700_000_000, 0)
	engine := NewEngine(nil)
	aircraft := make([]domain.Aircraft, 1_000)
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index)}
	}
	engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Value: domain.AircraftBatch{GeneratedAt: now, Aircraft: aircraft}}, time.Second)
	b.ReportAllocs()
	for b.Loop() {
		engine.applyStats(source.Frame[domain.Statistics]{FetchedAt: now, Value: domain.Statistics{WindowEnd: now}}, 30*time.Second)
	}
}

func TestSourceTimestampSanity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	stale := successHealth(domain.SourceHealth{}, now, now.Add(-time.Minute), time.Second)
	if stale.Status != domain.HealthStale || stale.ErrorClass != "source_data_stale" {
		t.Fatalf("stale=%+v", stale)
	}
	future := successHealth(domain.SourceHealth{}, now, now.Add(10*time.Minute), time.Second)
	if future.Status != domain.HealthDegraded || future.ErrorClass != "source_clock_future" {
		t.Fatalf("future=%+v", future)
	}
}

func BenchmarkNormalizeSnapshot(b *testing.B) {
	batch := domain.AircraftBatch{
		GeneratedAt: time.Unix(1787414400, 0),
		Aircraft:    make([]domain.Aircraft, 1000),
	}
	for index := range batch.Aircraft {
		batch.Aircraft[index] = domain.Aircraft{
			ICAO:        fmt.Sprintf("%06X", index),
			Callsign:    fmt.Sprintf("SKY%04d", index),
			Latitude:    39 + float64(index%100)/100,
			Longitude:   -76 + float64(index%100)/100,
			HasPosition: true,
		}
	}

	b.ReportAllocs()
	for range b.N {
		engine := NewEngine(nil)
		engine.applyReceiver(source.Frame[domain.Receiver]{
			FetchedAt: batch.GeneratedAt,
			Value:     domain.Receiver{Latitude: 40, Longitude: -75, HasPosition: true},
		}, 30*time.Second)
		engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: batch.GeneratedAt, Value: batch}, time.Second)
	}
}
