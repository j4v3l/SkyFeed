package domain

import "strings"

type UnitSystem string

const (
	UnitsImperial UnitSystem = "imperial"
	UnitsAviation UnitSystem = "aviation"
	UnitsMetric   UnitSystem = "metric"
)

const DefaultUnitSystem = UnitsImperial

func ParseUnitSystem(value string) (UnitSystem, bool) {
	switch UnitSystem(strings.ToLower(strings.TrimSpace(value))) {
	case UnitsImperial:
		return UnitsImperial, true
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
		return DefaultUnitSystem
	}
	return units
}
