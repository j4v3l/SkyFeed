package domain

import "strings"

func EmergencyActive(aircraft Aircraft) bool {
	emergency := strings.ToLower(strings.TrimSpace(aircraft.Emergency))
	return EmergencySquawk(aircraft.Squawk) || (emergency != "" && emergency != "none")
}

func EmergencySquawk(squawk string) bool {
	switch strings.TrimSpace(squawk) {
	case "7500", "7600", "7700":
		return true
	default:
		return false
	}
}

func SquawkMeaning(squawk string) string {
	switch strings.TrimSpace(squawk) {
	case "7500":
		return "unlawful interference (hijack)"
	case "7600":
		return "radio failure"
	case "7700":
		return "general emergency"
	default:
		return "assigned transponder code"
	}
}
