package discord

import (
	"strings"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
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
	for _, test := range []struct {
		name    string
		guildID uint64
	}{{name: "direct-message", guildID: 0}, {name: "other-guild", guildID: 99}} {
		t.Run(test.name, func(t *testing.T) {
			command := &responseRecorder{}
			if err := router.HandleCommand(CommandRequest{Name: "status", GuildID: test.guildID}, command); err != nil {
				t.Fatal(err)
			}
			assertPrivateRejection(t, command)

			component := &responseRecorder{}
			if err := router.HandleComponent(ComponentRequest{CustomID: "untrusted", GuildID: test.guildID}, component); err != nil {
				t.Fatal(err)
			}
			assertPrivateRejection(t, component)

			modal := &responseRecorder{}
			if err := router.HandleModal(ModalRequest{CustomID: "untrusted", GuildID: test.guildID}, modal); err != nil {
				t.Fatal(err)
			}
			assertPrivateRejection(t, modal)

			autocomplete := &responseRecorder{}
			if err := router.HandleAutocomplete(AutocompleteRequest{Name: "aircraft", GuildID: test.guildID}, autocomplete); err != nil {
				t.Fatal(err)
			}
			if len(autocomplete.completions) != 1 || len(autocomplete.completions[0]) != 0 {
				t.Fatalf("autocomplete response = %#v", autocomplete.completions)
			}
		})
	}
}

func TestRouterComponentBindingAndModalFlow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := NewRouter(snapshotStub{testSnapshot(now)}, NewSessionManager(100, 10, 15*time.Minute), 2, now)
	router.now = func() time.Time { return now }
	recorder := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "aircraft", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"query": "ABC123"}}, recorder); err != nil {
		t.Fatal(err)
	}
	button := recorder.created[0].Components[0].(disgocord.ActionRowComponent).Components[0].(disgocord.ButtonComponent)
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
		t.Fatal("cached status should respond immediately")
	}
	report := CommandRequest{Name: "reports", Subcommand: "generate"}
	if !deferCommand(report) || deferredEphemeral(report) {
		t.Fatal("generated reports should defer publicly")
	}
	if !deferCommand(CommandRequest{Name: "route"}) || deferredEphemeral(CommandRequest{Name: "route"}) {
		t.Fatal("route should defer publicly")
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
	if err := router.HandleCommand(CommandRequest{Name: "top", UserID: 1, GuildID: 2, ChannelID: 3, Strings: map[string]string{"metric": "messages"}, Ints: map[string]int{"limit": 1}}, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.created) != 1 || len(recorder.created[0].Embeds) != 1 {
		t.Fatalf("top response = %#v", recorder.created)
	}
	if !strings.Contains(recorder.created[0].Embeds[0].Fields[0].Name, "SKY456") {
		t.Fatalf("top ranking = %#v", recorder.created[0].Embeds[0].Fields)
	}
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
