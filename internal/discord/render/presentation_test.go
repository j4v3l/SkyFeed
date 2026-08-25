package render

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
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
	fields := fieldMap(embed)
	if !strings.Contains(fields["Receiver"], "refresh Unavailable") {
		t.Fatalf("receiver field = %q", fields["Receiver"])
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
	status := Status(snapshot, time.Minute, now, false)
	if !strings.Contains(fieldMap(status)["Live"], "Unavailable") {
		t.Fatalf("status live = %q", fieldMap(status)["Live"])
	}
	if strings.Contains(status.Description, "2562047") || !strings.Contains(status.Description, "waiting for the first aircraft payload") {
		t.Fatalf("status description = %q", status.Description)
	}
	feeder := fieldMap(Feeder(snapshot, now))
	if !strings.Contains(feeder["Receiver"], "refresh Unavailable") {
		t.Fatalf("feeder receiver = %q", feeder["Receiver"])
	}
	if !strings.Contains(feeder["Window"], "Unavailable msgs") || !strings.Contains(feeder["Window"], "Unavailable tracks") || !strings.Contains(feeder["Window"], "max Unavailable") {
		t.Fatalf("feeder window = %q", feeder["Window"])
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
	status := fieldMap(Status(snapshot, time.Minute, now, false))
	if !strings.Contains(status["Live"], "30.0 msg/s") || !strings.Contains(status["Live"], "110.0 NM") {
		t.Fatalf("status live = %q", status["Live"])
	}

	feeder := fieldMap(Feeder(snapshot, now))
	if !strings.Contains(feeder["Window"], "1800 msgs") || !strings.Contains(feeder["Window"], "6 tracks") || !strings.Contains(feeder["Window"], "max 110.0 NM") {
		t.Fatalf("feeder window = %q", feeder["Window"])
	}
}

func fieldMap(embed discord.Embed) map[string]string {
	fields := make(map[string]string, len(embed.Fields))
	for _, field := range embed.Fields {
		fields[field.Name] = field.Value
	}
	return fields
}

func TestPrivacyRendererOmitsCoordinates(t *testing.T) {
	embed := Privacy(privacy.NewDisclosure(
		[]string{"readsb", "airplanes.live"},
		"KPBI",
		50,
		[]privacy.Retention{{Category: "snapshots", Period: "memory only"}},
		[]privacy.Attribution{{Provider: "adsb.lol", Notice: "https://evil.example/route **bold**"}},
	))
	body := embed.Description + embed.Fields[0].Value + embed.Fields[1].Value + embed.Fields[len(embed.Fields)-1].Value
	if strings.Contains(body, "https://") || strings.Contains(body, "26.") || strings.Contains(body, "-80.") {
		t.Fatalf("privacy embed leaked sensitive text: %q", body)
	}
	message := SafeMessage(embed, true)
	if message.Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatal("privacy response must be ephemeral")
	}
}

func TestRouteRendererSanitizesProviderText(t *testing.T) {
	route := domain.Route{
		Callsign:    "SKY123",
		Origin:      domain.Airport{ICAO: "KBOS", Name: "https://evil.example"},
		Destination: domain.Airport{ICAO: "KJFK", Name: "Normal"},
		Attribution: "adsb.lol https://evil.example",
	}
	embed := Route(route, domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123"}, "", "", time.Unix(1_700_000_000, 0))
	combined := embed.Description
	for _, field := range embed.Fields {
		combined += field.Value
	}
	if strings.Contains(combined, "https://") {
		t.Fatalf("route embed leaked URL: %q", combined)
	}
}

func TestInterestingAlertMessageUsesLinkButtonForHTTPSReference(t *testing.T) {
	alert := domain.Alert{
		Description:      "Interesting aircraft sighting",
		AircraftICAO:     "AE1234",
		Callsign:         "RCH123",
		InterestingGroup: "Mil",
		InterestingLink:  "https://w.wiki/CzEu",
		ObservedAt:       time.Unix(1_700_000_000, 0),
	}
	embed := InterestingAlert(alert)
	for _, field := range embed.Fields {
		if field.Name == "Reference" {
			t.Fatalf("reference field should be omitted for https links: %#v", embed.Fields)
		}
	}
	message := InterestingAlertMessage(alert, false)
	if len(message.Components) != 1 {
		t.Fatalf("components=%d", len(message.Components))
	}
	row, ok := message.Components[0].(discord.ActionRowComponent)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("action row=%#v", message.Components)
	}
	button, ok := row.Components[0].(discord.ButtonComponent)
	if !ok || button.Style != discord.ButtonStyleLink || button.URL != "https://w.wiki/CzEu" {
		t.Fatalf("button=%#v", row.Components[0])
	}
	if button.Label != "w.wiki" {
		t.Fatalf("label=%q", button.Label)
	}
}

func TestInterestingAlertKeepsPlainReferenceForInvalidURL(t *testing.T) {
	alert := domain.Alert{
		Description:     "Interesting aircraft sighting",
		InterestingLink: "see local notes",
		ObservedAt:      time.Unix(1_700_000_000, 0),
	}
	embed := InterestingAlert(alert)
	found := false
	for _, field := range embed.Fields {
		if field.Name == "Reference" {
			found = true
			if strings.Contains(field.Value, "https[:]//") {
				t.Fatalf("unexpected escaped url in %q", field.Value)
			}
		}
	}
	if !found {
		t.Fatal("missing reference field for non-url text")
	}
	if len(InterestingAlertMessage(alert, false).Components) != 0 {
		t.Fatal("invalid reference should not add link button")
	}
}

func TestAircraftUsesSectionFieldsNotInlineColumns(t *testing.T) {
	embed := Aircraft(domain.Aircraft{
		ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF",
		HasDistance: true, DistanceNM: 12.3, BearingDegrees: 42,
		HasAltitude: true, AltitudeFeet: 32000, HasGroundSpeed: true, GroundSpeedKts: 441,
		HasTrack: true, TrackDegrees: 90, Squawk: "1200",
	}, nil, time.Unix(1_700_000_000, 0))
	if !strings.Contains(embed.Description, "`ABC123`") {
		t.Fatalf("description = %q", embed.Description)
	}
	if len(embed.Fields) == 0 || embed.Fields[0].Name != "Live" {
		t.Fatalf("fields = %#v", embed.Fields)
	}
	for _, field := range embed.Fields {
		if field.Inline != nil && *field.Inline {
			t.Fatalf("field %q should be full-width for mobile readability", field.Name)
		}
	}
}

func BenchmarkRenderAircraft(b *testing.B) {
	aircraft := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF", HasDistance: true, DistanceNM: 12.3, BearingDegrees: 42, HasAltitude: true, AltitudeFeet: 32000, HasGroundSpeed: true, GroundSpeedKts: 441}
	now := time.Unix(1_700_000_000, 0)
	for b.Loop() {
		_ = Aircraft(aircraft, nil, now)
	}
}
