package rules

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type feederState struct {
	offline         bool
	recoverySamples int
}

type FeederMonitor struct {
	mu     sync.Mutex
	states map[string]feederState
}

func NewFeederMonitor() *FeederMonitor { return &FeederMonitor{states: make(map[string]feederState)} }

// Restore consumes newest-first durable feeder fingerprints and restores one
// state per source. Unknown historical values are ignored.
func (monitor *FeederMonitor) Restore(fingerprints []string) {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	for _, fingerprint := range fingerprints {
		parts := strings.Split(fingerprint, ":")
		if len(parts) != 3 || parts[0] != "feeder" {
			continue
		}
		if _, exists := monitor.states[parts[2]]; exists {
			continue
		}
		switch parts[1] {
		case "offline":
			monitor.states[parts[2]] = feederState{offline: true}
		case "recovered":
			monitor.states[parts[2]] = feederState{}
		}
	}
}

func (monitor *FeederMonitor) Evaluate(guildID uint64, snapshot *domain.Snapshot) []domain.Alert {
	if snapshot == nil {
		return nil
	}
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = time.Now()
	}
	sources := []struct {
		name   string
		health domain.SourceHealth
	}{
		{name: "aircraft", health: snapshot.Health.Aircraft},
		{name: "receiver", health: snapshot.Health.Receiver},
		{name: "stats", health: snapshot.Health.Stats},
	}
	alerts := make([]domain.Alert, 0, 1)
	for _, source := range sources {
		if source.health.Status == domain.HealthDisabled {
			delete(monitor.states, source.name)
			continue
		}
		state := monitor.states[source.name]
		if source.health.Status == domain.HealthOffline {
			state.recoverySamples = 0
			if !state.offline {
				state.offline = true
				priority := domain.AlertNormal
				if source.name == "aircraft" {
					priority = domain.AlertEmergency
				}
				alerts = append(alerts, domain.Alert{ID: "feeder-offline:" + source.name + ":" + strconv.FormatInt(now.Unix(), 10), GuildID: guildID, Type: domain.RuleFeeder, Priority: priority, Title: "Feeder source offline", Description: source.name + " source is offline", ConditionFingerprint: "feeder:offline:" + source.name, ObservedAt: now})
			}
		} else if state.offline && source.health.Status == domain.HealthHealthy {
			state.recoverySamples++
			if state.recoverySamples >= 2 {
				state.offline = false
				state.recoverySamples = 0
				alerts = append(alerts, domain.Alert{ID: "feeder-recovered:" + source.name + ":" + strconv.FormatInt(now.Unix(), 10), GuildID: guildID, Type: domain.RuleFeeder, Priority: domain.AlertNormal, Title: "Feeder source recovered", Description: source.name + " source recovered and is stable", ConditionFingerprint: "feeder:recovered:" + source.name, ObservedAt: now})
			}
		} else if source.health.Status != domain.HealthHealthy {
			state.recoverySamples = 0
		}
		monitor.states[source.name] = state
	}
	return alerts
}
