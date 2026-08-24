package discord

import (
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestExtractAircraftQueryFromSkyFeedEmbed(t *testing.T) {
	message := disgocord.Message{
		Embeds: []disgocord.Embed{{
			Title:       "SkyFeed • Aircraft • SKY123",
			Description: "`ABC123` • N123SF • B738",
			Fields:      []disgocord.EmbedField{{Name: "ICAO", Value: "`ABC123`"}},
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
