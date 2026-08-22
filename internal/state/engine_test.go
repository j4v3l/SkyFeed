package state

import (
	"errors"
	"fmt"
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
		Value: domain.AircraftBatch{
			GeneratedAt: now,
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
