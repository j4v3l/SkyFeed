package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestTopRouteRankingsAggregateSightings(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	batch := storage.RouteSightingsBatch{
		GuildID:     42,
		BucketStart: now.Truncate(time.Hour),
		Observations: []storage.RouteSightingsObservation{
			{ICAO: "ABC123", Callsign: "AAL100", Route: storage.RouteCatalog{Source: domain.DataSourceADSBLOL,
				Callsign: "AAL100", AirlineName: "American", AirlineICAO: "AAL",
				OriginIATA: "JFK", OriginName: "JFK", OriginCountryISO: "US",
				DestinationIATA: "PBI", DestinationName: "PBI", DestinationCountryISO: "US",
				Plausible: true, UpdatedAt: now,
			}},
			{ICAO: "DEF456", Callsign: "AAL100", Route: storage.RouteCatalog{Source: domain.DataSourceADSBLOL,
				Callsign: "AAL100", AirlineName: "American", AirlineICAO: "AAL",
				OriginIATA: "JFK", OriginName: "JFK", OriginCountryISO: "US",
				DestinationIATA: "PBI", DestinationName: "PBI", DestinationCountryISO: "US",
				Plausible: true, UpdatedAt: now,
			}},
		},
	}
	if err := store.RecordRouteSightings(ctx, batch); err != nil {
		t.Fatal(err)
	}
	rows, err := store.TopRouteRankings(ctx, 42, "routes", "all", 5, "US")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Count != 2 || rows[0].Label != "JFK → PBI" {
		t.Fatalf("rows=%#v", rows)
	}
	airlines, err := store.TopRouteRankings(ctx, 42, "airlines", "all", 5, "US")
	if err != nil {
		t.Fatal(err)
	}
	if len(airlines) != 1 || airlines[0].Label != "American" {
		t.Fatalf("airlines=%#v", airlines)
	}
}
