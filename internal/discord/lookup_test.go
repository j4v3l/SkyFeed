package discord

import (
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestExtractAircraftQueryFromDescriptionOnly(t *testing.T) {
	message := disgocord.Message{
		Embeds: []disgocord.Embed{{
			Title:       "SkyFeed • Aircraft • AAL1147",
			Description: "`A0FAF3` · N162UW · A321 · source readsb\n**Live** 12.3 NM • 042° · 32000 ft · 441 kt · track 090° · +0 ft/min",
		}},
	}
	if got := extractAircraftQuery(message); got != "A0FAF3" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractAircraftQueryFromAlertDescription(t *testing.T) {
	message := disgocord.Message{
		Embeds: []disgocord.Embed{{
			Description: "Emergency squawk observed\n`ABC123` · SKY123 · emergency · EMERGENCY",
		}},
	}
	if got := extractAircraftQuery(message); got != "ABC123" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractAircraftQueryFromAlertField(t *testing.T) {
	message := disgocord.Message{
		Embeds: []disgocord.Embed{{
			Fields: []disgocord.EmbedField{{Name: "Aircraft", Value: "DEF456"}},
		}},
	}
	if got := extractAircraftQuery(message); got != "DEF456" {
		t.Fatalf("got %q", got)
	}
}

func TestAircraftLinkButtonsUseHTTPSOnly(t *testing.T) {
	buttons := aircraftLinkButtons(domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123"}, "https://www.planespotters.net/photo/1")
	if len(buttons) != 3 {
		t.Fatalf("buttons=%d", len(buttons))
	}
	for _, button := range buttons {
		link := button.(disgocord.ButtonComponent)
		if !startsWithHTTPS(link.URL) {
			t.Fatalf("url=%q", link.URL)
		}
	}
	if got := aircraftLinkButtons(domain.Aircraft{ICAO: "ABC123"}, "http://evil.example/photo"); len(got) != 2 {
		t.Fatalf("rejected photo buttons=%d", len(got))
	}
}

func startsWithHTTPS(value string) bool {
	return len(value) >= 8 && value[:8] == "https://"
}
