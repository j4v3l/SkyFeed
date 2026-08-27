package rules

import (
	"sort"
	"sync"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const maxMovementStateGlobal = 25_000

// MovementRegistry isolates trend inference by feeder. It also supplies a
// bounded aggregate activity view without mixing samples between receivers.
type MovementRegistry struct {
	mu       sync.RWMutex
	monitors map[domain.FeederID]*MovementMonitor
}

func NewMovementRegistry() *MovementRegistry {
	return &MovementRegistry{monitors: make(map[domain.FeederID]*MovementMonitor)}
}

func (registry *MovementRegistry) Register(id domain.FeederID, config MovementConfig) {
	if !id.Valid() || id == domain.FeederAll {
		return
	}
	registry.mu.Lock()
	if current := registry.monitors[id]; current != nil && current.config == config {
		registry.mu.Unlock()
		return
	}
	registry.monitors[id] = NewMovementMonitor(config)
	registry.mu.Unlock()
}

func (registry *MovementRegistry) Remove(id domain.FeederID) {
	registry.mu.Lock()
	delete(registry.monitors, id)
	registry.mu.Unlock()
}

func (registry *MovementRegistry) Evaluate(guildID uint64, snapshot *domain.Snapshot) []domain.Alert {
	if snapshot == nil || snapshot.FeederID == "" || snapshot.FeederID == domain.FeederAll {
		return nil
	}
	registry.mu.RLock()
	monitor := registry.monitors[snapshot.FeederID]
	registry.mu.RUnlock()
	if monitor == nil {
		return nil
	}
	alerts := monitor.Evaluate(guildID, snapshot)
	for index := range alerts {
		alerts[index].FeederID = snapshot.FeederID
	}
	registry.enforceGlobalBound()
	return alerts
}

func (registry *MovementRegistry) Activity() domain.AirportActivity {
	return registry.ActivityFor(domain.FeederAll)
}

func (registry *MovementRegistry) ActivityFor(id domain.FeederID) domain.AirportActivity {
	registry.mu.RLock()
	if id != "" && id != domain.FeederAll {
		monitor := registry.monitors[id]
		registry.mu.RUnlock()
		if monitor == nil {
			return domain.AirportActivity{}
		}
		return monitor.Activity()
	}
	monitors := make([]*MovementMonitor, 0, len(registry.monitors))
	for _, monitor := range registry.monitors {
		monitors = append(monitors, monitor)
	}
	registry.mu.RUnlock()
	result := domain.AirportActivity{Configured: len(monitors) > 0}
	for _, monitor := range monitors {
		activity := monitor.Activity()
		if !activity.Configured {
			continue
		}
		if result.AirportCode == "" {
			result.AirportCode = activity.AirportCode
		} else if result.AirportCode != activity.AirportCode {
			result.AirportCode = "ALL"
		}
		if activity.UpdatedAt.After(result.UpdatedAt) {
			result.UpdatedAt = activity.UpdatedAt
		}
		result.Movements = append(result.Movements, activity.Movements...)
	}
	sort.Slice(result.Movements, func(left, right int) bool {
		return result.Movements[left].ObservedAt.After(result.Movements[right].ObservedAt)
	})
	if len(result.Movements) > 100 {
		result.Movements = result.Movements[:100]
	}
	return result
}

func (registry *MovementRegistry) StateLen() int {
	registry.mu.RLock()
	monitors := make([]*MovementMonitor, 0, len(registry.monitors))
	for _, monitor := range registry.monitors {
		monitors = append(monitors, monitor)
	}
	registry.mu.RUnlock()
	total := 0
	for _, monitor := range monitors {
		total += monitor.StateLen()
	}
	return total
}

func (registry *MovementRegistry) enforceGlobalBound() {
	over := registry.StateLen() - maxMovementStateGlobal
	if over <= 0 {
		return
	}
	registry.mu.RLock()
	monitors := make([]*MovementMonitor, 0, len(registry.monitors))
	for _, monitor := range registry.monitors {
		monitors = append(monitors, monitor)
	}
	registry.mu.RUnlock()
	for over > 0 {
		progress := false
		for _, monitor := range monitors {
			if monitor.pruneOldest(1) == 1 {
				over--
				progress = true
				if over == 0 {
					break
				}
			}
		}
		if !progress {
			return
		}
	}
}
