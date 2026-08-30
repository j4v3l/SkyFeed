package state

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const DefaultAggregateInterval = 250 * time.Millisecond

type SnapshotReader interface {
	Current() *domain.Snapshot
	Aggregate() *domain.Snapshot
	Feeder(domain.FeederID) (*domain.Snapshot, bool)
	ListFeeders() []domain.FeederSummary
}

type feederEntry struct {
	descriptor domain.FeederDescriptor
	snapshot   atomic.Pointer[domain.Snapshot]
}

// FeederManager owns dynamic per-feeder snapshots and a coalesced immutable
// community view. A publication never waits for aggregate construction.
type FeederManager struct {
	mu                sync.RWMutex
	feeders           map[domain.FeederID]*feederEntry
	current           atomic.Pointer[domain.Snapshot]
	dirty             chan struct{}
	interval          time.Duration
	now               func() time.Time
	sequence          atomic.Uint64
	uniqueHint        atomic.Int64
	observer          func(domain.FeederID, *domain.Snapshot)
	aggregateObserver func(*domain.Snapshot)
}

func NewFeederManager(interval time.Duration) *FeederManager {
	if interval <= 0 {
		interval = DefaultAggregateInterval
	}
	manager := &FeederManager{
		feeders:  make(map[domain.FeederID]*feederEntry),
		dirty:    make(chan struct{}, 1),
		interval: interval,
		now:      time.Now,
	}
	manager.current.Store(emptyAggregate(time.Now().UTC()))
	return manager
}

func emptyAggregate(now time.Time) *domain.Snapshot {
	return &domain.Snapshot{
		FeederID: domain.FeederAll, ActiveProvider: domain.ProviderUnknown, PublishedAt: now,
		Aircraft: []domain.Aircraft{}, ByICAO: map[string]int{}, Search: []domain.AircraftKey{}, Feeders: []domain.FeederSummary{},
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
			Receiver: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthDisabled},
			Stats:    domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
		},
	}
}

func (manager *FeederManager) Register(descriptor domain.FeederDescriptor) error {
	id, err := domain.NormalizeFeederID(string(descriptor.ID))
	if err != nil {
		return err
	}
	if id == domain.FeederAll {
		return &invalidFeederError{id: id}
	}
	descriptor.ID = id
	descriptor.DisplayName = strings.TrimSpace(descriptor.DisplayName)
	if descriptor.DisplayName == "" {
		descriptor.DisplayName = string(id)
	}
	descriptor.DisplayName, err = domain.NormalizeFeederDisplayName(descriptor.DisplayName)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	entry := manager.feeders[id]
	if entry == nil {
		entry = &feederEntry{}
		manager.feeders[id] = entry
	}
	entry.descriptor = descriptor
	manager.mu.Unlock()
	manager.markDirty()
	return nil
}

type invalidFeederError struct{ id domain.FeederID }

func (err *invalidFeederError) Error() string {
	return "reserved feeder ID cannot be registered: " + string(err.id)
}

func (manager *FeederManager) Remove(id domain.FeederID) {
	manager.mu.Lock()
	delete(manager.feeders, id)
	manager.mu.Unlock()
	manager.markDirty()
}

func (manager *FeederManager) Publish(id domain.FeederID, snapshot *domain.Snapshot) bool {
	if snapshot == nil || !validSnapshotAircraft(snapshot.Aircraft) {
		return false
	}
	manager.mu.RLock()
	entry := manager.feeders[id]
	manager.mu.RUnlock()
	if entry == nil || !entry.descriptor.Enabled {
		return false
	}
	next := *snapshot
	next.FeederID = id
	next.Feeders = nil
	entry.snapshot.Store(&next)
	manager.markDirty()
	if manager.observer != nil {
		manager.observer(id, &next)
	}
	return true
}

// Observers must be installed during application startup before Run or Publish.
func (manager *FeederManager) SetPublishObserver(observer func(domain.FeederID, *domain.Snapshot)) {
	manager.observer = observer
}

func (manager *FeederManager) SetAggregateObserver(observer func(*domain.Snapshot)) {
	manager.aggregateObserver = observer
}

func (manager *FeederManager) markDirty() {
	select {
	case manager.dirty <- struct{}{}:
	default:
	}
}

func (manager *FeederManager) Current() *domain.Snapshot   { return manager.current.Load() }
func (manager *FeederManager) Aggregate() *domain.Snapshot { return manager.current.Load() }

func (manager *FeederManager) Feeder(id domain.FeederID) (*domain.Snapshot, bool) {
	if id == domain.FeederAll {
		return manager.Aggregate(), true
	}
	manager.mu.RLock()
	entry := manager.feeders[id]
	manager.mu.RUnlock()
	if entry == nil {
		return nil, false
	}
	snapshot := entry.snapshot.Load()
	return snapshot, snapshot != nil
}

func (manager *FeederManager) ListFeeders() []domain.FeederSummary {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	result := make([]domain.FeederSummary, 0, len(manager.feeders))
	for _, entry := range manager.feeders {
		summary := domain.FeederSummary{FeederDescriptor: entry.descriptor, Health: domain.HealthUnknown}
		if snapshot := entry.snapshot.Load(); snapshot != nil {
			summary.Health = snapshot.Health.Aircraft.Status
			summary.LastPublished = snapshot.PublishedAt
			summary.Aircraft = len(snapshot.Aircraft)
		}
		result = append(result, summary)
	}
	slices.SortFunc(result, func(left, right domain.FeederSummary) int {
		if left.DisplayName != right.DisplayName {
			return strings.Compare(left.DisplayName, right.DisplayName)
		}
		return strings.Compare(string(left.ID), string(right.ID))
	})
	return result
}

func (manager *FeederManager) Run(ctx context.Context) error {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	pending := false
	for {
		select {
		case <-ctx.Done():
			if pending {
				manager.Rebuild()
			}
			return nil
		case <-manager.dirty:
			if !pending {
				pending = true
				timer.Reset(manager.interval)
			}
		case <-timer.C:
			manager.Rebuild()
			pending = false
		}
	}
}

// Rebuild is public for deterministic startup and benchmark tests. Normal
// ingestion uses Run, which coalesces bursts to at most four builds per second.
func (manager *FeederManager) Rebuild() *domain.Snapshot {
	type aggregateInput struct {
		descriptor domain.FeederDescriptor
		snapshot   *domain.Snapshot
	}
	manager.mu.RLock()
	inputs := make([]aggregateInput, 0, len(manager.feeders))
	feeders := make([]domain.FeederSummary, 0, len(manager.feeders))
	totalObservations := 0
	for _, entry := range manager.feeders {
		snapshot := entry.snapshot.Load()
		summary := domain.FeederSummary{FeederDescriptor: entry.descriptor, Health: domain.HealthUnknown}
		if snapshot != nil {
			summary.Health = snapshot.Health.Aircraft.Status
			summary.LastPublished = snapshot.PublishedAt
			summary.Aircraft = len(snapshot.Aircraft)
		}
		feeders = append(feeders, summary)
		if entry.descriptor.Enabled && snapshot != nil {
			inputs = append(inputs, aggregateInput{descriptor: entry.descriptor, snapshot: snapshot})
			totalObservations += len(snapshot.Aircraft)
		}
	}
	manager.mu.RUnlock()
	slices.SortFunc(feeders, func(left, right domain.FeederSummary) int {
		if left.DisplayName != right.DisplayName {
			return strings.Compare(left.DisplayName, right.DisplayName)
		}
		return strings.Compare(string(left.ID), string(right.ID))
	})
	// Processing inputs by feeder ID makes every SeenBy slice deterministic, so
	// no per-aircraft sort or temporary feeder slice is required.
	slices.SortFunc(inputs, func(left, right aggregateInput) int {
		return strings.Compare(string(left.descriptor.ID), string(right.descriptor.ID))
	})

	snapshots := make([]*domain.Snapshot, len(inputs))
	for index := range inputs {
		snapshots[index] = inputs[index].snapshot
	}

	type selected struct {
		snapshot          *domain.Snapshot
		emergencySnapshot *domain.Snapshot
		seenAt            int64
		emergencyAt       int64
		aircraftIndex     uint32
		emergencyIndex    uint32
		seenOffset        uint32
		seenCount         uint32
		seenCursor        uint32
	}
	capacity := totalObservations
	if hint := int(manager.uniqueHint.Load()); hint > 0 && hint < capacity {
		// Leave measured headroom for normal churn without sizing the map for all
		// overlapping observations.
		capacity = min(totalObservations, hint+max(hint/4, 64))
	}
	chosen := make(map[string]selected, capacity)
	keys := make([]string, 0, capacity)
	var fetchedAt, generatedAt time.Time
	var messages uint64
	messageValid := false
	stats := domain.Statistics{}
	capabilities := domain.Capabilities(0)
	health := aggregateHealth(snapshots)
	for _, input := range inputs {
		snapshot := input.snapshot
		capabilities |= snapshot.Capabilities
		if snapshot.FetchedAt.After(fetchedAt) {
			fetchedAt = snapshot.FetchedAt
		}
		if snapshot.SourceGeneratedAt.After(generatedAt) {
			generatedAt = snapshot.SourceGeneratedAt
		}
		if snapshot.MessageCounterValid {
			messages += snapshot.ReceiverMessages
			messageValid = true
		}
		stats.Messages += snapshot.Statistics.Messages
		stats.MessageRate += snapshot.Statistics.MessageRate
		stats.MaxRangeNM = max(stats.MaxRangeNM, snapshot.Statistics.MaxRangeNM)
		for aircraftIndex := range snapshot.Aircraft {
			aircraft := snapshot.Aircraft[aircraftIndex]
			icao := aircraft.ICAO
			seenAt := snapshot.FetchedAt.Add(-max(aircraft.Seen, 0)).UnixNano()
			current, exists := chosen[icao]
			if !exists {
				keys = append(keys, icao)
				current = selected{snapshot: snapshot, aircraftIndex: uint32(aircraftIndex), seenAt: seenAt}
			}
			current.seenCount++
			if domain.EmergencyActive(aircraft) && (current.emergencySnapshot == nil || seenAt > current.emergencyAt) {
				current.emergencySnapshot = snapshot
				current.emergencyIndex = uint32(aircraftIndex)
				current.emergencyAt = seenAt
			}
			if !exists || betterObservationAt(aircraft, seenAt, current.snapshot.Aircraft[current.aircraftIndex], current.seenAt) {
				current.snapshot = snapshot
				current.aircraftIndex = uint32(aircraftIndex)
				current.seenAt = seenAt
			}
			chosen[icao] = current
		}
	}
	manager.uniqueHint.Store(int64(len(chosen)))
	slices.Sort(keys)
	totalSeen := 0
	for _, icao := range keys {
		value := chosen[icao]
		value.seenOffset = uint32(totalSeen)
		totalSeen += int(value.seenCount)
		chosen[icao] = value
	}
	seenArena := make([]domain.FeederID, totalSeen)
	for _, input := range inputs {
		for _, item := range input.snapshot.Aircraft {
			value := chosen[item.ICAO]
			seenArena[int(value.seenOffset)+int(value.seenCursor)] = input.descriptor.ID
			value.seenCursor++
			chosen[item.ICAO] = value
		}
	}

	aircraft := make([]domain.Aircraft, len(keys))
	byICAO := make(map[string]int, len(keys))
	search := make([]domain.AircraftKey, len(keys))
	for index, icao := range keys {
		value := chosen[icao]
		item := value.snapshot.Aircraft[value.aircraftIndex]
		if !domain.EmergencyActive(item) && value.emergencySnapshot != nil {
			emergency := value.emergencySnapshot.Aircraft[value.emergencyIndex]
			item.Emergency = emergency.Emergency
			item.Squawk = emergency.Squawk
		}
		seenStart := int(value.seenOffset)
		seenEnd := seenStart + int(value.seenCount)
		item.SeenBy = seenArena[seenStart:seenEnd:seenEnd]
		aircraft[index] = item
		byICAO[icao] = index
		search[index] = domain.AircraftKey{ICAO: icao, Callsign: item.Callsign, Registration: item.Registration}
	}
	slices.SortFunc(search, func(left, right domain.AircraftKey) int {
		if left.Callsign != right.Callsign {
			return strings.Compare(left.Callsign, right.Callsign)
		}
		if left.Registration != right.Registration {
			return strings.Compare(left.Registration, right.Registration)
		}
		return strings.Compare(left.ICAO, right.ICAO)
	})
	stats.TrackedAircraft = len(aircraft)
	stats.FetchedAt = fetchedAt
	now := manager.now().UTC()
	next := &domain.Snapshot{
		FeederID: domain.FeederAll, Feeders: feeders, Sequence: manager.sequence.Add(1), ActiveProvider: domain.ProviderUnknown,
		Capabilities: capabilities, SourceGeneratedAt: generatedAt, FetchedAt: fetchedAt, PublishedAt: now,
		Statistics: stats, ReceiverMessages: messages, MessageCounterValid: messageValid,
		Aircraft: aircraft, ByICAO: byICAO, Search: search, Health: health,
	}
	manager.current.Store(next)
	if manager.aggregateObserver != nil {
		manager.aggregateObserver(next)
	}
	return next
}

func betterObservationAt(candidate domain.Aircraft, candidateAt int64, current domain.Aircraft, currentAt int64) bool {
	if candidateAt > currentAt {
		return true
	}
	if candidateAt < currentAt {
		return false
	}
	if candidate.HasPosition != current.HasPosition {
		return candidate.HasPosition
	}
	return candidate.Messages > current.Messages
}

func validSnapshotAircraft(aircraft []domain.Aircraft) bool {
	previous := ""
	for index := range aircraft {
		icao := aircraft[index].ICAO
		if icao == "" || icao != strings.TrimSpace(icao) || icao != strings.ToUpper(icao) || (previous != "" && previous >= icao) {
			return false
		}
		previous = icao
	}
	return true
}

func aggregateHealth(snapshots []*domain.Snapshot) domain.Health {
	result := domain.Health{
		Aircraft: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
		Receiver: domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthDisabled},
		Stats:    domain.SourceHealth{Provider: domain.ProviderUnknown, Status: domain.HealthUnknown},
	}
	if len(snapshots) == 0 {
		return result
	}
	result.Aircraft.Status = domain.HealthHealthy
	result.Stats.Status = domain.HealthHealthy
	for _, snapshot := range snapshots {
		result.Aircraft.LastAttempt = later(result.Aircraft.LastAttempt, snapshot.Health.Aircraft.LastAttempt)
		result.Aircraft.LastSuccess = later(result.Aircraft.LastSuccess, snapshot.Health.Aircraft.LastSuccess)
		result.Stats.LastAttempt = later(result.Stats.LastAttempt, snapshot.Health.Stats.LastAttempt)
		result.Stats.LastSuccess = later(result.Stats.LastSuccess, snapshot.Health.Stats.LastSuccess)
		if snapshot.Health.Aircraft.Status != domain.HealthHealthy {
			result.Aircraft.Status = domain.HealthDegraded
		}
		if snapshot.Health.Stats.Status != domain.HealthHealthy && snapshot.Health.Stats.Status != domain.HealthDisabled {
			result.Stats.Status = domain.HealthDegraded
		}
	}
	return result
}

func later(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
