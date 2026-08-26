package rules

import (
	"sort"
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
	lastSeenAt   time.Time
}

type seenRecord struct {
	firstSeen time.Time
	lastSeen  time.Time
}

const (
	minimumSeenRetention = 30 * time.Minute
	stateRetention       = 7 * 24 * time.Hour
	maxSeenEntries       = 100_000
	maxStateEntries      = 250_000
)

type Engine struct {
	mu       sync.Mutex
	index    atomic.Pointer[Index]
	states   map[stateKey]stateRecord
	seen     map[string]seenRecord
	sequence uint64
	now      func() time.Time
}

func NewEngine(rules []domain.WatchRule, restored []domain.AlertState) *Engine {
	engine := &Engine{states: make(map[stateKey]stateRecord, len(restored)), seen: make(map[string]seenRecord), now: time.Now}
	engine.index.Store(BuildIndex(rules))
	bestEffort := make(map[int64]bool, len(rules))
	for _, rule := range rules {
		bestEffort[rule.ID] = rule.BestEffortEnrichment || rule.Type == domain.RuleOperator || rule.Type == domain.RuleOwner || rule.Type == domain.RuleAircraftType
	}
	for _, state := range restored {
		key := stateKey{ruleID: state.RuleID, icao: state.AircraftICAO, fingerprint: state.ConditionFingerprint}
		lastSeen := state.LastClearAt
		if state.LastFiredAt.After(lastSeen) {
			lastSeen = state.LastFiredAt
		}
		engine.states[key] = stateRecord{state: state, bestEffort: bestEffort[state.RuleID], lastSeenAt: lastSeen}
	}
	return engine
}

func (engine *Engine) ReplaceRules(rules []domain.WatchRule) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	next := BuildIndex(rules)
	engine.index.Store(next)
	active := make(map[int64]struct{}, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			active[rule.ID] = struct{}{}
		}
	}
	for key := range engine.states {
		if key.ruleID > 0 {
			if _, ok := active[key.ruleID]; !ok {
				delete(engine.states, key)
			}
		}
	}
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
		previous, existed := engine.seen[aircraft.ICAO]
		gap := time.Duration(0)
		if existed {
			gap = now.Sub(previous.lastSeen)
			previous.lastSeen = now
		} else {
			previous = seenRecord{firstSeen: now, lastSeen: now}
		}
		engine.seen[aircraft.ICAO] = previous
		alerts, updates = engine.evaluateEmergency(aircraft, now, alerts, updates)
		if index == nil {
			continue
		}
		alerts, updates = engine.matchAll(index.icao[aircraft.ICAO], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.registration[aircraft.Registration], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.callsign[aircraft.Callsign], aircraft, now, sequence, alerts, updates)
		alerts, updates = engine.matchAll(index.squawk[aircraft.Squawk], aircraft, now, sequence, alerts, updates)
		for _, length := range index.prefixLengths {
			if length > len(aircraft.Callsign) {
				break
			}
			alerts, updates = engine.matchAll(index.prefixes[length][aircraft.Callsign[:length]], aircraft, now, sequence, alerts, updates)
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
			quiet := rule.rule.Cooldown
			matches := !existed || (quiet > 0 && gap >= quiet)
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
	if sequence%300 == 0 || len(engine.seen) > maxSeenEntries || len(engine.states) > maxStateEntries {
		engine.pruneLocked(now, index)
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
	record, exists := engine.states[key]
	record.seenSequence = sequence
	record.bestEffort = rule.rule.BestEffortEnrichment
	record.lastSeenAt = now
	if !matches {
		if !exists {
			return alerts, updates
		}
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
	recognized := domain.EmergencyActive(aircraft)
	fingerprint := "emergency"
	key := stateKey{ruleID: -1, icao: aircraft.ICAO, fingerprint: fingerprint}
	record, exists := engine.states[key]
	if !recognized && !exists {
		return alerts, updates
	}
	record.seenSequence = engine.sequence
	record.lastSeenAt = now
	if !recognized {
		if record.state.Active {
			record.state.Active = false
			record.state.ConsecutiveMatches = 0
			record.state.LastClearAt = now
			updates = append(updates, record.state)
		}
		engine.states[key] = record
		return alerts, updates
	}
	if recognized && !record.state.Active {
		record.state = domain.AlertState{RuleID: -1, AircraftICAO: aircraft.ICAO, ConditionFingerprint: fingerprint, LastFiredAt: now, ConsecutiveMatches: 1, Active: true}
		updates = append(updates, record.state)
		alerts = append(alerts, domain.Alert{
			ID: "emergency:" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), AircraftICAO: aircraft.ICAO, Callsign: aircraft.Callsign, ConditionFingerprint: fingerprint,
			Type: domain.RuleEmergency, Priority: domain.AlertEmergency, Title: "Emergency aircraft", Description: emergencyDescription(aircraft), ObservedAt: now,
		})
	}
	engine.states[key] = record
	return alerts, updates
}

func (engine *Engine) Prune(now time.Time) (seenRemoved, statesRemoved int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if now.IsZero() {
		now = engine.now()
	}
	return engine.pruneLocked(now, engine.index.Load())
}

func (engine *Engine) Sizes() (seen, states int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return len(engine.seen), len(engine.states)
}

func (engine *Engine) pruneLocked(now time.Time, index *Index) (seenRemoved, statesRemoved int) {
	seenRetention := minimumSeenRetention
	if index != nil {
		for _, rule := range index.firstSeen {
			if rule.rule.Cooldown > seenRetention {
				seenRetention = rule.rule.Cooldown
			}
		}
	}
	seenCutoff := now.Add(-seenRetention)
	for icao, record := range engine.seen {
		if record.lastSeen.Before(seenCutoff) {
			delete(engine.seen, icao)
			seenRemoved++
		}
	}
	stateCutoff := now.Add(-stateRetention)
	for key, record := range engine.states {
		if !record.state.Active && !record.lastSeenAt.IsZero() && record.lastSeenAt.Before(stateCutoff) {
			delete(engine.states, key)
			statesRemoved++
		}
	}
	if len(engine.seen) > maxSeenEntries {
		entries := make([]struct {
			icao string
			at   time.Time
		}, 0, len(engine.seen))
		for icao, record := range engine.seen {
			entries = append(entries, struct {
				icao string
				at   time.Time
			}{icao, record.lastSeen})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		for _, entry := range entries[:len(entries)-maxSeenEntries] {
			delete(engine.seen, entry.icao)
			seenRemoved++
		}
	}
	if len(engine.states) > maxStateEntries {
		entries := make([]struct {
			key stateKey
			at  time.Time
		}, 0, len(engine.states))
		for key, record := range engine.states {
			if !record.state.Active {
				entries = append(entries, struct {
					key stateKey
					at  time.Time
				}{key, record.lastSeenAt})
			}
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		remove := min(len(entries), len(engine.states)-maxStateEntries)
		for _, entry := range entries[:remove] {
			delete(engine.states, entry.key)
			statesRemoved++
		}
	}
	return seenRemoved, statesRemoved
}

func buildAlert(rule compiledRule, aircraft domain.Aircraft, now time.Time) domain.Alert {
	return domain.Alert{
		ID: strconv.FormatInt(rule.rule.ID, 10) + ":" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), RuleID: rule.rule.ID,
		GuildID: rule.rule.GuildID, UserID: rule.rule.UserID, AircraftICAO: aircraft.ICAO, Callsign: aircraft.Callsign, ConditionFingerprint: rule.fingerprint,
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
