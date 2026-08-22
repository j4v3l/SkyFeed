package report

import (
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

// Aggregator retains only counters needed for hourly rollups. It never keeps a
// raw snapshot or observation history.
type Aggregator struct {
	mu           sync.Mutex
	lastMessages uint64
	hasMessages  bool
	emergencies  map[string]struct{}
}

func NewAggregator() *Aggregator { return &Aggregator{emergencies: make(map[string]struct{})} }

func (aggregator *Aggregator) Observe(guildID uint64, snapshot *domain.Snapshot) storage.ReportRollup {
	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()
	now := snapshot.PublishedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rollup := storage.ReportRollup{GuildID: guildID, BucketStart: now.Truncate(time.Hour), AircraftSeen: int64(len(snapshot.Aircraft)), DistinctICAOs: int64(len(snapshot.ByICAO))}
	if aggregator.hasMessages && snapshot.ReceiverMessages >= aggregator.lastMessages {
		rollup.Messages = int64(snapshot.ReceiverMessages - aggregator.lastMessages)
	}
	aggregator.lastMessages, aggregator.hasMessages = snapshot.ReceiverMessages, true
	nextEmergencies := make(map[string]struct{})
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.HasDistance && aircraft.DistanceNM > rollup.MaximumRange {
			rollup.MaximumRange = aircraft.DistanceNM
		}
		if isEmergency(aircraft) {
			nextEmergencies[aircraft.ICAO] = struct{}{}
			if _, active := aggregator.emergencies[aircraft.ICAO]; !active {
				rollup.Emergencies++
			}
		}
	}
	aggregator.emergencies = nextEmergencies
	return rollup
}

func isEmergency(aircraft domain.Aircraft) bool {
	emergency := strings.ToLower(strings.TrimSpace(aircraft.Emergency))
	return aircraft.Squawk == "7500" || aircraft.Squawk == "7600" || aircraft.Squawk == "7700" || (emergency != "" && emergency != "none")
}
