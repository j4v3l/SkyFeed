package rules

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type compiledRule struct {
	rule        domain.WatchRule
	value       string
	fingerprint string
}

type scopeIndex struct {
	icao          map[string][]compiledRule
	registration  map[string][]compiledRule
	callsign      map[string][]compiledRule
	squawk        map[string][]compiledRule
	prefixes      map[int]map[string][]compiledRule
	prefixLengths []int
	radius        []compiledRule
	altitude      []compiledRule
	firstSeen     []compiledRule
	operator      map[string][]compiledRule
	owner         map[string][]compiledRule
	aircraftType  map[string][]compiledRule
}

type Index struct {
	scopes       map[domain.FeederID]*scopeIndex
	fingerprints map[int64]string
	bestEffort   map[int64]bool
	count        int
}

func newScopeIndex() *scopeIndex {
	return &scopeIndex{
		icao: make(map[string][]compiledRule), registration: make(map[string][]compiledRule),
		callsign: make(map[string][]compiledRule), squawk: make(map[string][]compiledRule),
		prefixes: make(map[int]map[string][]compiledRule),
		operator: make(map[string][]compiledRule), owner: make(map[string][]compiledRule), aircraftType: make(map[string][]compiledRule),
	}
}

func BuildIndex(rules []domain.WatchRule) *Index {
	index := &Index{scopes: make(map[domain.FeederID]*scopeIndex), fingerprints: make(map[int64]string), bestEffort: make(map[int64]bool)}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		value := strings.ToUpper(strings.TrimSpace(rule.Value))
		if rule.MinimumObservations < 1 {
			rule.MinimumObservations = 2
		}
		compiled := compiledRule{rule: rule, value: value, fingerprint: string(rule.Type) + ":" + value + ":" + strconv.FormatFloat(rule.EnterThreshold, 'f', -1, 64) + ":" + strconv.FormatFloat(rule.ExitThreshold, 'f', -1, 64)}
		scope := normalizedRuleScope(rule)
		scoped := index.scopes[scope]
		if scoped == nil {
			scoped = newScopeIndex()
			index.scopes[scope] = scoped
		}
		switch rule.Type {
		case domain.RuleICAO:
			scoped.icao[value] = append(scoped.icao[value], compiled)
		case domain.RuleRegistration:
			scoped.registration[value] = append(scoped.registration[value], compiled)
		case domain.RuleCallsign:
			scoped.callsign[value] = append(scoped.callsign[value], compiled)
		case domain.RuleSquawk:
			scoped.squawk[value] = append(scoped.squawk[value], compiled)
		case domain.RuleCallsignPrefix:
			length := len(value)
			if scoped.prefixes[length] == nil {
				scoped.prefixes[length] = make(map[string][]compiledRule)
				scoped.prefixLengths = append(scoped.prefixLengths, length)
			}
			scoped.prefixes[length][value] = append(scoped.prefixes[length][value], compiled)
		case domain.RuleRadius:
			scoped.radius = append(scoped.radius, compiled)
		case domain.RuleAltitude:
			scoped.altitude = append(scoped.altitude, compiled)
		case domain.RuleFirstSeen:
			compiled.rule.MinimumObservations = 1
			scoped.firstSeen = append(scoped.firstSeen, compiled)
		case domain.RuleOperator:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			scoped.operator[value] = append(scoped.operator[value], compiled)
		case domain.RuleOwner:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			scoped.owner[value] = append(scoped.owner[value], compiled)
		case domain.RuleAircraftType:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			scoped.aircraftType[value] = append(scoped.aircraftType[value], compiled)
		default:
			continue
		}
		index.fingerprints[rule.ID] = compiled.fingerprint
		index.bestEffort[rule.ID] = compiled.rule.BestEffortEnrichment
		index.count++
	}
	for _, scoped := range index.scopes {
		slices.Sort(scoped.prefixLengths)
	}
	return index
}

func (index *Index) scope(id domain.FeederID) *scopeIndex {
	if index == nil {
		return nil
	}
	return index.scopes[id]
}

func (index *Index) maxSeenRetention() time.Duration {
	retention := minimumSeenRetention
	if index == nil {
		return retention
	}
	for _, scoped := range index.scopes {
		for _, rule := range scoped.firstSeen {
			if rule.rule.Cooldown > retention {
				retention = rule.rule.Cooldown
			}
		}
	}
	return retention
}

func (index *Index) Len() int {
	if index == nil {
		return 0
	}
	return index.count
}
