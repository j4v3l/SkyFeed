package config

import "strings"

// InferDomesticCountryISO mirrors Skystats' DOMESTIC_COUNTRY_ISO defaulting for common ICAO prefixes.
func InferDomesticCountryISO(publicAirportCode string) string {
	code := strings.ToUpper(strings.TrimSpace(publicAirportCode))
	switch {
	case strings.HasPrefix(code, "K"):
		return "US"
	case strings.HasPrefix(code, "C"):
		return "CA"
	case strings.HasPrefix(code, "EG"):
		return "GB"
	case strings.HasPrefix(code, "LF"):
		return "FR"
	case strings.HasPrefix(code, "ED"), strings.HasPrefix(code, "ET"):
		return "DE"
	default:
		return ""
	}
}
