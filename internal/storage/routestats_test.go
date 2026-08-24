package storage

import (
	"testing"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestRouteCatalogFromDomainRequiresSimplePlausibleRoute(t *testing.T) {
	route := domain.Route{
		Callsign: "AAL100",
		Origin:   domain.Airport{ICAO: "KJFK", IATA: "JFK", CountryCode: "US"},
		Destination: domain.Airport{ICAO: "KPBI", IATA: "PBI", CountryCode: "US"},
		Plausible: true, PlausibilityKnown: true,
	}
	catalog, ok := RouteCatalogFromDomain(route)
	if !ok || catalog.OriginIATA != "JFK" || catalog.DestinationIATA != "PBI" {
		t.Fatalf("catalog=%+v ok=%t", catalog, ok)
	}
	mid := domain.Airport{ICAO: "KMCO"}
	route.Midpoint = &mid
	if _, ok := RouteCatalogFromDomain(route); ok {
		t.Fatal("midpoint routes should be skipped")
	}
	route.Midpoint = nil
	route.Plausible = false
	if _, ok := RouteCatalogFromDomain(route); ok {
		t.Fatal("implausible routes should be skipped")
	}
}
