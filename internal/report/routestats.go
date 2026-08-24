package report

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

type RouteLookup interface {
	CachedRoute(callsign string) (domain.Route, bool, error)
}

type AircraftEnrichmentLookup interface {
	Cached(icao, callsign string) (domain.Enrichment, bool, error)
}

type RouteStatsLookup struct {
	AdsbLol    RouteLookup
	AdsbDB     AircraftEnrichmentLookup
}

func (lookup RouteStatsLookup) routeFor(icao, callsign string) (domain.Route, bool) {
	if lookup.AdsbLol != nil {
		route, found, err := lookup.AdsbLol.CachedRoute(callsign)
		if err == nil && found {
			if _, ok := storage.RouteCatalogFromDomain(route); ok {
				return route, true
			}
		}
	}
	if lookup.AdsbDB != nil {
		enrichment, found, err := lookup.AdsbDB.Cached(icao, callsign)
		if err == nil && found && enrichment.Route != nil {
			if _, ok := storage.RouteCatalogFromDomain(*enrichment.Route); ok {
				return *enrichment.Route, true
			}
		}
	}
	return domain.Route{}, false
}

type RouteStatsCollector struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewRouteStatsCollector() *RouteStatsCollector {
	return &RouteStatsCollector{seen: make(map[string]struct{})}
}

func (collector *RouteStatsCollector) Observe(guildID uint64, snapshot *domain.Snapshot, routes RouteStatsLookup, now time.Time) storage.RouteSightingsBatch {
	if collector == nil || guildID == 0 || snapshot == nil {
		return storage.RouteSightingsBatch{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	bucket := now.UTC().Truncate(time.Hour)
	batch := storage.RouteSightingsBatch{GuildID: guildID, BucketStart: bucket, Observations: make([]storage.RouteSightingsObservation, 0, len(snapshot.Aircraft))}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.pruneLocked(bucket)
	for _, aircraft := range snapshot.Aircraft {
		callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
		if callsign == "" {
			continue
		}
		key := sightingKey(guildID, aircraft.ICAO, bucket)
		if _, exists := collector.seen[key]; exists {
			continue
		}
		route, found := routes.routeFor(aircraft.ICAO, callsign)
		if !found {
			continue
		}
		catalog, ok := storage.RouteCatalogFromDomain(route)
		if !ok {
			continue
		}
		collector.seen[key] = struct{}{}
		batch.Observations = append(batch.Observations, storage.RouteSightingsObservation{
			ICAO:     strings.ToUpper(strings.TrimSpace(aircraft.ICAO)),
			Callsign: callsign,
			Route:    catalog,
		})
	}
	if len(batch.Observations) == 0 {
		return storage.RouteSightingsBatch{}
	}
	return batch
}

func (collector *RouteStatsCollector) pruneLocked(bucket time.Time) {
	cutoff := bucket.Add(-2 * time.Hour).Format(time.RFC3339)
	for key := range collector.seen {
		parts := strings.Split(key, "|")
		if len(parts) != 3 || parts[2] < cutoff {
			delete(collector.seen, key)
		}
	}
}

func sightingKey(guildID uint64, icao string, bucket time.Time) string {
	return fmt.Sprintf("%d|%s|%s", guildID, strings.ToUpper(strings.TrimSpace(icao)), bucket.UTC().Truncate(time.Hour).Format(time.RFC3339))
}
