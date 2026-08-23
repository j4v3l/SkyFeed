package rules

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/planealert"
)

type InterestingMonitor struct {
	mu     sync.Mutex
	seen   map[string]struct{}
	lookup func(string) (planealert.Record, bool)
}

func NewInterestingMonitor(lookup func(string) (planealert.Record, bool)) *InterestingMonitor {
	return &InterestingMonitor{seen: make(map[string]struct{}), lookup: lookup}
}

func (monitor *InterestingMonitor) Restore(icaos []string) {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	for _, icao := range icaos {
		monitor.seen[normalizeInterestingICAO(icao)] = struct{}{}
	}
}

func (monitor *InterestingMonitor) Evaluate(guildID uint64, snapshot *domain.Snapshot) []domain.Alert {
	if snapshot == nil || monitor.lookup == nil || guildID == 0 {
		return nil
	}
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = time.Now()
	}
	alerts := make([]domain.Alert, 0, 1)
	for _, aircraft := range snapshot.Aircraft {
		icao := normalizeInterestingICAO(aircraft.ICAO)
		if icao == "" {
			continue
		}
		if _, exists := monitor.seen[icao]; exists {
			continue
		}
		record, ok := monitor.lookup(icao)
		if !ok {
			continue
		}
		monitor.seen[icao] = struct{}{}
		title := firstNonEmpty(record.Operator, record.Type, record.Group, "Interesting aircraft")
		description := interestingDescription(record, aircraft)
		alerts = append(alerts, domain.Alert{
			ID:                   fmt.Sprintf("interesting:%s:%d", icao, now.UnixNano()),
			GuildID:              guildID,
			AircraftICAO:         icao,
			Callsign:             aircraft.Callsign,
			Type:                 domain.RuleInteresting,
			Priority:             domain.AlertNormal,
			Title:                title,
			Description:          description,
			ConditionFingerprint: "interesting:" + icao,
			ObservedAt:           now,
			InterestingGroup:     record.Group,
			InterestingOperator:  record.Operator,
			InterestingTags:      record.Tags(),
			InterestingLink:      record.Link,
			InterestingImage:     record.PrimaryImage(),
		})
	}
	return alerts
}

func interestingDescription(record planealert.Record, aircraft domain.Aircraft) string {
	parts := make([]string, 0, 4)
	if record.Group != "" {
		parts = append(parts, record.Group)
	}
	if record.Category != "" {
		parts = append(parts, record.Category)
	}
	if tags := record.Tags(); tags != "" {
		parts = append(parts, tags)
	}
	if aircraft.HasDistance {
		parts = append(parts, fmt.Sprintf("%.1f NM", aircraft.DistanceNM))
	}
	if len(parts) == 0 {
		return "Matched plane-alert-db reference entry."
	}
	return strings.Join(parts, " • ")
}

func normalizeInterestingICAO(icao string) string {
	return strings.ToUpper(strings.TrimSpace(icao))
}
