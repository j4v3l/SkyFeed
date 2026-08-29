package render

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/report"
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

func TestStatusSummarizesCommunityFeederHealthWithoutPrivateIDs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	snapshot := &domain.Snapshot{FeederID: domain.FeederAll, PublishedAt: now, FetchedAt: now, Feeders: []domain.FeederSummary{
		{FeederDescriptor: domain.FeederDescriptor{ID: "private-owner-one", DisplayName: "Coast", PublicArea: "Palm Beach", Enabled: true}, Health: domain.HealthHealthy},
		{FeederDescriptor: domain.FeederDescriptor{ID: "private-owner-two", DisplayName: "North", PublicArea: "Palm Beach", Enabled: true}, Health: domain.HealthStale},
		{FeederDescriptor: domain.FeederDescriptor{ID: "private-owner-three", DisplayName: "Paused", Enabled: false}, Health: domain.HealthUnknown},
	}}
	embed := StatusWithUnits(snapshot, time.Hour, now, false, domain.UnitsAviation)
	values := embedFieldValues(embed)
	for _, expected := range []string{"1 healthy", "1 attention", "1 paused", "Palm Beach ×2"} {
		if !strings.Contains(values, expected) {
			t.Fatalf("community status missing %q: %q", expected, values)
		}
	}
	if strings.Contains(values, "private-owner") {
		t.Fatalf("private feeder ID leaked: %q", values)
	}
	if provider := fieldValueContaining(embed, "Data source"); !strings.Contains(provider, "Community aggregate") || strings.Contains(provider, "Unknown") {
		t.Fatalf("aggregate provider = %q", provider)
	}
}

func TestCommunityActivityIsGroupedByApprovedArea(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	embed := WithCommunityActivity(base("Status", Radar, now), []CommunityActivityView{
		{Area: "Palm Beach", Airport: "KPBI", Activity: domain.AirportActivity{Configured: true, Movements: []domain.AirportMovement{{Phase: domain.MovementLanded}, {Phase: domain.MovementDeparture}}}},
		{Area: "Treasure Coast", Airport: "KFPR", Activity: domain.AirportActivity{Configured: true}},
	}, now)
	values := embedFieldValues(embed)
	for _, expected := range []string{"Palm Beach", "KPBI", "1 likely landing", "1 likely departure", "Treasure Coast", "quiet right now"} {
		if !strings.Contains(values, expected) {
			t.Fatalf("community activity missing %q: %q", expected, values)
		}
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
				found = found || strings.Contains(field.Name, "Administration")
			}
			if found != test.wantConfig {
				t.Fatalf("settings visibility = %t, want %t", found, test.wantConfig)
			}
		})
	}
}

func TestFlightLeadersIsResponsiveProviderAwareAndUnitAware(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	snapshot := &domain.Snapshot{
		FeederID:    domain.FeederAll,
		PublishedAt: now.Add(-time.Second),
		Health:      domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}},
		Aircraft: []domain.Aircraft{
			{ICAO: "ABC123", Provider: domain.ProviderReadsb, Callsign: "SKY1", HasGroundSpeed: true, GroundSpeedKts: 500, HasAltitude: true, AltitudeFeet: 40_000, Seen: time.Second, SeenBy: []domain.FeederID{domain.FeederLocal, "coast"}},
			{ICAO: "DEF456", Provider: domain.ProviderReadsb, Registration: "N123SF", HasGroundSpeed: true, GroundSpeedKts: 90, HasAltitude: true, AltitudeFeet: 1_500, Seen: 2 * time.Second, SeenBy: []domain.FeederID{domain.FeederLocal}},
		},
	}
	leaders := report.SelectLiveLeaders(snapshot, now)
	aviation := FlightLeaders(snapshot, leaders, domain.UnitsAviation, now)
	if aviation.Title != "SkyFeed • Live flight leaders" || len(aviation.Fields) != 4 {
		t.Fatalf("unexpected leader card: %#v", aviation)
	}
	for _, field := range aviation.Fields {
		if field.Inline == nil || *field.Inline {
			t.Fatalf("leader field %q is inline", field.Name)
		}
		if !strings.Contains(field.Value, "\n") {
			t.Fatalf("leader field %q lacks mobile line breaks: %q", field.Name, field.Value)
		}
	}
	values := embedFieldValues(aviation)
	for _, expected := range []string{"500 kt", "40000 ft", "2 feeders", "90 kt", "1500 ft"} {
		if !strings.Contains(values, expected) {
			t.Fatalf("aviation card missing %q: %q", expected, values)
		}
	}
	if aviation.Footer == nil || !strings.Contains(strings.ToLower(aviation.Footer.Text), "readsb") || !strings.Contains(strings.ToLower(aviation.Footer.Text), "community aggregate") {
		t.Fatalf("provider footer = %#v", aviation.Footer)
	}
	metric := FlightLeaders(snapshot, leaders, domain.UnitsMetric, now)
	metricValues := embedFieldValues(metric)
	if !strings.Contains(metricValues, "km/h") || !strings.Contains(metricValues, "m") {
		t.Fatalf("metric card = %q", metricValues)
	}
}

func TestFlightLeadersShowsStaleAndEmptyStatesWithoutColor(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	snapshot := &domain.Snapshot{
		PublishedAt: now.Add(-time.Minute),
		Health:      domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthStale}},
		Aircraft:    []domain.Aircraft{{ICAO: "STALE1", HasGroundSpeed: true, GroundSpeedKts: 200}},
	}
	embed := FlightLeaders(snapshot, report.SelectLiveLeaders(snapshot, now), domain.UnitsAviation, now)
	if !strings.Contains(embed.Description, "STALE") || !strings.Contains(embed.Description, "No aircraft currently") {
		t.Fatalf("empty stale state = %q", embed.Description)
	}
	for _, field := range embed.Fields {
		if !strings.Contains(field.Value, "No fresh airborne aircraft") {
			t.Fatalf("empty field = %q", field.Value)
		}
	}
}

func TestFeederShowsUnknownRefresh(t *testing.T) {
	embed := Feeder(&domain.Snapshot{}, time.Unix(1_700_000_000, 0))
	if receiver := fieldValueContaining(embed, "Receiver"); !strings.Contains(receiver, "Refresh Unavailable") {
		t.Fatalf("receiver field = %q", receiver)
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
	if live := fieldValueContaining(status, "Live traffic"); !strings.Contains(live, "Unavailable") {
		t.Fatalf("status live = %q", live)
	}
	if strings.Contains(status.Description, "2562047") || !strings.Contains(strings.ToLower(status.Description), "waiting for the first aircraft update") {
		t.Fatalf("status description = %q", status.Description)
	}
	feederEmbed := Feeder(snapshot, now)
	if receiver := fieldValueContaining(Feeder(snapshot, now), "Receiver"); !strings.Contains(receiver, "Refresh Unavailable") {
		t.Fatalf("feeder receiver = %q", receiver)
	}
	if statistics := fieldValueContaining(feederEmbed, "Statistics"); !strings.Contains(statistics, "Unavailable messages") || !strings.Contains(statistics, "Unavailable tracks") || !strings.Contains(statistics, "Max range Unavailable") {
		t.Fatalf("feeder statistics = %q", statistics)
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
	statusEmbed := Status(snapshot, time.Minute, now, false)
	status := fieldValueContaining(statusEmbed, "Live traffic")
	if !strings.Contains(status, "30.0 msg/s") || !strings.Contains(status, "110.0 NM") {
		t.Fatalf("status live = %q", status)
	}

	feeder := fieldValueContaining(Feeder(snapshot, now), "Statistics")
	if !strings.Contains(feeder, "1800 messages") || !strings.Contains(feeder, "6 tracks") || !strings.Contains(feeder, "Max range 110.0 NM") {
		t.Fatalf("feeder statistics = %q", feeder)
	}
}

func fieldMap(embed discord.Embed) map[string]string {
	fields := make(map[string]string, len(embed.Fields))
	for _, field := range embed.Fields {
		fields[field.Name] = field.Value
	}
	return fields
}

func fieldValueContaining(embed discord.Embed, name string) string {
	for _, field := range embed.Fields {
		if strings.Contains(field.Name, name) {
			return field.Value
		}
	}
	return ""
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

func TestPriorityInterestingAlertUsesAccessibleRedCard(t *testing.T) {
	embed := InterestingAlert(domain.Alert{
		InterestingPriority: true, Title: "Custody flight", Description: "Guantanamo • ICE • Deportation Flight",
		AircraftICAO: "AE1234", InterestingTags: "Guantanamo • ICE • Deportation Flight", ObservedAt: time.Unix(1_700_000_000, 0),
	})
	if embed.Color != EmergencyColor || !strings.Contains(embed.Title, "High-interest aircraft") || !strings.Contains(embed.Description, "HIGH-INTEREST MATCH") || !strings.Contains(embed.Description, "verify independently") {
		t.Fatalf("priority embed = %#v", embed)
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
	if len(embed.Fields) == 0 || !strings.Contains(embed.Fields[0].Name, "Live position") {
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
		if embed.Color != EmergencyColor || !strings.Contains(fieldValueContaining(embed, "Transponder"), "🔴") {
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
	aviation := embedFieldValues(AircraftSummary(aircraft, nil, domain.UnitsAviation, now))
	metric := embedFieldValues(AircraftSummary(aircraft, nil, domain.UnitsMetric, now))
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
	if got := fieldMap(airport)["At a glance"]; !strings.Contains(got, "elevation 305 m") {
		t.Fatalf("metric airport elevation = %q", got)
	}

	airline := AirlineWithUnits(domain.Airline{ICAO: "SKY"}, []domain.Aircraft{{
		ICAO: "ABC123", Provider: domain.ProviderReadsb, HasDistance: true, DistanceNM: 10,
	}}, now, domain.UnitsMetric)
	if got := fieldMap(airline)["1. ABC123"]; !strings.Contains(got, "18.5 km") {
		t.Fatalf("metric airline row = %q", got)
	}

	report := ReportWithUnits(storage.ReportSummary{From: now.Add(-time.Hour), To: now, MaximumRangeNM: 10}, domain.UnitsMetric)
	if got := fieldValueContaining(report, "Range & alerts"); !strings.Contains(got, "18.5 km") {
		t.Fatalf("metric report = %q", got)
	}
}

func TestAirportDashboardUsesFriendlyWeatherActivityAndAttribution(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	embed := AirportDashboard(domain.Airport{ICAO: "KXYZ", Name: "Example Airport", Attribution: "Airport directory"}, WeatherView{
		METAR: "KXYZ 231453Z 18012KT P6SM SCT040 20/10 A3000", FlightCategory: "VFR", METARStatus: "available", TAFStatus: "available",
		FetchedAt: now.Add(-2 * time.Minute), Attribution: "Weather by example", HasWind: true, WindDirectionDegrees: 180, WindSpeedKts: 12,
		HasVisibility: true, VisibilityAtLeast: true, VisibilitySM: 6, Clouds: []WeatherCloudView{{Cover: "SCT", BaseFeet: 4000, HasBase: true}},
	}, domain.AirportActivity{AirportCode: "KXYZ", Configured: true, UpdatedAt: now, Movements: []domain.AirportMovement{{
		Phase: domain.MovementApproach, ICAO: "ABC123", Callsign: "SKY123", Confidence: 85, DistanceNM: 3.2, HasDistance: true, BearingDegrees: 180,
		AltitudeFeet: 1800, HasAltitude: true, VerticalRateFPM: -700, HasVerticalRate: true, GroundSpeedKts: 130, HasGroundSpeed: true, ObservedAt: now, Evidence: "descending and converging on the airport",
	}}}, "", now, domain.UnitsMetric)
	fields := embedFieldValues(embed)
	for _, expected := range []string{"generally good visual flying conditions", "22 km/h", "at least 9.7 km", "likely approaching", "5.9 km", "548 m"} {
		if !strings.Contains(fields, expected) {
			t.Fatalf("airport dashboard missing %q: %q", expected, fields)
		}
	}
	if strings.Contains(fields, "231453Z") {
		t.Fatalf("overview unexpectedly showed raw METAR: %q", fields)
	}
	if embed.Footer == nil || !strings.Contains(embed.Footer.Text, "Airport directory") || !strings.Contains(embed.Footer.Text, "Weather by example") {
		t.Fatalf("composite attribution = %#v", embed.Footer)
	}
	for _, field := range embed.Fields {
		if field.Inline == nil || *field.Inline {
			t.Fatalf("airport field must remain readable on narrow screens: %#v", field)
		}
	}

	details := AirportDashboard(domain.Airport{ICAO: "KXYZ"}, WeatherView{METAR: "KXYZ 231453Z 18012KT", METARStatus: "available"}, domain.AirportActivity{Configured: true}, "weather-details", now, domain.UnitsAviation)
	if !strings.Contains(embedFieldValues(details), "231453Z") {
		t.Fatalf("weather details omitted raw report: %q", embedFieldValues(details))
	}
}

func TestAirportDashboardExplainsReplacementWeatherStation(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	embed := AirportDashboard(domain.Airport{ICAO: "KPBI", Name: "West Palm Beach"}, WeatherView{
		RequestedICAO: "KPBI", ReportingICAO: "KDJT", StationStatus: "alias", HasStationDistance: true,
		StationDistanceNM: 0.02, METARStatus: "available", TAFStatus: "not-found", FlightCategory: "VFR",
		ObservedAt: now.Add(-7 * time.Minute),
	}, domain.AirportActivity{}, "", now, domain.UnitsAviation)
	values := embedFieldValues(embed)
	for _, expected := range []string{"Observed at", "KDJT", "KPBI", "renamed/replacement station", "observed 7m0s ago"} {
		if !strings.Contains(values, expected) {
			t.Fatalf("weather summary missing %q: %s", expected, values)
		}
	}
}

func embedFieldValues(embed discord.Embed) string {
	var builder strings.Builder
	for _, field := range embed.Fields {
		builder.WriteString(field.Value)
		builder.WriteByte('\n')
	}
	return builder.String()
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
