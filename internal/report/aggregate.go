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
	mu     sync.Mutex
	states map[domain.FeederID]messageState
}

type messageState struct {
	lastMessages uint64
	hasMessages  bool
	lastProvider domain.ProviderID
	lastSample   time.Time
}

func NewAggregator() *Aggregator { return &Aggregator{states: make(map[domain.FeederID]messageState)} }

func (aggregator *Aggregator) Observe(guildID uint64, snapshot *domain.Snapshot) storage.ReportRollup {
	rollup, _ := aggregator.ObserveSampled(guildID, snapshot, 0)
	return rollup
}

// ObserveSampled prevents a coalesced aggregate view from inflating aircraft
// observations when many feeder publications trigger multiple builds per
// second. Message deltas remain measured from the last accepted sample.
func (aggregator *Aggregator) ObserveSampled(guildID uint64, snapshot *domain.Snapshot, minimumInterval time.Duration) (storage.ReportRollup, bool) {
	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()
	now := snapshot.PublishedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	scope := snapshot.FeederID
	if scope == "" {
		scope = domain.FeederAll
	}
	state := aggregator.states[scope]
	if minimumInterval > 0 && !state.lastSample.IsZero() && now.Sub(state.lastSample) < minimumInterval {
		return storage.ReportRollup{}, false
	}
	rollup := storage.ReportRollup{GuildID: guildID, FeederScope: scope, BucketStart: now.Truncate(time.Hour), AircraftObservations: int64(len(snapshot.Aircraft)), PeakTracked: int64(len(snapshot.ByICAO))}
	if snapshot.MessageCounterValid {
		if state.hasMessages &&
			snapshot.ActiveProvider == state.lastProvider &&
			snapshot.ReceiverMessages >= state.lastMessages {
			rollup.Messages = int64(snapshot.ReceiverMessages - state.lastMessages)
		}
		state.lastMessages = snapshot.ReceiverMessages
		state.lastProvider = snapshot.ActiveProvider
		state.hasMessages = true
	} else {
		state.hasMessages = false
	}
	state.lastSample = now
	aggregator.states[scope] = state
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.HasDistance && aircraft.DistanceNM > rollup.MaximumRange {
			rollup.MaximumRange = aircraft.DistanceNM
		}
	}
	return rollup, true
}

func EmergencyEvent(guildID uint64, observedAt time.Time) storage.ReportRollup {
	return EmergencyEventForScope(guildID, domain.FeederAll, observedAt)
}

func EmergencyEventForScope(guildID uint64, scope domain.FeederID, observedAt time.Time) storage.ReportRollup {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if scope == "" {
		scope = domain.FeederAll
	}
	return storage.ReportRollup{GuildID: guildID, FeederScope: scope, BucketStart: observedAt.UTC().Truncate(time.Hour), EmergencyEvents: 1}
}
