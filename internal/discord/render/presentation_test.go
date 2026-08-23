package render

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestBoundEmbedEnforcesLimits(t *testing.T) {
	fields := make([]discord.EmbedField, 30)
	for index := range fields {
		fields[index] = discord.EmbedField{Name: strings.Repeat("n", 300), Value: strings.Repeat("v", 1200)}
	}
	embed := BoundEmbed(discord.Embed{
		Title:       strings.Repeat("t", 300),
		Description: strings.Repeat("d", 5000),
		Fields:      fields,
		Footer:      &discord.EmbedFooter{Text: strings.Repeat("f", 3000)},
	})
	if got := runeCount(embed.Title); got > maxTitle {
		t.Fatalf("title has %d runes", got)
	}
	if len(embed.Fields) > maxFields {
		t.Fatalf("got %d fields", len(embed.Fields))
	}
	total := runeCount(embed.Title) + runeCount(embed.Description) + runeCount(embed.Footer.Text)
	for _, field := range embed.Fields {
		if runeCount(field.Name) > maxFieldName || runeCount(field.Value) > maxFieldValue {
			t.Fatalf("field is oversized: %#v", field)
		}
		total += runeCount(field.Name) + runeCount(field.Value)
	}
	if total > maxEmbedText {
		t.Fatalf("total text is %d", total)
	}
}

func TestPlainTextNeutralizesMarkdownLinksAndControls(t *testing.T) {
	got := PlainText("**reason** https://example.invalid\x00")
	if strings.Contains(got, "**") || strings.Contains(got, "https://") || strings.ContainsRune(got, '\x00') {
		t.Fatalf("unsafe text remained: %q", got)
	}
}

func TestStatusHasAccessibleHealthTextAndNoMentions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := &domain.Snapshot{
		FetchedAt:   now.Add(-2 * time.Second),
		PublishedAt: now,
		Aircraft:    []domain.Aircraft{{ICAO: "ABC123"}},
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Status: domain.HealthHealthy},
			Receiver: domain.SourceHealth{Status: domain.HealthStale},
			Stats:    domain.SourceHealth{Status: domain.HealthHealthy},
		},
	}
	embed := Status(snapshot, time.Minute, now, true)
	if !strings.Contains(embed.Description, "STALE") {
		t.Fatalf("health text not accessible: %q", embed.Description)
	}
	message := SafeMessage(embed, true)
	if message.AllowedMentions == nil || len(message.AllowedMentions.Parse) != 0 {
		t.Fatal("allowed mentions are not explicitly disabled")
	}
	if message.Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatal("ephemeral flag missing")
	}
}

func TestHelpOnlyShowsSettingsToManagers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, test := range []struct {
		name       string
		manage     bool
		wantConfig bool
	}{
		{name: "member", manage: false, wantConfig: false},
		{name: "manager", manage: true, wantConfig: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			embed := Help(now, test.manage)
			found := false
			for _, field := range embed.Fields {
				found = found || field.Name == "/settings"
			}
			if found != test.wantConfig {
				t.Fatalf("settings visibility = %t, want %t", found, test.wantConfig)
			}
		})
	}
}

func TestFeederShowsUnknownRefresh(t *testing.T) {
	embed := Feeder(&domain.Snapshot{}, time.Unix(1_700_000_000, 0))
	for _, field := range embed.Fields {
		if field.Name == "Refresh" && field.Value != "Unavailable" {
			t.Fatalf("refresh = %q, want Unavailable", field.Value)
		}
	}
}

func TestInitialSnapshotShowsUnavailableMeasurements(t *testing.T) {
	now := time.Unix(1_787_414_400, 0)
	snapshot := &domain.Snapshot{
		Aircraft: []domain.Aircraft{},
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Status: domain.HealthDegraded},
			Receiver: domain.SourceHealth{Status: domain.HealthDegraded},
			Stats:    domain.SourceHealth{Status: domain.HealthDegraded},
		},
	}
	for _, embed := range []discord.Embed{Status(snapshot, time.Minute, now, false), Feeder(snapshot, now)} {
		for _, field := range embed.Fields {
			switch field.Name {
			case "Tracked", "Recent message rate", "Recent maximum range", "Messages in window", "Tracks reported", "Max range in window":
				if field.Value != "Unavailable" {
					t.Fatalf("%s = %q, want Unavailable", field.Name, field.Value)
				}
			}
		}
	}
	status := Status(snapshot, time.Minute, now, false)
	if strings.Contains(status.Description, "2562047") || !strings.Contains(status.Description, "waiting for the first aircraft payload") {
		t.Fatalf("status description = %q", status.Description)
	}
}

func TestStatusAndFeederDescribeRecentStatistics(t *testing.T) {
	now := time.Unix(1_787_414_400, 0)
	snapshot := &domain.Snapshot{
		FetchedAt: now,
		Aircraft:  make([]domain.Aircraft, 6),
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Status: domain.HealthHealthy, LastSuccess: now},
			Receiver: domain.SourceHealth{Status: domain.HealthHealthy, LastSuccess: now},
			Stats:    domain.SourceHealth{Status: domain.HealthHealthy, LastSuccess: now},
		},
		Statistics: domain.Statistics{
			WindowStart:     now.Add(-time.Minute),
			WindowEnd:       now,
			Messages:        1800,
			MessageRate:     30,
			MaxRangeNM:      110,
			TrackedAircraft: 6,
		},
	}
	status := Status(snapshot, time.Minute, now, false)
	statusFields := make(map[string]string, len(status.Fields))
	for _, field := range status.Fields {
		statusFields[field.Name] = field.Value
	}
	if statusFields["Recent message rate"] != "30.0 msg/s" || statusFields["Recent maximum range"] != "110.0 NM" {
		t.Fatalf("status fields = %#v", statusFields)
	}

	feeder := Feeder(snapshot, now)
	feederFields := make(map[string]string, len(feeder.Fields))
	for _, field := range feeder.Fields {
		feederFields[field.Name] = field.Value
	}
	if feederFields["Messages in window"] != "1800" || feederFields["Tracks reported"] != "6" || feederFields["Max range in window"] != "110.0 NM" {
		t.Fatalf("feeder fields = %#v", feederFields)
	}
}

func BenchmarkRenderAircraft(b *testing.B) {
	aircraft := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF", HasDistance: true, DistanceNM: 12.3, BearingDegrees: 42, HasAltitude: true, AltitudeFeet: 32000, HasGroundSpeed: true, GroundSpeedKts: 441}
	now := time.Unix(1_700_000_000, 0)
	for b.Loop() {
		_ = Aircraft(aircraft, nil, now)
	}
}
