package rules

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type stateKey struct {
	ruleID      int64
	icao        string
	fingerprint string
}

type stateRecord struct {
	state        domain.AlertState
	seenSequence uint64
	bestEffort   bool
}

type Engine struct {
	mu       sync.Mutex
	index    atomic.Pointer[Index]
	states   map[stateKey]stateRecord
	seen     map[string]time.Time
	sequence uint64
	now      func() time.Time
}

func NewEngine(rules []domain.WatchRule, restored []domain.AlertState) *Engine {
	engine := &Engine{states: make(map[stateKey]stateRecord, len(restored)), seen: make(map[string]time.Time), now: time.Now}
	engine.index.Store(BuildIndex(rules))
	bestEffort := make(map[int64]bool, len(rules))
	for _, rule := range rules {
		bestEffort[rule.ID] = rule.BestEffortEnrichment || rule.Type == domain.RuleOperator || rule.Type == domain.RuleOwner || rule.Type == domain.RuleAircraftType
	}
	for _, state := range restored {
		key := stateKey{ruleID: state.RuleID, icao: state.AircraftICAO, fingerprint: state.ConditionFingerprint}
		engine.states[key] = stateRecord{state: state, bestEffort: bestEffort[state.RuleID]}
	}
	return engine
}

func (engine *Engine) ReplaceRules(rules []domain.WatchRule) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.index.Store(BuildIndex(rules))
}

func (engine *Engine) Evaluate(snapshot *domain.Snapshot) ([]domain.Alert, []domain.AlertState) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if snapshot == nil {
		return nil, nil
	}
	engine.sequence++
	sequence := engine.sequence
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = engine.now()
	}
	index := engine.index.Load()
	alerts := make([]domain.Alert, 0, 4)
	updates := make([]domain.AlertState, 0, 8)
	for _, aircraft := range snapshot.Aircraft {
		if _, exists := engine.seen[aircraft.ICAO]; !exists {
			engine.seen[aircraft.ICAO] = now
		}
		alerts, updates = engine.evaluateEmergency(aircraft, now, alerts, updates)
		if index == nil {
			continue
		}
		alerts, updates = engine.matchAll(index.icao[aircraft.ICAO], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.registration[aircraft.Registration], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.callsign[aircraft.Callsign], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.squawk[aircraft.Squawk], aircraft, now, sequence, alerts, updates)
		for _, rule := range index.prefixes {
			if strings.HasPrefix(aircraft.Callsign, rule.value) {
				alerts, updates = engine.match(rule, aircraft, true, now, sequence, alerts, updates)
			}
		}
		for _, rule := range index.radius {
			active := engine.active(rule, aircraft.ICAO)
			threshold := rule.rule.EnterThreshold
			if active && rule.rule.ExitThreshold > 0 {
				threshold = rule.rule.ExitThreshold
			}
			alerts, updates = engine.match(rule, aircraft, aircraft.HasDistance && threshold > 0 && aircraft.DistanceNM <= threshold, now, sequence, alerts, updates)
		}
		for _, rule := range index.altitude {
			matches := aircraft.HasAltitude && float64(aircraft.AltitudeFeet) >= rule.rule.EnterThreshold && (rule.rule.ExitThreshold <= 0 || float64(aircraft.AltitudeFeet) <= rule.rule.ExitThreshold)
			alerts, updates = engine.match(rule, aircraft, matches, now, sequence, alerts, updates)
		}
		for _, rule := range index.firstSeen {
			first := engine.seen[aircraft.ICAO]
			quiet := rule.rule.Cooldown
			matches := first.Equal(now) && (quiet == 0 || aircraft.Seen <= quiet)
			alerts, updates = engine.match(rule, aircraft, matches, now, sequence, alerts, updates)
		}
	}
	for key, record := range engine.states {
		if record.seenSequence == sequence || !record.state.Active {
			continue
		}
		if record.bestEffort {
			if _, visible := snapshot.ByICAO[key.icao]; visible {
				continue
			}
		}
		record.state.Active = false
		record.state.ConsecutiveMatches = 0
		record.state.LastClearAt = now
		engine.states[key] = record
		updates = append(updates, record.state)
	}
	return alerts, updates
}

// EvaluateEnrichment handles presentation metadata independently of live
// receiver evaluation. These rules are always best-effort and can never create
// emergency alerts.
func (engine *Engine) EvaluateEnrichment(value domain.Enrichment, aircraft domain.Aircraft, observedAt time.Time) ([]domain.Alert, []domain.AlertState) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if aircraft.ICAO == "" || value.ICAO == "" {
		return nil, nil
	}
	index := engine.index.Load()
	if index == nil {
		return nil, nil
	}
	if observedAt.IsZero() {
		observedAt = engine.now()
	}
	engine.sequence++
	sequence := engine.sequence
	alerts := make([]domain.Alert, 0, 1)
	updates := make([]domain.AlertState, 0, 2)
	metadata := value.Aircraft
	operator, owner, aircraftType := "", "", ""
	if value.Found && metadata != nil {
		operator = strings.ToUpper(strings.TrimSpace(metadata.OperatorFlag))
		owner = strings.ToUpper(strings.TrimSpace(metadata.Owner))
		aircraftType = strings.ToUpper(strings.TrimSpace(firstNonEmpty(metadata.ICAOType, metadata.AircraftType)))
	}
	for expected, rules := range index.operator {
		for _, rule := range rules {
			alerts, updates = engine.match(rule, aircraft, operator != "" && operator == expected, observedAt, sequence, alerts, updates)
		}
	}
	for expected, rules := range index.owner {
		for _, rule := range rules {
			alerts, updates = engine.match(rule, aircraft, owner != "" && owner == expected, observedAt, sequence, alerts, updates)
		}
	}
	for expected, rules := range index.aircraftType {
		for _, rule := range rules {
			alerts, updates = engine.match(rule, aircraft, aircraftType != "" && aircraftType == expected, observedAt, sequence, alerts, updates)
		}
	}
	return alerts, updates
}

func (engine *Engine) matchAll(rules []compiledRule, aircraft domain.Aircraft, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	for _, rule := range rules {
		alerts, updates = engine.match(rule, aircraft, true, now, sequence, alerts, updates)
	}
	return alerts, updates
}

func (engine *Engine) match(rule compiledRule, aircraft domain.Aircraft, matches bool, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	key := stateKey{ruleID: rule.rule.ID, icao: aircraft.ICAO, fingerprint: rule.fingerprint}
	record := engine.states[key]
	record.seenSequence = sequence
	record.bestEffort = rule.rule.BestEffortEnrichment
	if !matches {
		if record.state.Active || record.state.ConsecutiveMatches != 0 {
			record.state.Active = false
			record.state.ConsecutiveMatches = 0
			record.state.LastClearAt = now
			updates = append(updates, record.state)
		}
		engine.states[key] = record
		return alerts, updates
	}
	if record.state.RuleID == 0 {
		record.state = domain.AlertState{RuleID: rule.rule.ID, AircraftICAO: aircraft.ICAO, ConditionFingerprint: rule.fingerprint}
	}
	minimum := rule.rule.MinimumObservations
	if minimum < 1 {
		minimum = 2
	}
	changed := false
	if !record.state.Active && record.state.ConsecutiveMatches < minimum {
		record.state.ConsecutiveMatches++
		changed = true
	}
	canFire := !record.state.Active && record.state.ConsecutiveMatches >= minimum && (record.state.LastFiredAt.IsZero() || now.Sub(record.state.LastFiredAt) >= rule.rule.Cooldown)
	if canFire {
		record.state.Active = true
		record.state.LastFiredAt = now
		changed = true
		alerts = append(alerts, buildAlert(rule, aircraft, now))
	}
	engine.states[key] = record
	if changed {
		updates = append(updates, record.state)
	}
	return alerts, updates
}

func (engine *Engine) active(rule compiledRule, icao string) bool {
	return engine.states[stateKey{ruleID: rule.rule.ID, icao: icao, fingerprint: rule.fingerprint}].state.Active
}

func (engine *Engine) evaluateEmergency(aircraft domain.Aircraft, now time.Time, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	emergency := strings.ToLower(strings.TrimSpace(aircraft.Emergency))
	recognized := aircraft.Squawk == "7500" || aircraft.Squawk == "7600" || aircraft.Squawk == "7700" || (emergency != "" && emergency != "none")
	fingerprint := "emergency:" + aircraft.Squawk + ":" + emergency
	key := stateKey{ruleID: -1, icao: aircraft.ICAO, fingerprint: fingerprint}
	record := engine.states[key]
	record.seenSequence = engine.sequence
	if recognized && !record.state.Active {
		record.state = domain.AlertState{RuleID: -1, AircraftICAO: aircraft.ICAO, ConditionFingerprint: fingerprint, LastFiredAt: now, ConsecutiveMatches: 1, Active: true}
		updates = append(updates, record.state)
		alerts = append(alerts, domain.Alert{
			ID: "emergency:" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), AircraftICAO: aircraft.ICAO, ConditionFingerprint: fingerprint,
			Type: domain.RuleEmergency, Priority: domain.AlertEmergency, Title: "Emergency aircraft", Description: emergencyDescription(aircraft), ObservedAt: now,
		})
	}
	engine.states[key] = record
	return alerts, updates
}

func buildAlert(rule compiledRule, aircraft domain.Aircraft, now time.Time) domain.Alert {
	return domain.Alert{
		ID: strconv.FormatInt(rule.rule.ID, 10) + ":" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), RuleID: rule.rule.ID,
		GuildID: rule.rule.GuildID, UserID: rule.rule.UserID, AircraftICAO: aircraft.ICAO, ConditionFingerprint: rule.fingerprint,
		Type: rule.rule.Type, Priority: domain.AlertNormal, Title: "Watch rule matched", Description: string(rule.rule.Type) + " matched " + aircraft.ICAO, ObservedAt: now, Cooldown: rule.rule.Cooldown,
	}
}

func emergencyDescription(aircraft domain.Aircraft) string {
	detail := "Emergency state reported"
	if aircraft.Squawk != "" {
		detail += " • squawk " + aircraft.Squawk
	}
	if aircraft.Emergency != "" && aircraft.Emergency != "none" {
		detail += " • " + aircraft.Emergency
	}
	return detail
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
