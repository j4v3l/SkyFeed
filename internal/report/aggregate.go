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
	lastProvider domain.ProviderID
}

func NewAggregator() *Aggregator { return &Aggregator{} }

func (aggregator *Aggregator) Observe(guildID uint64, snapshot *domain.Snapshot) storage.ReportRollup {
	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()
	now := snapshot.PublishedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rollup := storage.ReportRollup{GuildID: guildID, BucketStart: now.Truncate(time.Hour), AircraftSeen: int64(len(snapshot.Aircraft)), DistinctICAOs: int64(len(snapshot.ByICAO))}
	if snapshot.MessageCounterValid {
		if aggregator.hasMessages &&
			snapshot.ActiveProvider == aggregator.lastProvider &&
			snapshot.ReceiverMessages >= aggregator.lastMessages {
			rollup.Messages = int64(snapshot.ReceiverMessages - aggregator.lastMessages)
		}
		aggregator.lastMessages = snapshot.ReceiverMessages
		aggregator.lastProvider = snapshot.ActiveProvider
		aggregator.hasMessages = true
	} else {
		aggregator.hasMessages = false
	}
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.HasDistance && aircraft.DistanceNM > rollup.MaximumRange {
			rollup.MaximumRange = aircraft.DistanceNM
		}
		if isEmergency(aircraft) {
			rollup.Emergencies++
		}
	}
	return rollup
}

func isEmergency(aircraft domain.Aircraft) bool {
	emergency := strings.ToLower(strings.TrimSpace(aircraft.Emergency))
	return aircraft.Squawk == "7500" || aircraft.Squawk == "7600" || aircraft.Squawk == "7700" || (emergency != "" && emergency != "none")
}
