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
	ruleID int64
	scope  domain.FeederID
	icao   string
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

type scopeState struct {
	mu       sync.Mutex
	states   map[stateKey]stateRecord
	seen     map[string]seenRecord
	sequence uint64
}

const (
	minimumSeenRetention = 30 * time.Minute
	stateRetention       = 7 * 24 * time.Hour
	maxSeenEntries       = 100_000
	maxStateEntries      = 250_000
)

type Engine struct {
	index atomic.Pointer[Index]
	now   func() time.Time

	scopes     sync.Map
	scopeCount atomic.Int64

	emergencyMu       sync.Mutex
	emergencies       map[string]stateRecord
	emergencySequence uint64
}

func NewEngine(rules []domain.WatchRule, restored []domain.AlertState) *Engine {
	engine := &Engine{emergencies: make(map[string]stateRecord), now: time.Now}
	index := BuildIndex(rules)
	engine.index.Store(index)
	for _, state := range restored {
		if state.FeederScope == "" {
			state.FeederScope = domain.FeederLocal
		}
		if state.RuleID > 0 && index.fingerprints[state.RuleID] != state.ConditionFingerprint {
			continue
		}
		lastSeen := state.LastClearAt
		if state.LastFiredAt.After(lastSeen) {
			lastSeen = state.LastFiredAt
		}
		record := stateRecord{state: state, bestEffort: index.bestEffort[state.RuleID], lastSeenAt: lastSeen}
		if state.RuleID == -1 {
			engine.emergencies[state.AircraftICAO] = record
			continue
		}
		scoped := engine.scope(state.FeederScope)
		key := stateKey{ruleID: state.RuleID, scope: state.FeederScope, icao: state.AircraftICAO}
		scoped.states[key] = record
	}
	return engine
}

func (engine *Engine) ReplaceRules(rules []domain.WatchRule) {
	next := BuildIndex(rules)
	engine.index.Store(next)
	active := make(map[int64]struct{}, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			active[rule.ID] = struct{}{}
		}
	}
	for _, scoped := range engine.scopeValues() {
		scoped.mu.Lock()
		for key := range scoped.states {
			if key.ruleID > 0 {
				record := scoped.states[key]
				fingerprint, ok := next.fingerprints[key.ruleID]
				if _, enabled := active[key.ruleID]; !enabled || !ok || record.state.ConditionFingerprint != fingerprint {
					delete(scoped.states, key)
				}
			}
		}
		scoped.mu.Unlock()
	}
}

func (engine *Engine) scope(id domain.FeederID) *scopeState {
	if value, ok := engine.scopes.Load(id); ok {
		return value.(*scopeState)
	}
	created := &scopeState{states: make(map[stateKey]stateRecord), seen: make(map[string]seenRecord)}
	value, loaded := engine.scopes.LoadOrStore(id, created)
	if !loaded {
		engine.scopeCount.Add(1)
	}
	return value.(*scopeState)
}

func (engine *Engine) scopeValues() []*scopeState {
	values := make([]*scopeState, 0, engine.scopeCount.Load())
	engine.scopes.Range(func(_, value any) bool {
		values = append(values, value.(*scopeState))
		return true
	})
	return values
}

func (engine *Engine) Evaluate(snapshot *domain.Snapshot) ([]domain.Alert, []domain.AlertState) {
	if snapshot == nil {
		return nil, nil
	}
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = engine.now()
	}
	index := engine.index.Load()
	actualScope := snapshot.FeederID
	if actualScope == "" {
		actualScope = domain.FeederLocal
	}
	alerts := make([]domain.Alert, 0, 4)
	updates := make([]domain.AlertState, 0, 8)
	if actualScope == domain.FeederAll || actualScope == domain.FeederLocal {
		engine.emergencyMu.Lock()
		engine.emergencySequence++
		for _, aircraft := range snapshot.Aircraft {
			alerts, updates = engine.evaluateEmergency(actualScope, aircraft, now, engine.emergencySequence, alerts, updates)
		}
		if engine.emergencySequence%300 == 0 || len(engine.emergencies) > maxSeenEntries {
			pruneEmergencyLocked(engine.emergencies, now, maxSeenEntries)
		}
		engine.emergencyMu.Unlock()
	} else {
		for _, aircraft := range snapshot.Aircraft {
			if !domain.EmergencyActive(aircraft) {
				continue
			}
			engine.emergencyMu.Lock()
			engine.emergencySequence++
			alerts, updates = engine.evaluateEmergency(actualScope, aircraft, now, engine.emergencySequence, alerts, updates)
			engine.emergencyMu.Unlock()
		}
	}

	runtime := engine.scope(actualScope)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.sequence++
	sequence := runtime.sequence
	scoped := index.scope(actualScope)
	for _, aircraft := range snapshot.Aircraft {
		previous, existed := runtime.seen[aircraft.ICAO]
		gap := time.Duration(0)
		if existed {
			gap = now.Sub(previous.lastSeen)
			previous.lastSeen = now
		} else {
			previous = seenRecord{firstSeen: now, lastSeen: now}
		}
		runtime.seen[aircraft.ICAO] = previous
		if scoped == nil {
			continue
		}
		alerts, updates = matchAll(runtime.states, scoped.icao[aircraft.ICAO], actualScope, aircraft, now, sequence, alerts, updates)
		alerts, updates = matchAll(runtime.states, scoped.registration[aircraft.Registration], actualScope, aircraft, now, sequence, alerts, updates)
		alerts, updates = matchAll(runtime.states, scoped.callsign[aircraft.Callsign], actualScope, aircraft, now, sequence, alerts, updates)
		alerts, updates = matchAll(runtime.states, scoped.squawk[aircraft.Squawk], actualScope, aircraft, now, sequence, alerts, updates)
		for _, length := range scoped.prefixLengths {
			if length > len(aircraft.Callsign) {
				break
			}
			alerts, updates = matchAll(runtime.states, scoped.prefixes[length][aircraft.Callsign[:length]], actualScope, aircraft, now, sequence, alerts, updates)
		}
		for _, rule := range scoped.radius {
			key, record, exists := ruleState(runtime.states, rule, aircraft.ICAO)
			active := exists && record.state.Active
			threshold := rule.rule.EnterThreshold
			if active && rule.rule.ExitThreshold > 0 {
				threshold = rule.rule.ExitThreshold
			}
			alerts, updates = matchRecord(runtime.states, key, record, exists, rule, actualScope, aircraft, aircraft.HasDistance && threshold > 0 && aircraft.DistanceNM <= threshold, now, sequence, alerts, updates)
		}
		for _, rule := range scoped.altitude {
			matches := aircraft.HasAltitude && float64(aircraft.AltitudeFeet) >= rule.rule.EnterThreshold && (rule.rule.ExitThreshold <= 0 || float64(aircraft.AltitudeFeet) <= rule.rule.ExitThreshold)
			alerts, updates = match(runtime.states, rule, actualScope, aircraft, matches, now, sequence, alerts, updates)
		}
		for _, rule := range scoped.firstSeen {
			quiet := rule.rule.Cooldown
			matches := !existed || (quiet > 0 && gap >= quiet)
			alerts, updates = match(runtime.states, rule, actualScope, aircraft, matches, now, sequence, alerts, updates)
		}
	}
	for key, record := range runtime.states {
		if key.scope != actualScope {
			continue
		}
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
		runtime.states[key] = record
		updates = append(updates, record.state)
	}
	seenCap, stateCap := engine.scopeCaps()
	if sequence%300 == 0 || len(runtime.seen) > seenCap || len(runtime.states) > stateCap {
		pruneScopeLocked(runtime, now, index, seenCap, stateCap)
	}
	return alerts, updates
}

// EvaluateEnrichment handles presentation metadata independently of live
// receiver evaluation. These rules are always best-effort and can never create
// emergency alerts.
func (engine *Engine) EvaluateEnrichment(value domain.Enrichment, aircraft domain.Aircraft, observedAt time.Time) ([]domain.Alert, []domain.AlertState) {
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
	alerts := make([]domain.Alert, 0, 1)
	updates := make([]domain.AlertState, 0, 2)
	actualScope := domain.FeederLocal
	if len(aircraft.SeenBy) == 1 {
		actualScope = aircraft.SeenBy[0]
	} else if len(aircraft.SeenBy) > 1 {
		actualScope = domain.FeederAll
	}
	scoped := index.scope(actualScope)
	if scoped == nil {
		return nil, nil
	}
	runtime := engine.scope(actualScope)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.sequence++
	sequence := runtime.sequence
	metadata := value.Aircraft
	operator, owner, aircraftType := "", "", ""
	if value.Found && metadata != nil {
		operator = strings.ToUpper(strings.TrimSpace(metadata.OperatorFlag))
		owner = strings.ToUpper(strings.TrimSpace(metadata.Owner))
		aircraftType = strings.ToUpper(strings.TrimSpace(firstNonEmpty(metadata.ICAOType, metadata.AircraftType)))
	}
	for expected, rules := range scoped.operator {
		for _, rule := range rules {
			alerts, updates = match(runtime.states, rule, actualScope, aircraft, operator != "" && operator == expected, observedAt, sequence, alerts, updates)
		}
	}
	for expected, rules := range scoped.owner {
		for _, rule := range rules {
			alerts, updates = match(runtime.states, rule, actualScope, aircraft, owner != "" && owner == expected, observedAt, sequence, alerts, updates)
		}
	}
	for expected, rules := range scoped.aircraftType {
		for _, rule := range rules {
			alerts, updates = match(runtime.states, rule, actualScope, aircraft, aircraftType != "" && aircraftType == expected, observedAt, sequence, alerts, updates)
		}
	}
	return alerts, updates
}

func matchAll(states map[stateKey]stateRecord, rules []compiledRule, actualScope domain.FeederID, aircraft domain.Aircraft, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	for _, rule := range rules {
		alerts, updates = match(states, rule, actualScope, aircraft, true, now, sequence, alerts, updates)
	}
	return alerts, updates
}

func match(states map[stateKey]stateRecord, rule compiledRule, actualScope domain.FeederID, aircraft domain.Aircraft, matches bool, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	key, record, exists := ruleState(states, rule, aircraft.ICAO)
	return matchRecord(states, key, record, exists, rule, actualScope, aircraft, matches, now, sequence, alerts, updates)
}

func ruleState(states map[stateKey]stateRecord, rule compiledRule, icao string) (stateKey, stateRecord, bool) {
	key := stateKey{ruleID: rule.rule.ID, scope: normalizedRuleScope(rule.rule), icao: icao}
	record, exists := states[key]
	if exists && record.state.ConditionFingerprint != rule.fingerprint {
		record = stateRecord{}
		exists = false
	}
	return key, record, exists
}

func matchRecord(states map[stateKey]stateRecord, key stateKey, record stateRecord, exists bool, rule compiledRule, actualScope domain.FeederID, aircraft domain.Aircraft, matches bool, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
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
		states[key] = record
		return alerts, updates
	}
	if record.state.RuleID == 0 {
		record.state = domain.AlertState{RuleID: rule.rule.ID, FeederScope: key.scope, AircraftICAO: aircraft.ICAO, ConditionFingerprint: rule.fingerprint}
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
		alerts = append(alerts, buildAlert(rule, actualScope, aircraft, now))
	}
	states[key] = record
	if changed {
		updates = append(updates, record.state)
	}
	return alerts, updates
}

func (engine *Engine) evaluateEmergency(actualScope domain.FeederID, aircraft domain.Aircraft, now time.Time, sequence uint64, alerts []domain.Alert, updates []domain.AlertState) ([]domain.Alert, []domain.AlertState) {
	recognized := domain.EmergencyActive(aircraft)
	fingerprint := "emergency"
	record, exists := engine.emergencies[aircraft.ICAO]
	if !recognized && !exists {
		return alerts, updates
	}
	if !recognized && actualScope != domain.FeederAll && actualScope != domain.FeederLocal {
		return alerts, updates
	}
	record.seenSequence = sequence
	record.lastSeenAt = now
	if !recognized {
		if record.state.Active {
			record.state.Active = false
			record.state.ConsecutiveMatches = 0
			record.state.LastClearAt = now
			updates = append(updates, record.state)
		}
		engine.emergencies[aircraft.ICAO] = record
		return alerts, updates
	}
	if recognized && !record.state.Active {
		record.state = domain.AlertState{RuleID: -1, FeederScope: domain.FeederAll, AircraftICAO: aircraft.ICAO, ConditionFingerprint: fingerprint, LastFiredAt: now, ConsecutiveMatches: 1, Active: true}
		updates = append(updates, record.state)
		alerts = append(alerts, domain.Alert{
			ID: "emergency:" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), FeederID: actualScope, AircraftICAO: aircraft.ICAO, Callsign: aircraft.Callsign, ConditionFingerprint: fingerprint,
			Type: domain.RuleEmergency, Priority: domain.AlertEmergency, Title: "Emergency aircraft", Description: emergencyDescription(aircraft), ObservedAt: now,
		})
	}
	engine.emergencies[aircraft.ICAO] = record
	return alerts, updates
}

func pruneEmergencyLocked(states map[string]stateRecord, now time.Time, limit int) int {
	removed := 0
	cutoff := now.Add(-stateRetention)
	for icao, record := range states {
		if !record.state.Active && !record.lastSeenAt.IsZero() && record.lastSeenAt.Before(cutoff) {
			delete(states, icao)
			removed++
		}
	}
	if len(states) <= limit {
		return removed
	}
	entries := make([]struct {
		icao string
		at   time.Time
	}, 0, len(states))
	for icao, record := range states {
		if !record.state.Active {
			entries = append(entries, struct {
				icao string
				at   time.Time
			}{icao, record.lastSeenAt})
		}
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].at.Before(entries[right].at) })
	for _, entry := range entries[:min(len(entries), len(states)-limit)] {
		delete(states, entry.icao)
		removed++
	}
	return removed
}

func (engine *Engine) Prune(now time.Time) (seenRemoved, statesRemoved int) {
	if now.IsZero() {
		now = engine.now()
	}
	seenCap, stateCap := engine.scopeCaps()
	for _, scoped := range engine.scopeValues() {
		scoped.mu.Lock()
		seen, states := pruneScopeLocked(scoped, now, engine.index.Load(), seenCap, stateCap)
		scoped.mu.Unlock()
		seenRemoved += seen
		statesRemoved += states
	}
	engine.emergencyMu.Lock()
	statesRemoved += pruneEmergencyLocked(engine.emergencies, now, maxSeenEntries)
	engine.emergencyMu.Unlock()
	return seenRemoved, statesRemoved
}

func (engine *Engine) Sizes() (seen, states int) {
	for _, scoped := range engine.scopeValues() {
		scoped.mu.Lock()
		seen += len(scoped.seen)
		states += len(scoped.states)
		scoped.mu.Unlock()
	}
	engine.emergencyMu.Lock()
	states += len(engine.emergencies)
	engine.emergencyMu.Unlock()
	return seen, states
}

func (engine *Engine) scopeCaps() (seen, states int) {
	count := max(1, int(engine.scopeCount.Load()))
	return max(1_000, maxSeenEntries/count), max(2_500, maxStateEntries/count)
}

func pruneScopeLocked(scoped *scopeState, now time.Time, index *Index, seenCap, stateCap int) (seenRemoved, statesRemoved int) {
	seenRetention := index.maxSeenRetention()
	seenCutoff := now.Add(-seenRetention)
	for icao, record := range scoped.seen {
		if record.lastSeen.Before(seenCutoff) {
			delete(scoped.seen, icao)
			seenRemoved++
		}
	}
	stateCutoff := now.Add(-stateRetention)
	for key, record := range scoped.states {
		if !record.state.Active && !record.lastSeenAt.IsZero() && record.lastSeenAt.Before(stateCutoff) {
			delete(scoped.states, key)
			statesRemoved++
		}
	}
	if len(scoped.seen) > seenCap {
		entries := make([]struct {
			key string
			at  time.Time
		}, 0, len(scoped.seen))
		for key, record := range scoped.seen {
			entries = append(entries, struct {
				key string
				at  time.Time
			}{key, record.lastSeen})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		for _, entry := range entries[:len(entries)-seenCap] {
			delete(scoped.seen, entry.key)
			seenRemoved++
		}
	}
	if len(scoped.states) > stateCap {
		entries := make([]struct {
			key stateKey
			at  time.Time
		}, 0, len(scoped.states))
		for key, record := range scoped.states {
			if !record.state.Active {
				entries = append(entries, struct {
					key stateKey
					at  time.Time
				}{key, record.lastSeenAt})
			}
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		remove := min(len(entries), len(scoped.states)-stateCap)
		for _, entry := range entries[:remove] {
			delete(scoped.states, entry.key)
			statesRemoved++
		}
	}
	return seenRemoved, statesRemoved
}

func buildAlert(rule compiledRule, actualScope domain.FeederID, aircraft domain.Aircraft, now time.Time) domain.Alert {
	return domain.Alert{
		ID: strconv.FormatInt(rule.rule.ID, 10) + ":" + aircraft.ICAO + ":" + strconv.FormatInt(now.Unix(), 10), RuleID: rule.rule.ID,
		GuildID: rule.rule.GuildID, FeederID: actualScope, UserID: rule.rule.UserID, AircraftICAO: aircraft.ICAO, Callsign: aircraft.Callsign, ConditionFingerprint: rule.fingerprint,
		Type: rule.rule.Type, Priority: domain.AlertNormal, Title: "Watch rule matched", Description: string(rule.rule.Type) + " matched " + aircraft.ICAO, ObservedAt: now, Cooldown: rule.rule.Cooldown,
	}
}

func normalizedRuleScope(rule domain.WatchRule) domain.FeederID {
	if rule.FeederScope == "" {
		return domain.FeederLocal
	}
	return rule.FeederScope
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
