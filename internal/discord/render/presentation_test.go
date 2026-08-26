package render

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
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
				found = found || field.Name == "Administration"
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

func TestAircraftClassifiesEmergencySquawksWithoutEmergencyField(t *testing.T) {
	for _, code := range []string{"7500", "7600", "7700"} {
		embed := Aircraft(domain.Aircraft{ICAO: "ABC123", Squawk: code}, nil, time.Unix(1_700_000_000, 0))
		if embed.Color != EmergencyColor || !strings.Contains(fieldMap(embed)["Live"], "🔴") {
			t.Fatalf("squawk %s embed = %#v", code, embed)
		}
	}
}

func TestAircraftFooterCombinesLiveAndEnrichmentProvenance(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := &domain.Snapshot{ActiveProvider: domain.ProviderAirplanesLive, FetchedAt: now.Add(-2 * time.Second)}
	route := &domain.Route{Source: domain.DataSourceADSBLOL, Attribution: "adsb.lol route data"}
	value := &domain.Enrichment{Found: true, FetchedAt: now.Add(-time.Minute), Aircraft: &domain.AircraftMetadata{Source: domain.DataSourceADSBDB}}
	embed := AircraftWithEnrichment(domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderAirplanesLive}, snapshot, value, route, now)
	footer := embed.Footer.Text
	for _, expected := range []string{"airplanes-live", "ADSBDB", "adsb-lol"} {
		if !strings.Contains(footer, expected) {
			t.Fatalf("footer %q missing %q", footer, expected)
		}
	}
	if strings.Contains(strings.ToLower(footer), "receiver") || strings.Contains(strings.ToLower(footer), "readsb") {
		t.Fatalf("provider-aware footer = %q", footer)
	}
}

func TestAircraftUnknownProviderDoesNotClaimReadsb(t *testing.T) {
	embed := Aircraft(domain.Aircraft{ICAO: "ABC123"}, nil, time.Unix(1_700_000_000, 0))
	content := strings.ToLower(embed.Description + " " + embed.Footer.Text)
	if strings.Contains(content, "readsb") || strings.Contains(content, "receiver") {
		t.Fatalf("unknown provider made a false receiver claim: %q", content)
	}
}

func TestAircraftAndRouteUseCompositeProviderFootersWithoutSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	aircraft := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderAirplanesLive, Seen: 2 * time.Second}
	aircraftEmbed := Aircraft(aircraft, nil, now)
	if !strings.Contains(aircraftEmbed.Footer.Text, "airplanes-live") {
		t.Fatalf("aircraft footer = %q", aircraftEmbed.Footer.Text)
	}

	route := Route(domain.Route{Source: domain.DataSourceADSBLOL, Attribution: "adsb.lol route data"}, aircraft, "KJFK METAR", "", now)
	for _, expected := range []string{"airplanes-live", "adsb-lol", "aviationweather.gov"} {
		if !strings.Contains(route.Footer.Text, expected) {
			t.Fatalf("route footer %q missing %q", route.Footer.Text, expected)
		}
	}
}

func TestAircraftRendersBothUnitSystemsAndTrends(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	aircraft := domain.Aircraft{ICAO: "ABC123", HasDistance: true, DistanceNM: 10, BearingDegrees: 45, HasAltitude: true, AltitudeFeet: 10_000, HasGroundSpeed: true, GroundSpeedKts: 100, HasVerticalRate: true, VerticalRateFPM: -500}
	aviation := fieldMap(AircraftSummary(aircraft, nil, domain.UnitsAviation, now))["Live"]
	metric := fieldMap(AircraftSummary(aircraft, nil, domain.UnitsMetric, now))["Live"]
	for _, expected := range []string{"NE", "10.0 NM", "10000 ft", "100 kt", "↓ -500 ft/min"} {
		if !strings.Contains(aviation, expected) {
			t.Fatalf("aviation %q missing %q", aviation, expected)
		}
	}
	for _, expected := range []string{"18.5 km", "3048 m", "185 km/h", "↓ -2.5 m/s"} {
		if !strings.Contains(metric, expected) {
			t.Fatalf("metric %q missing %q", metric, expected)
		}
	}
}

func TestListFooterUsesActualLiveProvider(t *testing.T) {
	embed := NearbyWithUnits([]domain.Aircraft{{
		ICAO: "ABC123", Provider: domain.ProviderAirplanesLive, Seen: 3 * time.Second,
	}}, 0, 10, time.Unix(1_700_000_000, 0), domain.UnitsAviation)
	footer := embed.Footer.Text
	if !strings.Contains(footer, "airplanes-live") || !strings.Contains(footer, "3s old") {
		t.Fatalf("provider footer = %q", footer)
	}
	if strings.Contains(strings.ToLower(footer), "readsb") || strings.Contains(strings.ToLower(footer), "receiver") {
		t.Fatalf("provider footer made a false receiver claim: %q", footer)
	}
}

func TestAirportAirlineAndReportRespectMetricUnits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	airport := AirportWithWeatherViewAndUnits(domain.Airport{ICAO: "KJFK", HasElevation: true, ElevationFeet: 1000}, WeatherView{}, false, now, domain.UnitsMetric)
	if got := fieldMap(airport)["Elevation"]; got != "305 m" {
		t.Fatalf("metric airport elevation = %q", got)
	}

	airline := AirlineWithUnits(domain.Airline{ICAO: "SKY"}, []domain.Aircraft{{
		ICAO: "ABC123", Provider: domain.ProviderReadsb, HasDistance: true, DistanceNM: 10,
	}}, now, domain.UnitsMetric)
	if got := fieldMap(airline)["1. ABC123"]; !strings.Contains(got, "18.5 km") {
		t.Fatalf("metric airline row = %q", got)
	}

	report := ReportWithUnits(storage.ReportSummary{From: now.Add(-time.Hour), To: now, MaximumRangeNM: 10}, domain.UnitsMetric)
	if got := fieldMap(report)["Range & alerts"]; !strings.Contains(got, "18.5 km") {
		t.Fatalf("metric report = %q", got)
	}
}

func TestPlaneAlertURLAllowlistsRejectArbitraryHTTPS(t *testing.T) {
	if _, ok := SafeHTTPSURL("https://evil.example/aircraft"); ok {
		t.Fatal("arbitrary reference host accepted")
	}
	if _, ok := SafePlaneAlertImageURL("https://github.com/example/photo.jpg"); ok {
		t.Fatal("non-image provider accepted")
	}
	if _, ok := SafePlaneAlertImageURL("https://upload.wikimedia.org/example.jpg"); !ok {
		t.Fatal("allowlisted image provider rejected")
	}
}

func BenchmarkRenderAircraft(b *testing.B) {
	aircraft := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF", HasDistance: true, DistanceNM: 12.3, BearingDegrees: 42, HasAltitude: true, AltitudeFeet: 32000, HasGroundSpeed: true, GroundSpeedKts: 441}
	now := time.Unix(1_700_000_000, 0)
	for b.Loop() {
		_ = Aircraft(aircraft, nil, now)
	}
}
