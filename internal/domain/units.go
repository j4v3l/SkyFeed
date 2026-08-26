package domain

import "strings"

type UnitSystem string

const (
	UnitsAviation UnitSystem = "aviation"
	UnitsMetric   UnitSystem = "metric"
)

func ParseUnitSystem(value string) (UnitSystem, bool) {
	switch UnitSystem(strings.ToLower(strings.TrimSpace(value))) {
	case UnitsAviation:
		return UnitsAviation, true
	case UnitsMetric:
		return UnitsMetric, true
	default:
		return "", false
	}
}

func NormalizeUnitSystem(value string) UnitSystem {
	units, ok := ParseUnitSystem(value)
	if !ok {
		return UnitsAviation
	}
	return units
}
