package render

import (
	"fmt"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const nauticalMilesToStatuteMiles = 1.150779448

type unitFormatter struct {
	system domain.UnitSystem
}

func unitsFor(system domain.UnitSystem) unitFormatter {
	switch system {
	case domain.UnitsImperial, domain.UnitsAviation, domain.UnitsMetric:
		return unitFormatter{system: system}
	default:
		return unitFormatter{system: domain.DefaultUnitSystem}
	}
}

func (formatter unitFormatter) distanceNM(value float64) string {
	switch formatter.system {
	case domain.UnitsMetric:
		return fmt.Sprintf("%.1f km", value*1.852)
	case domain.UnitsImperial:
		return fmt.Sprintf("%.1f mi", value*nauticalMilesToStatuteMiles)
	default:
		return fmt.Sprintf("%.1f NM", value)
	}
}

func (formatter unitFormatter) altitudeFeet(value int) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%d m", int(float64(value)*0.3048))
	}
	return fmt.Sprintf("%d ft", value)
}

func (formatter unitFormatter) elevationFeet(value float64) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%.0f m", value*0.3048)
	}
	return fmt.Sprintf("%.0f ft", value)
}

func (formatter unitFormatter) signedAltitudeFeet(value int) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%+.0f m", float64(value)*0.3048)
	}
	return fmt.Sprintf("%+d ft", value)
}

func (formatter unitFormatter) speedKts(value float64) string {
	switch formatter.system {
	case domain.UnitsMetric:
		return fmt.Sprintf("%.0f km/h", value*1.852)
	case domain.UnitsImperial:
		return fmt.Sprintf("%.0f mph", value*nauticalMilesToStatuteMiles)
	default:
		return fmt.Sprintf("%.0f kt", value)
	}
}

func (formatter unitFormatter) verticalRateFPM(value int) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%+.1f m/s", float64(value)*0.00508)
	}
	return fmt.Sprintf("%+d ft/min", value)
}

func (formatter unitFormatter) temperatureC(value int) string {
	if formatter.system == domain.UnitsImperial {
		return fmt.Sprintf("%.0f°F", float64(value)*9/5+32)
	}
	return fmt.Sprintf("%d°C", value)
}

func (formatter unitFormatter) pressureInHg(value float64) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%.0f hPa", value*33.8639)
	}
	return fmt.Sprintf("%.2f inHg", value)
}

func (formatter unitFormatter) visibilitySM(value float64, prefix string) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%s%.1f km", prefix, value*1.609344)
	}
	return fmt.Sprintf("%s%.1f statute miles", prefix, value)
}

func (formatter unitFormatter) cloudBaseFeet(value int) string {
	if formatter.system == domain.UnitsMetric {
		return fmt.Sprintf("%.0f m", float64(value)*0.3048)
	}
	return fmt.Sprintf("%s ft", commaInt(value))
}
