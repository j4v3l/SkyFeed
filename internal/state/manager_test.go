package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/rules"
	"github.com/j4v3l/SkyFeed/internal/track"
)

func TestFeederManagerDeduplicatesByFreshestObservation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	manager := NewFeederManager(time.Millisecond)
	manager.now = func() time.Time { return now }
	for _, descriptor := range []domain.FeederDescriptor{
		{ID: "north", DisplayName: "North Field", Enabled: true, SourceKind: domain.FeederSourceAgent},
		{ID: "south", DisplayName: "South Field", Enabled: true, SourceKind: domain.FeederSourceAgent},
	} {
		if err := manager.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	manager.Publish("north", feederSnapshot("north", now, []domain.Aircraft{{ICAO: "ABC123", Callsign: "OLD", Seen: 5 * time.Second}, {ICAO: "DEF456"}}))
	manager.Publish("south", feederSnapshot("south", now.Add(time.Second), []domain.Aircraft{{ICAO: "ABC123", Callsign: "NEW", HasPosition: true}, {ICAO: "FED321"}}))
	aggregate := manager.Rebuild()
	if len(aggregate.Aircraft) != 3 || aggregate.FeederID != domain.FeederAll || len(aggregate.Feeders) != 2 {
		t.Fatalf("aggregate = %+v", aggregate)
	}
	aircraft, ok := aggregate.LookupICAO("ABC123")
	if !ok || aircraft.Callsign != "NEW" || len(aircraft.SeenBy) != 2 || aircraft.SeenBy[0] != "north" || aircraft.SeenBy[1] != "south" {
		t.Fatalf("deduplicated aircraft = %+v, %t", aircraft, ok)
	}
	if original, _ := manager.Feeder("south"); original.Aircraft[0].SeenBy != nil {
		t.Fatalf("source snapshot mutated: %+v", original.Aircraft[0])
	}
}

func TestFeederManagerRejectsReservedAndDisabledFeeders(t *testing.T) {
	manager := NewFeederManager(time.Second)
	if err := manager.Register(domain.FeederDescriptor{ID: domain.FeederAll, Enabled: true}); err == nil {
		t.Fatal("reserved aggregate ID accepted")
	}
	if err := manager.Register(domain.FeederDescriptor{ID: "disabled", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if manager.Publish("disabled", feederSnapshot("disabled", time.Now(), nil)) {
		t.Fatal("disabled feeder publication accepted")
	}
}

func TestFeederManagerRejectsNonCanonicalAircraft(t *testing.T) {
	manager := NewFeederManager(time.Second)
	if err := manager.Register(domain.FeederDescriptor{ID: "feed", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for name, aircraft := range map[string][]domain.Aircraft{
		"lowercase": {{ICAO: "abc123"}},
		"unsorted":  {{ICAO: "DEF456"}, {ICAO: "ABC123"}},
		"duplicate": {{ICAO: "ABC123"}, {ICAO: "ABC123"}},
	} {
		t.Run(name, func(t *testing.T) {
			if manager.Publish("feed", feederSnapshot("feed", time.Now(), aircraft)) {
				t.Fatal("non-canonical snapshot was published")
			}
		})
	}
}

func TestAggregatePreservesEmergencyReportedByAnyFeeder(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	manager := NewFeederManager(time.Millisecond)
	for _, id := range []domain.FeederID{"east", "west"} {
		if err := manager.Register(domain.FeederDescriptor{ID: id, DisplayName: string(id), Enabled: true, SourceKind: domain.FeederSourceAgent}); err != nil {
			t.Fatal(err)
		}
	}
	manager.Publish("east", feederSnapshot("east", now, []domain.Aircraft{{ICAO: "ABC123", Squawk: "7700"}}))
	manager.Publish("west", feederSnapshot("west", now.Add(time.Second), []domain.Aircraft{{ICAO: "ABC123", Squawk: "1200"}}))
	aggregate := manager.Rebuild()
	aircraft, ok := aggregate.LookupICAO("ABC123")
	if !ok || !domain.EmergencyActive(aircraft) || aircraft.Squawk != "7700" || len(aircraft.SeenBy) != 2 {
		t.Fatalf("aggregate emergency = %+v", aircraft)
	}
}

func BenchmarkAggregateTwentyFiveThousandObservations(b *testing.B) {
	benchmarkAggregate(b, 250)
}

func BenchmarkAggregateHundredThousandObservations(b *testing.B) {
	benchmarkAggregate(b, 1_000)
}

func benchmarkAggregate(b *testing.B, aircraftPerFeeder int) {
	b.Helper()
	manager := NewFeederManager(time.Second)
	now := time.Unix(1_800_000_000, 0)
	for feederIndex := 0; feederIndex < 100; feederIndex++ {
		id := domain.FeederID(fmt.Sprintf("feed-%03d", feederIndex))
		_ = manager.Register(domain.FeederDescriptor{ID: id, DisplayName: string(id), Enabled: true, SourceKind: domain.FeederSourceAgent})
		aircraft := make([]domain.Aircraft, aircraftPerFeeder)
		for aircraftIndex := range aircraft {
			// Adjacent feeds overlap by half, exercising both selection and insertion.
			aircraft[aircraftIndex] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", feederIndex*(aircraftPerFeeder/2)+aircraftIndex), Messages: uint64(feederIndex)}
		}
		manager.Publish(id, feederSnapshot(id, now, aircraft))
	}
	b.ReportAllocs()
	for range b.N {
		manager.Rebuild()
	}
}

func BenchmarkPublishOneThousandAircraft(b *testing.B) {
	manager := NewFeederManager(time.Second)
	_ = manager.Register(domain.FeederDescriptor{ID: "bench", DisplayName: "bench", Enabled: true, SourceKind: domain.FeederSourceAgent})
	aircraft := make([]domain.Aircraft, 1_000)
	for index := range aircraft {
		aircraft[index].ICAO = fmt.Sprintf("%06X", index)
	}
	snapshot := feederSnapshot("bench", time.Unix(1_800_000_000, 0), aircraft)
	b.ReportAllocs()
	for range b.N {
		manager.Publish("bench", snapshot)
	}
}

func BenchmarkCommunityPipelineHundredFeeders(b *testing.B) {
	manager := NewFeederManager(time.Second)
	now := time.Unix(1_800_000_000, 0)
	manager.now = func() time.Time {
		now = now.Add(track.DefaultSampleInterval)
		return now
	}
	for feederIndex := range 100 {
		id := domain.FeederID(fmt.Sprintf("feed-%03d", feederIndex))
		_ = manager.Register(domain.FeederDescriptor{ID: id, DisplayName: string(id), Enabled: true, SourceKind: domain.FeederSourceAgent})
		aircraft := make([]domain.Aircraft, 250)
		for aircraftIndex := range aircraft {
			aircraft[aircraftIndex] = domain.Aircraft{
				ICAO: fmt.Sprintf("%06X", feederIndex*125+aircraftIndex), Callsign: fmt.Sprintf("SF%04d", aircraftIndex),
				HasDistance: true, DistanceNM: float64(aircraftIndex) / 10,
			}
		}
		manager.Publish(id, feederSnapshot(id, now, aircraft))
	}
	ruleSet := make([]domain.WatchRule, 1_000)
	for index := range ruleSet {
		ruleSet[index] = domain.WatchRule{ID: int64(index + 1), FeederScope: domain.FeederAll, Type: domain.RuleICAO, Value: fmt.Sprintf("%06X", index), Enabled: true}
	}
	ruleEngine := rules.NewEngine(ruleSet, nil)
	trackStore := track.NewStore()
	manager.SetAggregateObserver(func(snapshot *domain.Snapshot) {
		ruleEngine.Evaluate(snapshot)
		trackStore.Observe(snapshot)
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		manager.Rebuild()
	}
}

func feederSnapshot(id domain.FeederID, fetched time.Time, aircraft []domain.Aircraft) *domain.Snapshot {
	byICAO := make(map[string]int, len(aircraft))
	for index := range aircraft {
		byICAO[aircraft[index].ICAO] = index
	}
	return &domain.Snapshot{
		FeederID: id, FetchedAt: fetched, PublishedAt: fetched, Aircraft: aircraft, ByICAO: byICAO,
		Health: domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}, Stats: domain.SourceHealth{Status: domain.HealthHealthy}},
	}
}
