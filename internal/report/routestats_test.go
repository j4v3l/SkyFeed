package report

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type routeLookupStub struct {
	route domain.Route
	ok    bool
}

func (stub routeLookupStub) CachedRoute(string) (domain.Route, bool, error) {
	return stub.route, stub.ok, nil
}

func TestRouteStatsCollectorRejectsADSBDBRoutes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	collector := NewRouteStatsCollector()
	snapshot := &domain.Snapshot{
		PublishedAt: now,
		Aircraft:    []domain.Aircraft{{ICAO: "ABC123", Callsign: "AAL100"}},
	}
	lookup := RouteStatsLookup{AdsbLol: routeLookupStub{ok: true, route: domain.Route{
		Source: domain.DataSourceADSBDB, Callsign: "AAL100",
		Origin: domain.Airport{ICAO: "KJFK", IATA: "JFK", CountryCode: "US"}, Destination: domain.Airport{ICAO: "KPBI", IATA: "PBI", CountryCode: "US"},
		Plausible: true, PlausibilityKnown: true,
	}}}
	batch := collector.Observe(42, snapshot, lookup, now)
	if len(batch.Observations) != 0 {
		t.Fatalf("ADSBDB observations persisted=%d", len(batch.Observations))
	}
}

func TestRouteStatsCollectorDedupesWithinHour(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	collector := NewRouteStatsCollector()
	snapshot := &domain.Snapshot{
		PublishedAt: now,
		Aircraft: []domain.Aircraft{
			{ICAO: "ABC123", Callsign: "AAL100"},
			{ICAO: "ABC123", Callsign: "AAL100"},
		},
	}
	routes := RouteStatsLookup{AdsbLol: routeLookupStub{ok: true, route: domain.Route{
		Source:      domain.DataSourceADSBLOL,
		Callsign:    "AAL100",
		Origin:      domain.Airport{ICAO: "KJFK", IATA: "JFK", CountryCode: "US"},
		Destination: domain.Airport{ICAO: "KPBI", IATA: "PBI", CountryCode: "US"},
		Plausible:   true, PlausibilityKnown: true,
	}}}
	first := collector.Observe(42, snapshot, routes, now)
	if len(first.Observations) != 1 {
		t.Fatalf("observations=%d", len(first.Observations))
	}
	second := collector.Observe(42, snapshot, routes, now)
	if len(second.Observations) != 0 {
		t.Fatalf("duplicate observations=%d", len(second.Observations))
	}
}
