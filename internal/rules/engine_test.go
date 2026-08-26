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

func TestEmergencyEventRequiresInactiveToActiveTransition(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	engine := NewEngine(nil, nil)
	alerts, _ := engine.Evaluate(ruleSnapshot(now, domain.Aircraft{ICAO: "ABC123", Squawk: "7700"}))
	if len(alerts) != 1 {
		t.Fatalf("initial alerts = %d", len(alerts))
	}
	alerts, _ = engine.Evaluate(ruleSnapshot(now.Add(time.Second), domain.Aircraft{ICAO: "ABC123", Squawk: "7600", Emergency: "general"}))
	if len(alerts) != 0 {
		t.Fatalf("compatible emergency-state update refired: %+v", alerts)
	}
	alerts, updates := engine.Evaluate(ruleSnapshot(now.Add(2*time.Second), domain.Aircraft{ICAO: "ABC123", Squawk: "1200", Emergency: "none"}))
	if len(alerts) != 0 || len(updates) != 1 || updates[0].Active {
		t.Fatalf("clear alerts=%+v updates=%+v", alerts, updates)
	}
	alerts, _ = engine.Evaluate(ruleSnapshot(now.Add(3*time.Second), domain.Aircraft{ICAO: "ABC123", Squawk: "7500"}))
	if len(alerts) != 1 {
		t.Fatalf("new transition alerts = %d", len(alerts))
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

func TestFirstSeenFiresAgainAfterQuietPeriod(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rule := domain.WatchRule{ID: 7, GuildID: 2, Type: domain.RuleFirstSeen, Enabled: true, MinimumObservations: 1, Cooldown: time.Hour}
	engine := NewEngine([]domain.WatchRule{rule}, nil)
	aircraft := domain.Aircraft{ICAO: "ABC123"}
	alerts, _ := engine.Evaluate(ruleSnapshot(now, aircraft))
	if len(alerts) != 1 {
		t.Fatalf("initial alerts = %d", len(alerts))
	}
	engine.Evaluate(&domain.Snapshot{PublishedAt: now.Add(time.Minute), ByICAO: map[string]int{}})
	alerts, _ = engine.Evaluate(ruleSnapshot(now.Add(2*time.Hour), aircraft))
	if len(alerts) != 1 {
		t.Fatalf("return-after-quiet alerts = %d", len(alerts))
	}
}

func TestRuleStatePrunesRemovedRulesAndUniqueICAOChurn(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rule := domain.WatchRule{ID: 8, Type: domain.RuleCallsignPrefix, Value: "SF", Enabled: true, MinimumObservations: 1}
	engine := NewEngine([]domain.WatchRule{rule}, nil)
	for batch := 0; batch < 200; batch++ {
		aircraft := make([]domain.Aircraft, 1_000)
		for index := range aircraft {
			aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", batch*1000+index), Callsign: "SF1"}
		}
		engine.Evaluate(&domain.Snapshot{PublishedAt: now.Add(time.Duration(batch) * time.Second), Aircraft: aircraft, ByICAO: map[string]int{}})
	}
	seen, states := engine.Sizes()
	if seen > maxSeenEntries || states > maxStateEntries {
		t.Fatalf("sizes seen=%d states=%d", seen, states)
	}
	engine.ReplaceRules(nil)
	_, states = engine.Sizes()
	if states != 0 {
		t.Fatalf("removed-rule states = %d", states)
	}
	removed, _ := engine.Prune(now.Add(8 * 24 * time.Hour))
	if removed == 0 {
		t.Fatal("unique ICAO state was not pruned")
	}
}

func BenchmarkRuleEngineHeterogeneous(b *testing.B) {
	rules := make([]domain.WatchRule, 0, 5_000)
	for index := 0; index < 4_850; index++ {
		rules = append(rules, domain.WatchRule{ID: int64(index + 1), Type: domain.RuleICAO, Value: fmt.Sprintf("%06X", index), Enabled: true, MinimumObservations: 2})
	}
	for index := 0; index < 50; index++ {
		rules = append(rules,
			domain.WatchRule{ID: int64(4_851 + index*3), Type: domain.RuleCallsignPrefix, Value: fmt.Sprintf("S%02d", index), Enabled: true, MinimumObservations: 2},
			domain.WatchRule{ID: int64(4_852 + index*3), Type: domain.RuleRadius, EnterThreshold: float64(5 + index*2), ExitThreshold: float64(6 + index*2), Enabled: true, MinimumObservations: 2},
			domain.WatchRule{ID: int64(4_853 + index*3), Type: domain.RuleAltitude, EnterThreshold: float64(index * 1_000), ExitThreshold: float64(index*1_000 + 10_000), Enabled: true, MinimumObservations: 2},
		)
	}
	aircraft := make([]domain.Aircraft, 1_000)
	byICAO := make(map[string]int, len(aircraft))
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index), Callsign: fmt.Sprintf("S%02d%03d", index%50, index), HasDistance: true, DistanceNM: float64(index % 150), HasAltitude: true, AltitudeFeet: index * 50}
		byICAO[aircraft[index].ICAO] = index
	}
	snapshot := &domain.Snapshot{PublishedAt: time.Unix(1_700_000_000, 0), Aircraft: aircraft, ByICAO: byICAO}
	engine := NewEngine(rules, nil)
	engine.Evaluate(snapshot)
	engine.Evaluate(snapshot)
	b.ResetTimer()
	for b.Loop() {
		engine.Evaluate(snapshot)
	}
}

func BenchmarkRuleEngineEnrichment(b *testing.B) {
	rules := make([]domain.WatchRule, 5_000)
	for index := range rules {
		rules[index] = domain.WatchRule{ID: int64(index + 1), Type: domain.RuleOperator, Value: fmt.Sprintf("OP%04d", index), Enabled: true, BestEffortEnrichment: true, MinimumObservations: 1}
	}
	engine := NewEngine(rules, nil)
	aircraft := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123"}
	value := domain.Enrichment{ICAO: aircraft.ICAO, Found: true, Aircraft: &domain.AircraftMetadata{OperatorFlag: "OP2500"}}
	engine.EvaluateEnrichment(value, aircraft, time.Unix(1_700_000_000, 0))
	b.ResetTimer()
	for b.Loop() {
		engine.EvaluateEnrichment(value, aircraft, time.Unix(1_700_000_000, 0))
	}
}

func BenchmarkRuleEngineUniqueICAOChurn(b *testing.B) {
	engine := NewEngine([]domain.WatchRule{{ID: 1, Type: domain.RuleCallsignPrefix, Value: "SF", Enabled: true, MinimumObservations: 2}}, nil)
	batches := make([]*domain.Snapshot, 10)
	for batch := range batches {
		aircraft := make([]domain.Aircraft, 100)
		byICAO := make(map[string]int, len(aircraft))
		for index := range aircraft {
			icao := fmt.Sprintf("%06X", batch*len(aircraft)+index)
			aircraft[index] = domain.Aircraft{ICAO: icao, Callsign: "SF1"}
			byICAO[icao] = index
		}
		batches[batch] = &domain.Snapshot{PublishedAt: time.Unix(1_700_000_000+int64(batch), 0), Aircraft: aircraft, ByICAO: byICAO}
	}
	b.ResetTimer()
	for iteration := 0; b.Loop(); iteration++ {
		engine.Evaluate(batches[iteration%len(batches)])
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
