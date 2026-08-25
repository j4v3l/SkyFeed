package storage

import (
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type RouteCatalog struct {
	Callsign              string
	AirlineName           string
	AirlineICAO           string
	AirlineIATA           string
	OriginICAO            string
	OriginIATA            string
	OriginName            string
	OriginCountryISO      string
	DestinationICAO       string
	DestinationIATA       string
	DestinationName       string
	DestinationCountryISO string
	Plausible             bool
	PlausibilityKnown     bool
	UpdatedAt             time.Time
}

type RouteSightingsObservation struct {
	ICAO     string
	Callsign string
	Route    RouteCatalog
}

type RouteSightingsBatch struct {
	GuildID      uint64
	BucketStart  time.Time
	Observations []RouteSightingsObservation
}

type RouteRankingRow struct {
	Label  string
	Detail string
	Count  int64
}

func RouteCatalogFromDomain(route domain.Route) (RouteCatalog, bool) {
	if route.Midpoint != nil {
		return RouteCatalog{}, false
	}
	if route.PlausibilityKnown && !route.Plausible {
		return RouteCatalog{}, false
	}
	originCode := firstRouteCode(route.Origin)
	destinationCode := firstRouteCode(route.Destination)
	if originCode == "" || destinationCode == "" || originCode == destinationCode {
		return RouteCatalog{}, false
	}
	callsign := strings.ToUpper(strings.TrimSpace(route.Callsign))
	if callsign == "" {
		return RouteCatalog{}, false
	}
	updatedAt := route.FetchedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return RouteCatalog{
		Callsign:              callsign,
		AirlineName:           strings.TrimSpace(route.AirlineName),
		AirlineICAO:           strings.ToUpper(strings.TrimSpace(route.AirlineICAO)),
		AirlineIATA:           strings.ToUpper(strings.TrimSpace(route.AirlineIATA)),
		OriginICAO:            strings.ToUpper(strings.TrimSpace(route.Origin.ICAO)),
		OriginIATA:            strings.ToUpper(strings.TrimSpace(route.Origin.IATA)),
		OriginName:            strings.TrimSpace(route.Origin.Name),
		OriginCountryISO:      strings.ToUpper(strings.TrimSpace(route.Origin.CountryCode)),
		DestinationICAO:       strings.ToUpper(strings.TrimSpace(route.Destination.ICAO)),
		DestinationIATA:       strings.ToUpper(strings.TrimSpace(route.Destination.IATA)),
		DestinationName:       strings.TrimSpace(route.Destination.Name),
		DestinationCountryISO: strings.ToUpper(strings.TrimSpace(route.Destination.CountryCode)),
		Plausible:             route.Plausible,
		PlausibilityKnown:     route.PlausibilityKnown,
		UpdatedAt:             updatedAt,
	}, true
}

func firstRouteCode(airport domain.Airport) string {
	if code := strings.ToUpper(strings.TrimSpace(airport.IATA)); code != "" {
		return code
	}
	return strings.ToUpper(strings.TrimSpace(airport.ICAO))
}
