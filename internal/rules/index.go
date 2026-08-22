package rules

import (
	"strconv"
	"strings"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type compiledRule struct {
	rule        domain.WatchRule
	value       string
	fingerprint string
}

type Index struct {
	icao         map[string][]compiledRule
	registration map[string][]compiledRule
	callsign     map[string][]compiledRule
	squawk       map[string][]compiledRule
	prefixes     []compiledRule
	radius       []compiledRule
	altitude     []compiledRule
	firstSeen    []compiledRule
	operator     map[string][]compiledRule
	owner        map[string][]compiledRule
	aircraftType map[string][]compiledRule
	count        int
}

func BuildIndex(rules []domain.WatchRule) *Index {
	index := &Index{
		icao: make(map[string][]compiledRule), registration: make(map[string][]compiledRule),
		callsign: make(map[string][]compiledRule), squawk: make(map[string][]compiledRule),
		operator: make(map[string][]compiledRule), owner: make(map[string][]compiledRule), aircraftType: make(map[string][]compiledRule),
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		value := strings.ToUpper(strings.TrimSpace(rule.Value))
		if rule.MinimumObservations < 1 {
			rule.MinimumObservations = 2
		}
		compiled := compiledRule{rule: rule, value: value, fingerprint: string(rule.Type) + ":" + value + ":" + strconv.FormatFloat(rule.EnterThreshold, 'f', -1, 64) + ":" + strconv.FormatFloat(rule.ExitThreshold, 'f', -1, 64)}
		switch rule.Type {
		case domain.RuleICAO:
			index.icao[value] = append(index.icao[value], compiled)
		case domain.RuleRegistration:
			index.registration[value] = append(index.registration[value], compiled)
		case domain.RuleCallsign:
			index.callsign[value] = append(index.callsign[value], compiled)
		case domain.RuleSquawk:
			index.squawk[value] = append(index.squawk[value], compiled)
		case domain.RuleCallsignPrefix:
			index.prefixes = append(index.prefixes, compiled)
		case domain.RuleRadius:
			index.radius = append(index.radius, compiled)
		case domain.RuleAltitude:
			index.altitude = append(index.altitude, compiled)
		case domain.RuleFirstSeen:
			index.firstSeen = append(index.firstSeen, compiled)
		case domain.RuleOperator:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			index.operator[value] = append(index.operator[value], compiled)
		case domain.RuleOwner:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			index.owner[value] = append(index.owner[value], compiled)
		case domain.RuleAircraftType:
			compiled.rule.BestEffortEnrichment = true
			compiled.rule.MinimumObservations = 1
			index.aircraftType[value] = append(index.aircraftType[value], compiled)
		default:
			continue
		}
		index.count++
	}
	return index
}

func (index *Index) Len() int {
	if index == nil {
		return 0
	}
	return index.count
}
