package report

import (
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

type RouteLookup interface {
	CachedRoute(callsign string) (domain.Route, bool, error)
}

type RouteStatsLookup struct {
	AdsbLol RouteLookup
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
	return domain.Route{}, false
}

type routeSightingKey struct {
	guildID uint64
	feeder  domain.FeederID
	icao    string
	bucket  int64
}

type RouteStatsCollector struct {
	mu   sync.Mutex
	seen map[routeSightingKey]struct{}
}

func NewRouteStatsCollector() *RouteStatsCollector {
	return &RouteStatsCollector{seen: make(map[routeSightingKey]struct{})}
}

func (collector *RouteStatsCollector) Observe(guildID uint64, snapshot *domain.Snapshot, routes RouteStatsLookup, now time.Time) storage.RouteSightingsBatch {
	if collector == nil || guildID == 0 || snapshot == nil {
		return storage.RouteSightingsBatch{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	bucket := now.UTC().Truncate(time.Hour)
	feederID := snapshot.FeederID
	if feederID == "" {
		feederID = domain.FeederLocal
	}
	batch := storage.RouteSightingsBatch{GuildID: guildID, FeederID: feederID, BucketStart: bucket, Observations: make([]storage.RouteSightingsObservation, 0, len(snapshot.Aircraft))}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.pruneLocked(bucket)
	for _, aircraft := range snapshot.Aircraft {
		callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
		if callsign == "" {
			continue
		}
		key := routeSightingKey{guildID: guildID, feeder: feederID, icao: strings.ToUpper(strings.TrimSpace(aircraft.ICAO)), bucket: bucket.Unix()}
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
	cutoff := bucket.Add(-2 * time.Hour).Unix()
	for key := range collector.seen {
		if key.bucket < cutoff {
			delete(collector.seen, key)
		}
	}
}
