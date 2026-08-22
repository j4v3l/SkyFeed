package rules

import (
	"fmt"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestRuleCooldownConsecutiveAndRestartState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rule := domain.WatchRule{ID: 1, GuildID: 2, UserID: 3, Type: domain.RuleICAO, Value: "abc123", Enabled: true, MinimumObservations: 2, Cooldown: time.Hour}
	engine := NewEngine([]domain.WatchRule{rule}, nil)
	snapshot := ruleSnapshot(now, domain.Aircraft{ICAO: "ABC123"})
	alerts, updates := engine.Evaluate(snapshot)
	if len(alerts) != 0 || len(updates) != 1 {
		t.Fatalf("first alerts=%+v updates=%+v", alerts, updates)
	}
	snapshot.PublishedAt = now.Add(time.Second)
	alerts, updates = engine.Evaluate(snapshot)
	if len(alerts) != 1 || !updates[0].Active {
		t.Fatalf("second alerts=%+v updates=%+v", alerts, updates)
	}
	restarted := NewEngine([]domain.WatchRule{rule}, updates)
	snapshot.PublishedAt = now.Add(2 * time.Second)
	alerts, _ = restarted.Evaluate(snapshot)
	if len(alerts) != 0 {
		t.Fatalf("restart produced alert storm: %+v", alerts)
	}
}

func TestEmergencyIsImmediateAndIndependent(t *testing.T) {
	engine := NewEngine(nil, nil)
	now := time.Now()
	alerts, updates := engine.Evaluate(ruleSnapshot(now, domain.Aircraft{ICAO: "ABC123", Squawk: "7700"}))
	if len(alerts) != 1 || alerts[0].Priority != domain.AlertEmergency {
		t.Fatalf("alerts=%+v", alerts)
	}
	restarted := NewEngine(nil, updates)
	alerts, _ = restarted.Evaluate(ruleSnapshot(now.Add(time.Second), domain.Aircraft{ICAO: "ABC123", Squawk: "7700"}))
	if len(alerts) != 0 {
		t.Fatalf("restart duplicated emergency: %+v", alerts)
	}
}

func TestEngineEnrichmentRuleIsBestEffortAndNotEmergency(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rule := domain.WatchRule{ID: 9, GuildID: 42, Type: domain.RuleOperator, Value: "SKY AIR", Enabled: true, BestEffortEnrichment: true, MinimumObservations: 1}
	engine := NewEngine([]domain.WatchRule{rule}, nil)
	aircraft := domain.Aircraft{ICAO: "ABC123"}
	value := domain.Enrichment{ICAO: aircraft.ICAO, Found: true, Aircraft: &domain.AircraftMetadata{OperatorFlag: "Sky Air"}}
	alerts, updates := engine.EvaluateEnrichment(value, aircraft, now)
	if len(alerts) != 1 || alerts[0].Priority != domain.AlertNormal || alerts[0].Type != domain.RuleOperator {
		t.Fatalf("alerts=%+v", alerts)
	}
	if len(updates) == 0 || !updates[len(updates)-1].Active {
		t.Fatalf("updates=%+v", updates)
	}
	alerts, _ = engine.EvaluateEnrichment(value, aircraft, now.Add(time.Minute))
	if len(alerts) != 0 {
		t.Fatalf("duplicate alerts=%+v", alerts)
	}
}

func TestRadiusHysteresis(t *testing.T) {
	rule := domain.WatchRule{ID: 1, Type: domain.RuleRadius, Enabled: true, MinimumObservations: 1, EnterThreshold: 10, ExitThreshold: 12}
	engine := NewEngine([]domain.WatchRule{rule}, nil)
	now := time.Now()
	alerts, _ := engine.Evaluate(ruleSnapshot(now, domain.Aircraft{ICAO: "ABC123", HasDistance: true, DistanceNM: 9}))
	if len(alerts) != 1 {
		t.Fatalf("enter alerts=%d", len(alerts))
	}
	alerts, _ = engine.Evaluate(ruleSnapshot(now.Add(time.Second), domain.Aircraft{ICAO: "ABC123", HasDistance: true, DistanceNM: 11}))
	if len(alerts) != 0 {
		t.Fatalf("hysteresis refired: %+v", alerts)
	}
	engine.Evaluate(ruleSnapshot(now.Add(2*time.Second), domain.Aircraft{ICAO: "ABC123", HasDistance: true, DistanceNM: 13}))
	alerts, _ = engine.Evaluate(ruleSnapshot(now.Add(3*time.Second), domain.Aircraft{ICAO: "ABC123", HasDistance: true, DistanceNM: 9}))
	if len(alerts) != 1 {
		t.Fatalf("re-entry alerts=%d", len(alerts))
	}
}

func BenchmarkRuleEngine(b *testing.B) {
	rules := make([]domain.WatchRule, 5_000)
	for index := range rules {
		rules[index] = domain.WatchRule{ID: int64(index + 1), Type: domain.RuleICAO, Value: fmt.Sprintf("%06X", index), Enabled: true, MinimumObservations: 2, Cooldown: time.Minute}
	}
	aircraft := make([]domain.Aircraft, 1_000)
	byICAO := make(map[string]int, len(aircraft))
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index)}
		byICAO[aircraft[index].ICAO] = index
	}
	snapshot := &domain.Snapshot{PublishedAt: time.Unix(1_700_000_000, 0), Aircraft: aircraft, ByICAO: byICAO}
	engine := NewEngine(rules, nil)
	b.ResetTimer()
	for b.Loop() {
		engine.Evaluate(snapshot)
	}
}

func ruleSnapshot(now time.Time, aircraft domain.Aircraft) *domain.Snapshot {
	return &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{aircraft}, ByICAO: map[string]int{aircraft.ICAO: 0}}
}
