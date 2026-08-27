package state

import (
	"context"
	"sort"
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
	if snapshot == nil {
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
	sort.Slice(result, func(left, right int) bool {
		if result[left].DisplayName != result[right].DisplayName {
			return result[left].DisplayName < result[right].DisplayName
		}
		return result[left].ID < result[right].ID
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
	feeders := manager.ListFeeders()
	manager.mu.RLock()
	snapshots := make([]*domain.Snapshot, 0, len(feeders))
	for _, summary := range feeders {
		entry := manager.feeders[summary.ID]
		if entry == nil || !entry.descriptor.Enabled {
			continue
		}
		if snapshot := entry.snapshot.Load(); snapshot != nil {
			snapshots = append(snapshots, snapshot)
		}
	}
	manager.mu.RUnlock()

	type selected struct {
		aircraft domain.Aircraft
		seenAt   time.Time
	}
	chosen := make(map[string]selected)
	seenBy := make(map[string][]domain.FeederID)
	emergencyByICAO := make(map[string]domain.Aircraft)
	var fetchedAt, generatedAt time.Time
	var messages uint64
	messageValid := false
	stats := domain.Statistics{}
	capabilities := domain.Capabilities(0)
	health := aggregateHealth(snapshots)
	for _, snapshot := range snapshots {
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
		for _, aircraft := range snapshot.Aircraft {
			icao := strings.ToUpper(strings.TrimSpace(aircraft.ICAO))
			if icao == "" {
				continue
			}
			seenBy[icao] = appendUniqueFeeder(seenBy[icao], snapshot.FeederID)
			if domain.EmergencyActive(aircraft) {
				emergencyByICAO[icao] = aircraft
			}
			seenAt := snapshot.FetchedAt.Add(-max(aircraft.Seen, 0))
			current, exists := chosen[icao]
			if !exists || betterObservation(aircraft, seenAt, current.aircraft, current.seenAt) {
				copyValue := aircraft
				copyValue.ICAO = icao
				chosen[icao] = selected{aircraft: copyValue, seenAt: seenAt}
			}
		}
	}

	aircraft := make([]domain.Aircraft, 0, len(chosen))
	for icao, value := range chosen {
		if emergency, active := emergencyByICAO[icao]; active && !domain.EmergencyActive(value.aircraft) {
			value.aircraft.Emergency = emergency.Emergency
			value.aircraft.Squawk = emergency.Squawk
		}
		value.aircraft.SeenBy = append([]domain.FeederID(nil), seenBy[icao]...)
		sort.Slice(value.aircraft.SeenBy, func(left, right int) bool { return value.aircraft.SeenBy[left] < value.aircraft.SeenBy[right] })
		aircraft = append(aircraft, value.aircraft)
	}
	sort.Slice(aircraft, func(left, right int) bool { return aircraft[left].ICAO < aircraft[right].ICAO })
	byICAO := make(map[string]int, len(aircraft))
	search := make([]domain.AircraftKey, 0, len(aircraft))
	for index := range aircraft {
		byICAO[aircraft[index].ICAO] = index
		search = append(search, domain.AircraftKey{ICAO: aircraft[index].ICAO, Callsign: aircraft[index].Callsign, Registration: aircraft[index].Registration})
	}
	sort.Slice(search, func(left, right int) bool {
		if search[left].Callsign != search[right].Callsign {
			return search[left].Callsign < search[right].Callsign
		}
		if search[left].Registration != search[right].Registration {
			return search[left].Registration < search[right].Registration
		}
		return search[left].ICAO < search[right].ICAO
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

func betterObservation(candidate domain.Aircraft, candidateAt time.Time, current domain.Aircraft, currentAt time.Time) bool {
	if candidateAt.After(currentAt) {
		return true
	}
	if candidateAt.Before(currentAt) {
		return false
	}
	if candidate.HasPosition != current.HasPosition {
		return candidate.HasPosition
	}
	return candidate.Messages > current.Messages
}

func appendUniqueFeeder(values []domain.FeederID, value domain.FeederID) []domain.FeederID {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
