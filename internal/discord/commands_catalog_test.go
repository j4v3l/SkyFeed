package discord

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

func TestAllDesiredCommandsAndSubcommandsRespond(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	snapshot := testSnapshot(now)
	snapshot.Aircraft[0].HasPosition = true
	snapshot.Aircraft[0].Latitude = 26.68
	snapshot.Aircraft[0].Longitude = -80.1
	snapshot.Aircraft[0].Squawk = "1200"

	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := repository.EnsureGuild(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"admin", "operator", "moderator"} {
		if err := repository.UpsertRoleBinding(context.Background(), storage.RoleBinding{GuildID: 42, Tier: tier, RoleID: 88}); err != nil {
			t.Fatal(err)
		}
	}

	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(2_000, 20, 15*time.Minute), 42, now)
	router.now = func() time.Time { return now }
	router.SetRepository(repository)
	router.SetPrivacyDisclosure(privacy.NewDisclosure([]string{"readsb"}, "KPBI", 50, nil, nil))
	router.SetRoutes(catalogRouteStub{})
	router.SetModeration(&moderationExecutorStub{result: ModerationResult{DMStatus: "delivered"}})
	router.SetTestSender(func(context.Context, uint64, string) error { return nil })
	router.SetDashboardReset(func(context.Context) error { return nil })

	staff := CommandRequest{
		UserID: 7, GuildID: 42, ChannelID: 9,
		ManageGuild: true, Administrator: true,
		Permissions: disgocord.PermissionManageGuild | disgocord.PermissionManageRoles | disgocord.PermissionModerateMembers | disgocord.PermissionKickMembers | disgocord.PermissionBanMembers,
		RoleIDs:     []uint64{88},
		Strings:     map[string]string{}, Ints: map[string]int{}, Floats: map[string]float64{}, Bools: map[string]bool{}, IDs: map[string]uint64{},
	}

	cases := catalogCommandCases(t)
	if len(cases) < 19 {
		t.Fatalf("catalog too small: %d", len(cases))
	}
	for _, test := range cases {
		t.Run(test.label, func(t *testing.T) {
			request := staff
			request.Name = test.name
			request.Group = test.group
			request.Subcommand = test.subcommand
			request.Strings = copyStrings(test.strings)
			request.Ints = copyInts(test.ints)
			request.Floats = copyFloats(test.floats)
			request.Bools = copyBools(test.bools)
			request.IDs = copyIDs(test.ids)
			recorder := &responseRecorder{}
			if err := router.HandleCommand(request, recorder); err != nil {
				t.Fatal(err)
			}
			if len(recorder.created) != 1 {
				t.Fatalf("created=%d updated=%d modals=%d", len(recorder.created), len(recorder.updated), len(recorder.modals))
			}
			if len(recorder.created[0].Embeds) == 0 {
				t.Fatal("missing embed")
			}
			if recorder.created[0].Embeds[0].Title == "SkyFeed • Error" && containsUnknownCommand(recorder.created[0].Embeds[0].Description) {
				t.Fatalf("unhandled command: %q", recorder.created[0].Embeds[0].Description)
			}
		})
	}

	for _, name := range []string{"aircraft", "route", "airport", "airline", "watch"} {
		t.Run("autocomplete/"+name, func(t *testing.T) {
			recorder := &responseRecorder{}
			if err := router.HandleAutocomplete(AutocompleteRequest{Name: name, GuildID: 42, UserID: 7, Query: "A"}, recorder); err != nil {
				t.Fatal(err)
			}
			if len(recorder.completions) != 1 {
				t.Fatalf("completions=%d", len(recorder.completions))
			}
		})
	}
}

func TestUnitChoicesOfferImperialAviationAndMetric(t *testing.T) {
	choices := unitChoices()
	if len(choices) != 3 {
		t.Fatalf("unit choices=%d", len(choices))
	}
	for index, want := range []string{"imperial", "aviation", "metric"} {
		if choices[index].Value != want {
			t.Fatalf("choice %d=%q want=%q", index, choices[index].Value, want)
		}
	}
}

type catalogCase struct {
	label, name, group, subcommand string
	strings                        map[string]string
	ints                           map[string]int
	floats                         map[string]float64
	bools                          map[string]bool
	ids                            map[string]uint64
}

func catalogCommandCases(t *testing.T) []catalogCase {
	t.Helper()
	var cases []catalogCase
	for _, command := range DesiredCommands() {
		if command.Type() == disgocord.ApplicationCommandTypeMessage {
			cases = append(cases, catalogCase{
				label: "message/" + command.CommandName(), name: "aircraft",
				strings: map[string]string{"query": "ABC123"},
			})
			continue
		}
		slash, ok := command.(disgocord.SlashCommandCreate)
		if !ok {
			t.Fatalf("unexpected command %T", command)
		}
		cases = append(cases, walkSlashOptions(slash.Name, "", slash.Options)...)
	}
	return cases
}

func walkSlashOptions(name, group string, options []disgocord.ApplicationCommandOption) []catalogCase {
	hasSub := false
	var cases []catalogCase
	for _, option := range options {
		switch typed := option.(type) {
		case disgocord.ApplicationCommandOptionSubCommand:
			hasSub = true
			cases = append(cases, fillCatalogCase(name, group, typed.Name, typed.Options))
		case disgocord.ApplicationCommandOptionSubCommandGroup:
			hasSub = true
			for _, nested := range typed.Options {
				cases = append(cases, fillCatalogCase(name, typed.Name, nested.Name, nested.Options))
			}
		}
	}
	if !hasSub {
		return []catalogCase{fillCatalogCase(name, group, "", options)}
	}
	return cases
}

func fillCatalogCase(name, group, subcommand string, options []disgocord.ApplicationCommandOption) catalogCase {
	test := catalogCase{
		label: name, name: name, group: group, subcommand: subcommand,
		strings: map[string]string{}, ints: map[string]int{}, floats: map[string]float64{}, bools: map[string]bool{}, ids: map[string]uint64{},
	}
	if group != "" || subcommand != "" {
		test.label = name
		if group != "" {
			test.label += " " + group
		}
		if subcommand != "" {
			test.label += " " + subcommand
		}
	}
	for _, option := range options {
		switch option.OptionName() {
		case "query", "flight":
			test.strings[option.OptionName()] = "ABC123"
		case "code":
			switch name {
			case "airline":
				test.strings["code"] = "SKY"
			case "airport":
				test.strings["code"] = "KJFK"
			default:
				test.strings["code"] = "1200"
			}
		case "metric":
			test.strings["metric"] = "messages"
		case "system":
			test.strings["system"] = "imperial"
		case "sort":
			test.strings["sort"] = "distance"
		case "kind":
			test.strings["kind"] = "icao"
		case "value":
			test.strings["value"] = "ABC123"
		case "rule":
			test.strings["rule"] = "1"
		case "category":
			test.strings["category"] = "movements"
		case "period":
			test.strings["period"] = "1h"
		case "cadence":
			test.strings["cadence"] = "daily"
		case "purpose":
			test.strings["purpose"] = "alerts"
		case "tier":
			test.strings["tier"] = "admin"
		case "reason":
			test.strings["reason"] = "Repeated disruption"
		case "duration":
			test.strings["duration"] = "5m"
		case "user-id":
			test.strings["user-id"] = "123456789012345678"
		case "squawk":
			test.strings["squawk"] = "1200"
		case "radius-nm":
			test.floats["radius-nm"] = 20
		case "limit":
			test.ints["limit"] = 5
		case "cooldown-minutes":
			test.ints["cooldown-minutes"] = 15
		case "delete-message-days":
			test.ints["delete-message-days"] = 0
		case "case-id":
			test.ints["case-id"] = 1
		case "altitude-min":
			test.ints["altitude-min"] = 0
		case "altitude-max":
			test.ints["altitude-max"] = 40000
		case "enabled", "server":
			test.bools[option.OptionName()] = false
		case "destination", "channel":
			test.ids[option.OptionName()] = 99
		case "role":
			test.ids["role"] = 88
		case "user":
			test.ids["user"] = 8
		}
	}
	return test
}

func containsUnknownCommand(description string) bool {
	return description == "Unknown SkyFeed command."
}

func copyStrings(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyInts(values map[string]int) map[string]int {
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyFloats(values map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyBools(values map[string]bool) map[string]bool {
	out := make(map[string]bool, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyIDs(values map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

type catalogRouteStub struct{}

func (catalogRouteStub) CachedRoute(callsign string) (domain.Route, bool, error) {
	return domain.Route{
		Callsign:    callsign,
		Origin:      domain.Airport{ICAO: "KJFK", Name: "John F Kennedy Intl"},
		Destination: domain.Airport{ICAO: "KPBI", Name: "Palm Beach Intl"},
		Attribution: "Route enrichment by adsb.lol",
	}, true, nil
}
func (catalogRouteStub) CachedAirport(code string) (domain.Airport, bool, error) {
	return domain.Airport{ICAO: code, Name: "Test field", Attribution: "Airport data by adsb.lol"}, true, nil
}
func (catalogRouteStub) LookupRoute(_ context.Context, request enrichment.RouteRequest) (domain.Route, error) {
	route, _, _ := catalogRouteStub{}.CachedRoute(request.Callsign)
	return route, nil
}
func (catalogRouteStub) LookupAirport(_ context.Context, code string) (domain.Airport, error) {
	airport, _, _ := catalogRouteStub{}.CachedAirport(code)
	return airport, nil
}
func (catalogRouteStub) EnqueueRoute(enrichment.RouteRequest) enrichment.AdmissionResult {
	return enrichment.AdmissionEnqueued
}
func (catalogRouteStub) EnqueueAirport(string) enrichment.AdmissionResult {
	return enrichment.AdmissionEnqueued
}
