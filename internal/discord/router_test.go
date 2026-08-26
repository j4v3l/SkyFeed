package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
	"github.com/j4v3l/SkyFeed/internal/weather/aviationweather"
)

type snapshotStub struct{ snapshot *domain.Snapshot }

func (stub snapshotStub) Current() *domain.Snapshot { return stub.snapshot }

type responseRecorder struct {
	created     []disgocord.MessageCreate
	updated     []disgocord.MessageUpdate
	modals      []disgocord.ModalCreate
	completions [][]disgocord.AutocompleteChoice
}

func (recorder *responseRecorder) CreateMessage(message disgocord.MessageCreate) error {
	recorder.created = append(recorder.created, message)
	return nil
}
func (recorder *responseRecorder) UpdateMessage(message disgocord.MessageUpdate) error {
	recorder.updated = append(recorder.updated, message)
	return nil
}
func (recorder *responseRecorder) ShowModal(modal disgocord.ModalCreate) error {
	recorder.modals = append(recorder.modals, modal)
	return nil
}
func (recorder *responseRecorder) Autocomplete(choices []disgocord.AutocompleteChoice) error {
	recorder.completions = append(recorder.completions, choices)
	return nil
}

func TestRouterCachedCommandsAcknowledgeWithinTarget(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, 15*time.Minute), 2, now.Add(-time.Hour))
	router.now = func() time.Time { return now }
	for _, request := range []CommandRequest{
		{Name: "status", UserID: 1, GuildID: 2, ChannelID: 3},
		{Name: "nearby", UserID: 1, GuildID: 2, ChannelID: 3},
		{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}},
		{Name: "help", UserID: 1, GuildID: 2, ChannelID: 3},
	} {
		recorder := &responseRecorder{}
		started := time.Now()
		if err := router.HandleCommand(request, recorder); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			t.Fatalf("%s response took %s", request.Name, elapsed)
		}
		if len(recorder.created) != 1 {
			t.Fatalf("%s created %d responses", request.Name, len(recorder.created))
		}
	}
}

func TestRouterRejectsEveryInteractionOutsideConfiguredGuild(t *testing.T) {
	router := NewRouter(snapshotStub{}, NewSessionManager(100, 10, time.Minute), 42, time.Now())
	t.Run("other-guild", func(t *testing.T) {
		command := &responseRecorder{}
		if err := router.HandleCommand(CommandRequest{Name: "status", GuildID: 99}, command); err != nil {
			t.Fatal(err)
		}
		assertPrivateRejection(t, command)

		component := &responseRecorder{}
		if err := router.HandleComponent(ComponentRequest{CustomID: "untrusted", GuildID: 99}, component); err != nil {
			t.Fatal(err)
		}
		assertPrivateRejection(t, component)

		modal := &responseRecorder{}
		if err := router.HandleModal(ModalRequest{CustomID: "untrusted", GuildID: 99}, modal); err != nil {
			t.Fatal(err)
		}
		assertPrivateRejection(t, modal)

		autocomplete := &responseRecorder{}
		if err := router.HandleAutocomplete(AutocompleteRequest{Name: "aircraft", GuildID: 99}, autocomplete); err != nil {
			t.Fatal(err)
		}
		if len(autocomplete.completions) != 1 || len(autocomplete.completions[0]) != 0 {
			t.Fatalf("autocomplete response = %#v", autocomplete.completions)
		}
	})
}

func TestRouterAcceptsDirectMessagesAsConfiguredGuild(t *testing.T) {
	now := time.Now().UTC()
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, time.Minute), 42, now.Add(-time.Hour))
	router.SetGuildMemberProvider(memberProviderStub{
		info: GuildMemberInfo{Permissions: disgocord.PermissionAdministrator},
	})
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "status", UserID: 1, GuildID: 0, ChannelID: 9}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 {
		t.Fatalf("created %d responses", len(recorder.created))
	}
	if len(recorder.created[0].Embeds) == 0 || !strings.Contains(recorder.created[0].Embeds[0].Title, "Status") {
		t.Fatalf("unexpected DM status response: %+v", recorder.created)
	}
}

func TestRouterRejectsDirectMessagesWithoutAdmin(t *testing.T) {
	now := time.Now().UTC()
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, time.Minute), 42, now)
	router.SetGuildMemberProvider(memberProviderStub{
		info: GuildMemberInfo{RoleIDs: []uint64{7}},
	})
	denied := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "status", UserID: 1, GuildID: 0, ChannelID: 9}, denied); err != nil {
		t.Fatal(err)
	}
	assertPrivateRejection(t, denied)
	if len(denied.created) != 1 || !strings.Contains(denied.created[0].Embeds[0].Description, "Only SkyFeed Admins") {
		t.Fatalf("unexpected rejection: %+v", denied.created)
	}
}

type memberProviderStub struct {
	info GuildMemberInfo
	err  error
}

func (stub memberProviderStub) GuildMember(context.Context, uint64, uint64) (GuildMemberInfo, error) {
	if stub.err != nil {
		return GuildMemberInfo{}, stub.err
	}
	return stub.info, nil
}

func TestRouterComponentBindingAndModalFlow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, recorder); err != nil {
		t.Fatal(err)
	}
	var button disgocord.ButtonComponent
	for _, component := range recorder.created[0].Components[0].(disgocord.ActionRowComponent).Components {
		candidate := component.(disgocord.ButtonComponent)
		if candidate.Label == "Watch" {
			button = candidate
			break
		}
	}
	if button.CustomID == "" {
		t.Fatal("watch button missing")
	}
	wrongUser := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: button.CustomID, UserID: 9, GuildID: 2, ChannelID: 3}, wrongUser); err != nil {
		t.Fatal(err)
	}
	if len(wrongUser.created) != 1 || wrongUser.created[0].Flags&disgocord.MessageFlagEphemeral == 0 {
		t.Fatal("unauthorized component did not get one private response")
	}
	modalResponse := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: button.CustomID, UserID: 1, GuildID: 2, ChannelID: 3}, modalResponse); err != nil {
		t.Fatal(err)
	}
	if len(modalResponse.modals) != 1 {
		t.Fatalf("got %d modals", len(modalResponse.modals))
	}
	submit := &responseRecorder{}
	if err := router.HandleModal(ModalRequest{CustomID: modalResponse.modals[0].CustomID, UserID: 1, GuildID: 2, ChannelID: 3, Values: map[string]string{"label": "Home", "cooldown": "15"}}, submit); err != nil {
		t.Fatal(err)
	}
	if len(submit.created) != 1 {
		t.Fatalf("modal created %d responses", len(submit.created))
	}
}

func TestAutocompleteIsBounded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	for index := 0; index < 40; index++ {
		snapshot.Search = append(snapshot.Search, domain.AircraftKey{ICAO: "DEF" + string(rune('A'+index%26))})
	}
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, time.Minute), 2, now)
	recorder := &responseRecorder{}
	if err := router.HandleAutocomplete(AutocompleteRequest{Name: "aircraft", GuildID: 2}, recorder); err != nil {
		t.Fatal(err)
	}
	if got := len(recorder.completions[0]); got != 25 {
		t.Fatalf("got %d choices", got)
	}
}

func TestDeferredInteractionPolicy(t *testing.T) {
	if deferCommand(CommandRequest{Name: "status"}) {
		t.Fatal("cached status should respond immediately in guild channels")
	}
	if !shouldDeferCommand(CommandRequest{Name: "status", GuildID: 0}) {
		t.Fatal("direct messages should defer before admin authorization")
	}
	report := CommandRequest{Name: "reports", Subcommand: "generate"}
	if !deferCommand(report) || deferredEphemeral(report) {
		t.Fatal("generated reports should defer publicly")
	}
	if !deferCommand(CommandRequest{Name: "route"}) || deferredEphemeral(CommandRequest{Name: "route"}) {
		t.Fatal("route should defer publicly")
	}
	if !deferCommand(CommandRequest{Name: "aircraft"}) || deferredEphemeral(CommandRequest{Name: "aircraft"}) {
		t.Fatal("aircraft should defer publicly")
	}
	if !deferCommand(CommandRequest{Name: "airline"}) || deferredEphemeral(CommandRequest{Name: "airline"}) {
		t.Fatal("airline should defer publicly")
	}
	if deferCommand(CommandRequest{Name: "privacy"}) {
		t.Fatal("privacy should respond immediately")
	}
	if !deferredEphemeral(CommandRequest{Name: "privacy"}) {
		t.Fatal("privacy should be ephemeral")
	}
	if !deferCommand(CommandRequest{Name: "settings"}) || !deferredEphemeral(CommandRequest{Name: "settings"}) {
		t.Fatal("settings should defer ephemerally")
	}
}

func TestRouterPrivacyIsEphemeral(t *testing.T) {
	router := NewRouter(snapshotStub{}, NewSessionManager(100, 10, time.Minute), 2, time.Now())
	router.SetPrivacyDisclosure(privacy.NewDisclosure([]string{"readsb"}, "KPBI", 50, nil, nil))
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "privacy", UserID: 1, GuildID: 2, ChannelID: 3}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || recorder.created[0].Flags&disgocord.MessageFlagEphemeral == 0 {
		t.Fatalf("privacy response = %#v", recorder.created)
	}
}

func TestRouterSquawkRejectsInvalidCode(t *testing.T) {
	router := NewRouter(snapshotStub{testSnapshot(time.Now())}, NewSessionManager(100, 10, time.Minute), 2, time.Now())
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "squawk", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"code": "9999"}}, recorder); err != nil {
		t.Fatal(err)
	}
	assertPrivateRejection(t, recorder)
}

func TestRouterTopRanksByMetric(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	snapshot.Aircraft[0].Messages = 50
	snapshot.Aircraft[1].Messages = 200
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, time.Minute), 2, now)
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "top", Subcommand: "live", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"metric": "messages"}, Ints: map[string]int{"limit": 1}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || len(recorder.created[0].Embeds) != 1 {
		t.Fatalf("top response = %#v", recorder.created)
	}
	if !strings.Contains(recorder.created[0].Embeds[0].Fields[0].Name, "SKY456") {
		t.Fatalf("top ranking = %#v", recorder.created[0].Embeds[0].Fields)
	}
}

func TestRouterPersonalUnitsOverrideGuildDefault(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := repository.EnsureGuild(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	settings, err := repository.GuildSettings(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	settings.Units = "metric"
	if err := repository.UpsertGuildSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, time.Minute), 2, now)
	router.SetRepository(repository)
	metric := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, metric); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metric.created[0].Embeds[0].Fields[0].Value, "km") {
		t.Fatalf("guild units not used: %#v", metric.created[0].Embeds[0])
	}
	preference := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "preferences", Subcommand: "units", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"system": "aviation"}}, preference); err != nil {
		t.Fatal(err)
	}
	aviation := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 4, Strings: map[string]string{"query": "ABC123"}}, aviation); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aviation.created[0].Embeds[0].Fields[0].Value, "NM") {
		t.Fatalf("personal units not used: %#v", aviation.created[0].Embeds[0])
	}
}

func TestRouterTopRouteRankings(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := repository.EnsureGuild(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	batch := storage.RouteSightingsBatch{
		GuildID:     2,
		BucketStart: now.UTC().Truncate(time.Hour),
		Observations: []storage.RouteSightingsObservation{{
			ICAO: "ABC123", Callsign: "SKY123",
			Route: storage.RouteCatalog{Source: domain.DataSourceADSBLOL,
				Callsign: "SKY123", AirlineName: "Sky", AirlineICAO: "SKY",
				OriginIATA: "JFK", OriginName: "JFK", OriginCountryISO: "US",
				DestinationIATA: "PBI", DestinationName: "PBI", DestinationCountryISO: "US",
				Plausible: true, UpdatedAt: now,
			},
		}},
	}
	if err := repository.RecordRouteSightings(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, time.Minute), 2, now)
	router.SetRepository(repository)
	router.SetRoutes(catalogRouteStub{})
	router.SetDomesticCountryISO("US")
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "top", Subcommand: "traffic", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"metric": "routes", "period": "all"}, Ints: map[string]int{"limit": 5}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || !strings.Contains(recorder.created[0].Embeds[0].Title, "routes") {
		t.Fatalf("response=%#v", recorder.created[0].Embeds[0])
	}
}

func TestRouterEmergencyAndTrafficViews(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	snapshot.Aircraft[0].Squawk = "7700"
	snapshot.Aircraft[0].Emergency = "general"
	snapshot.Aircraft[1].HasDistance = true
	snapshot.Aircraft[1].DistanceNM = 12
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, time.Minute), 2, now)
	router.SetPrivacyDisclosure(privacy.NewDisclosure([]string{"readsb"}, "KPBI", 50, nil, nil))

	emergency := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "emergency", UserID: 1, GuildID: 2, ChannelID: 3}, emergency); err != nil {
		t.Fatal(err)
	}
	if len(emergency.created) != 1 || !strings.Contains(emergency.created[0].Embeds[0].Title, "Emergency") {
		t.Fatalf("emergency = %#v", emergency.created)
	}

	traffic := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "traffic", UserID: 1, GuildID: 2, ChannelID: 3, Floats: map[string]float64{"radius-nm": 20}}, traffic); err != nil {
		t.Fatal(err)
	}
	if len(traffic.created) != 1 || !strings.Contains(traffic.created[0].Embeds[0].Title, "Traffic") {
		t.Fatalf("traffic = %#v", traffic.created)
	}
}

func TestAircraftMessageIncludesHTTPSLinkButtons(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 {
		t.Fatalf("created=%d", len(recorder.created))
	}
	foundLink := false
	for _, row := range recorder.created[0].Components {
		action, ok := row.(disgocord.ActionRowComponent)
		if !ok {
			continue
		}
		for _, component := range action.Components {
			button, ok := component.(disgocord.ButtonComponent)
			if !ok || button.Style != disgocord.ButtonStyleLink {
				continue
			}
			foundLink = true
			if !strings.HasPrefix(button.URL, "https://") {
				t.Fatalf("non-https link %q", button.URL)
			}
		}
	}
	if !foundLink {
		t.Fatal("missing ADS-B Exchange/FlightAware link buttons")
	}
}

func TestAirportSelectOptionsSkipsEmptyCodes(t *testing.T) {
	empty := airportSelectOptions(domain.Route{
		Origin:      domain.Airport{Name: "Missing origin"},
		Destination: domain.Airport{Name: "Missing destination"},
		Midpoint:    &domain.Airport{Name: "Missing midpoint"},
	})
	if len(empty) != 0 {
		t.Fatalf("expected no options for empty codes, got %#v", empty)
	}

	partial := airportSelectOptions(domain.Route{
		Origin:      domain.Airport{ICAO: "KJFK"},
		Destination: domain.Airport{IATA: "PBI"},
	})
	if len(partial) != 2 || partial[0].Value != "KJFK" || partial[1].Value != "PBI" {
		t.Fatalf("partial=%#v", partial)
	}
}

func TestAircraftMessageOmitsAirportSelectWhenCodesEmpty(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	snapshot.Aircraft[0].HasPosition = true
	snapshot.Aircraft[0].Latitude = 26.68
	snapshot.Aircraft[0].Longitude = -80.1
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	router.SetRoutes(emptyAirportRouteStub{})
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 {
		t.Fatalf("created=%d", len(recorder.created))
	}
	for _, row := range recorder.created[0].Components {
		action, ok := row.(disgocord.ActionRowComponent)
		if !ok {
			continue
		}
		for _, component := range action.Components {
			if _, ok := component.(disgocord.StringSelectMenuComponent); ok {
				t.Fatal("airport select should be omitted when route ICAO/IATA codes are empty")
			}
		}
	}
}

func TestAirportWeatherUsesSummaryBeforeRawDetailsAction(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	router.SetRoutes(catalogRouteStub{})
	router.SetWeather(weatherStub{observation: aviationweather.Observation{
		METAR: "KPBI 231453Z 14008KT 10SM FEW040", TAF: "KPBI 231120Z 2312/2412 14010KT P6SM SCT040",
		FlightCategory: "VFR", METARStatus: "available", TAFStatus: "available", FetchedAt: now.Add(-time.Minute), Attribution: aviationweather.Attribution,
	}})
	initial := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "airport", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"code": "KPBI"}}, initial); err != nil {
		t.Fatal(err)
	}
	fields := fieldValues(initial.created[0].Embeds[0])
	if !strings.Contains(fields, "generally visual conditions") || strings.Contains(fields, "231453Z") {
		t.Fatalf("initial airport fields = %q", fields)
	}
	row := initial.created[0].Components[0].(disgocord.ActionRowComponent)
	button := row.Components[0].(disgocord.ButtonComponent)
	details := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: button.CustomID, UserID: 1, GuildID: 2, ChannelID: 3}, details); err != nil {
		t.Fatal(err)
	}
	if len(details.updated) != 1 || !strings.Contains(fieldValues((*details.updated[0].Embeds)[0]), "231453Z") {
		t.Fatalf("weather details = %#v", details.updated)
	}
}

type weatherStub struct{ observation aviationweather.Observation }

func (stub weatherStub) Lookup(context.Context, string) (aviationweather.Observation, error) {
	return stub.observation, nil
}

func fieldValues(embed disgocord.Embed) string {
	var builder strings.Builder
	for _, field := range embed.Fields {
		builder.WriteString(field.Value)
		builder.WriteByte('\n')
	}
	return builder.String()
}

type emptyAirportRouteStub struct{}

func (emptyAirportRouteStub) CachedRoute(callsign string) (domain.Route, bool, error) {
	return domain.Route{Callsign: callsign, Origin: domain.Airport{Name: "Somewhere"}, Destination: domain.Airport{Name: "Elsewhere"}}, true, nil
}
func (emptyAirportRouteStub) CachedAirport(string) (domain.Airport, bool, error) {
	return domain.Airport{}, false, nil
}
func (emptyAirportRouteStub) LookupRoute(context.Context, enrichment.RouteRequest) (domain.Route, error) {
	return domain.Route{}, nil
}
func (emptyAirportRouteStub) LookupAirport(context.Context, string) (domain.Airport, error) {
	return domain.Airport{}, nil
}
func (emptyAirportRouteStub) EnqueueRoute(enrichment.RouteRequest) enrichment.AdmissionResult {
	return enrichment.AdmissionEnqueued
}
func (emptyAirportRouteStub) EnqueueAirport(string) enrichment.AdmissionResult {
	return enrichment.AdmissionEnqueued
}

func TestAircraftFollowUpUpdatesWhenEnrichmentArrives(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	router.SetEnrichment(enrichmentFollowUpStub{value: domain.Enrichment{Found: true, ICAO: "ABC123", Aircraft: &domain.AircraftMetadata{Registration: "N123SF", PhotoURL: "https://www.planespotters.net/photo/1"}}})
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || len(recorder.updated) != 1 {
		t.Fatalf("created=%d updated=%d", len(recorder.created), len(recorder.updated))
	}
}

func TestAirlineListsVisibleCallsignPrefix(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "airline", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"code": "SKY"}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || !strings.Contains(recorder.created[0].Embeds[0].Title, "Airline") {
		t.Fatalf("airline=%#v", recorder.created)
	}
}

type enrichmentFollowUpStub struct {
	value domain.Enrichment
}

func (stub enrichmentFollowUpStub) Cached(string, string) (domain.Enrichment, bool, error) {
	return domain.Enrichment{}, false, nil
}
func (stub enrichmentFollowUpStub) Enqueue(string, string) enrichment.AdmissionResult {
	return enrichment.AdmissionEnqueued
}
func (stub enrichmentFollowUpStub) Lookup(context.Context, string, string) (domain.Enrichment, error) {
	return stub.value, nil
}

func assertPrivateRejection(t *testing.T, recorder *responseRecorder) {
	t.Helper()
	if len(recorder.created) != 1 || recorder.created[0].Flags&disgocord.MessageFlagEphemeral == 0 {
		t.Fatalf("rejection response = %#v", recorder.created)
	}
}

func testSnapshot(now time.Time) *domain.Snapshot {
	aircraft := []domain.Aircraft{
		{ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF", HasDistance: true, DistanceNM: 3.2, HasAltitude: true, AltitudeFeet: 10_000},
		{ICAO: "DEF456", Callsign: "SKY456", HasDistance: true, DistanceNM: 8.1, HasAltitude: true, AltitudeFeet: 20_000},
	}
	return &domain.Snapshot{
		FetchedAt: now, PublishedAt: now, Aircraft: aircraft,
		ByICAO: map[string]int{"ABC123": 0, "DEF456": 1},
		Search: []domain.AircraftKey{{ICAO: "ABC123", Callsign: "SKY123", Registration: "N123SF"}, {ICAO: "DEF456", Callsign: "SKY456"}},
		Health: domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}, Receiver: domain.SourceHealth{Status: domain.HealthHealthy}, Stats: domain.SourceHealth{Status: domain.HealthHealthy}},
	}
}
