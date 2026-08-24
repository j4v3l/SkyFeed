package discord

import (
	"net/url"
	"regexp"
	"strings"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

var (
	icaoHexPattern      = regexp.MustCompile(`\b[0-9A-Fa-f]{6}\b`)
	backtickedICAO      = regexp.MustCompile("`([0-9A-Fa-f]{6})`")
	callsignLikePattern = regexp.MustCompile(`\b[A-Z]{2,3}[0-9][A-Z0-9]{0,5}\b`)
)

func extractAircraftQuery(message disgocord.Message) string {
	for _, embed := range message.Embeds {
		for _, field := range embed.Fields {
			if strings.EqualFold(strings.TrimSpace(field.Name), "ICAO") || strings.EqualFold(strings.TrimSpace(field.Name), "Aircraft") {
				if query := firstToken(field.Value); query != "" {
					return strings.ToUpper(query)
				}
			}
		}
		if match := backtickedICAO.FindStringSubmatch(embed.Description); len(match) == 2 {
			return strings.ToUpper(match[1])
		}
		if match := backtickedICAO.FindStringSubmatch(embed.Title); len(match) == 2 {
			return strings.ToUpper(match[1])
		}
	}
	haystack := message.Content
	for _, embed := range message.Embeds {
		haystack += "\n" + embed.Title + "\n" + embed.Description
		for _, field := range embed.Fields {
			haystack += "\n" + field.Value
		}
	}
	if match := backtickedICAO.FindStringSubmatch(haystack); len(match) == 2 {
		return strings.ToUpper(match[1])
	}
	if match := icaoHexPattern.FindString(haystack); match != "" {
		return strings.ToUpper(match)
	}
	if match := callsignLikePattern.FindString(strings.ToUpper(haystack)); match != "" {
		return match
	}
	return ""
}

func firstToken(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "`", ""))
	if value == "" || strings.EqualFold(value, "Unknown") {
		return ""
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func aircraftLinkButtons(aircraft domain.Aircraft, photoURL string) []disgocord.InteractiveComponent {
	icao := strings.ToLower(strings.TrimSpace(aircraft.ICAO))
	if icao == "" {
		return nil
	}
	buttons := []disgocord.InteractiveComponent{
		disgocord.NewLinkButton("ADS-B Exchange", "https://globe.adsbexchange.com/?icao="+url.QueryEscape(icao)),
	}
	if callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign)); callsign != "" {
		buttons = append(buttons, disgocord.NewLinkButton("FlightAware", "https://www.flightaware.com/live/flight/"+url.PathEscape(callsign)))
	} else {
		buttons = append(buttons, disgocord.NewLinkButton("FlightAware", "https://www.flightaware.com/live/modes/"+url.PathEscape(icao)))
	}
	if parsed, err := url.Parse(strings.TrimSpace(photoURL)); err == nil && parsed.Scheme == "https" && parsed.Host != "" {
		buttons = append(buttons, disgocord.NewLinkButton("Photo", parsed.String()))
	}
	return buttons
}

func looksLikeICAO(query string) bool {
	if len(query) != 6 {
		return false
	}
	for _, r := range query {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}

func looksLikeNNumber(query string) bool {
	if len(query) < 2 || query[0] != 'N' {
		return false
	}
	for _, r := range query[1:] {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' {
			continue
		}
		return false
	}
	return true
}
