package report

import (
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
	rollup := storage.ReportRollup{GuildID: guildID, BucketStart: now.Truncate(time.Hour), AircraftObservations: int64(len(snapshot.Aircraft)), PeakTracked: int64(len(snapshot.ByICAO))}
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
	}
	return rollup
}

func EmergencyEvent(guildID uint64, observedAt time.Time) storage.ReportRollup {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return storage.ReportRollup{GuildID: guildID, BucketStart: observedAt.UTC().Truncate(time.Hour), EmergencyEvents: 1}
}
