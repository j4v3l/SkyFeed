package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/state"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

func TestFeederInvitationIsAdminOnlyEphemeralAndDoesNotExposePrivateListData(t *testing.T) {
	ctx := context.Background()
	repository, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "feeders.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := repository.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: 42, Tier: "admin", RoleID: 88}); err != nil {
		t.Fatal(err)
	}
	manager := state.NewFeederManager(time.Millisecond)
	router := NewRouter(manager, NewSessionManager(100, 10, time.Minute), 42, time.Now())
	router.SetRepository(repository)
	router.SetFeederAdminConfig(FeederAdminConfig{Enabled: true, PublicURL: "https://mesh.example.test", MaxFeeders: 100})

	denied := &responseRecorder{}
	request := CommandRequest{Name: "feeders", Subcommand: "invite", GuildID: 42, UserID: 7, Strings: map[string]string{"name": "North Field", "area": "Palm Beach", "airport": "KPBI"}}
	if err := router.HandleCommand(request, denied); err != nil {
		t.Fatal(err)
	}
	if len(denied.created) != 1 || !strings.Contains(denied.created[0].Embeds[0].Title, "Error") {
		t.Fatalf("unauthorized invite = %#v", denied.created)
	}

	request.ManageGuild = true
	request.RoleIDs = []uint64{88}
	request.IDs = map[string]uint64{"owner": 99}
	created := &responseRecorder{}
	if err := router.HandleCommand(request, created); err != nil {
		t.Fatal(err)
	}
	if len(created.created) != 1 || created.created[0].Flags == 0 || !strings.Contains(created.created[0].Embeds[0].Description, "One-time code") {
		t.Fatalf("private invitation = %#v", created.created)
	}

	feeders, err := repository.Feeders(ctx, 42, 10)
	if err != nil || len(feeders) != 2 {
		t.Fatalf("feeders=%+v err=%v", feeders, err)
	}
	var remoteID string
	for _, feeder := range feeders {
		if feeder.OwnerUserID == 99 {
			remoteID = string(feeder.Descriptor.ID)
		}
	}
	if remoteID == "" {
		t.Fatal("remote feeder was not stored")
	}
	listed := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "feeders", Subcommand: "list", GuildID: 42, UserID: 123}, listed); err != nil {
		t.Fatal(err)
	}
	publicText := listed.created[0].Embeds[0].Description
	for _, field := range listed.created[0].Embeds[0].Fields {
		publicText += field.Name + field.Value
	}
	if strings.Contains(publicText, "One-time code") || strings.Contains(publicText, "owner") || strings.Contains(publicText, "mesh.example.test") {
		t.Fatalf("public feeder list exposed private data: %s", publicText)
	}
}

func TestFeederRenameIsAuthorizedPersistentAndImmediatelyVisible(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "feeders.db")
	repository, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: 42, Tier: "admin", RoleID: 88}); err != nil {
		t.Fatal(err)
	}
	local, err := repository.Feeder(ctx, domain.FeederLocal)
	if err != nil {
		t.Fatal(err)
	}
	manager := state.NewFeederManager(time.Millisecond)
	if err := manager.Register(local.Descriptor); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(manager, NewSessionManager(100, 10, time.Minute), 42, time.Now())
	router.SetRepository(repository)

	denied := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{
		Name: "feeders", Subcommand: "rename", GuildID: 42, UserID: 7, ManageGuild: true,
		Strings: map[string]string{"feeder": "local", "name": "Unapproved name"},
	}, denied); err != nil {
		t.Fatal(err)
	}
	assertPrivateRejection(t, denied)

	admin := CommandRequest{
		Name: "feeders", GuildID: 42, UserID: 7, ManageGuild: true, RoleIDs: []uint64{88},
		Strings: map[string]string{"feeder": "local", "name": "  Palm Beach Radar  "},
	}
	admin.Subcommand = "rename"
	renamed := &responseRecorder{}
	if err := router.HandleCommand(admin, renamed); err != nil {
		t.Fatal(err)
	}
	if len(renamed.created) != 1 || renamed.created[0].Flags&disgocord.MessageFlagEphemeral == 0 {
		t.Fatalf("rename response was not private: %#v", renamed.created)
	}
	local, err = repository.Feeder(ctx, domain.FeederLocal)
	if err != nil || local.Descriptor.DisplayName != "Palm Beach Radar" {
		t.Fatalf("stored local feeder = %+v, err=%v", local.Descriptor, err)
	}
	feeders := manager.ListFeeders()
	if len(feeders) != 1 || feeders[0].DisplayName != "Palm Beach Radar" {
		t.Fatalf("live feeder registry = %+v", feeders)
	}

	completion := &responseRecorder{}
	if err := router.HandleAutocomplete(AutocompleteRequest{Name: "feeders", Option: "feeder", GuildID: 42, Query: "palm"}, completion); err != nil {
		t.Fatal(err)
	}
	if len(completion.completions) != 1 || len(completion.completions[0]) != 1 {
		t.Fatalf("rename was not visible in autocomplete: %#v", completion.completions)
	}
	choice, ok := completion.completions[0][0].(disgocord.AutocompleteChoiceString)
	if !ok || choice.Name != "Palm Beach Radar" || choice.Value != "local" {
		t.Fatalf("autocomplete choice = %#v", completion.completions[0][0])
	}

	admin.Subcommand = "set-default"
	admin.Strings = map[string]string{"feeder": "local"}
	if err := router.HandleCommand(admin, &responseRecorder{}); err != nil {
		t.Fatal(err)
	}
	settings, err := repository.GuildSettings(ctx, 42)
	if err != nil || settings.DefaultFeederID != domain.FeederLocal {
		t.Fatalf("default feeder = %q, err=%v", settings.DefaultFeederID, err)
	}

	community := storage.Feeder{GuildID: 42, Descriptor: domain.FeederDescriptor{
		ID: "community-one", DisplayName: "Community One", PublicArea: "North county",
		SourceKind: domain.FeederSourceAgent, Enabled: true,
	}}
	if err := repository.UpsertFeeder(ctx, community); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(community.Descriptor); err != nil {
		t.Fatal(err)
	}
	admin.Subcommand = "rename"
	admin.Strings = map[string]string{"feeder": "community-one", "name": "📡 Coastal Skywatch"}
	if err := router.HandleCommand(admin, &responseRecorder{}); err != nil {
		t.Fatal(err)
	}
	community, err = repository.Feeder(ctx, "community-one")
	if err != nil || community.Descriptor.DisplayName != "📡 Coastal Skywatch" {
		t.Fatalf("stored community feeder = %+v, err=%v", community.Descriptor, err)
	}

	for _, badName := range []string{"North\nRadar", strings.Repeat("🛰️", 81)} {
		admin.Subcommand = "rename"
		admin.Strings = map[string]string{"feeder": "local", "name": badName}
		rejected := &responseRecorder{}
		if err := router.HandleCommand(admin, rejected); err != nil {
			t.Fatal(err)
		}
		if len(rejected.created) != 1 || !strings.Contains(rejected.created[0].Embeds[0].Title, "Error") {
			t.Fatalf("invalid name was accepted: %q", badName)
		}
	}

	crossGuild := admin
	crossGuild.GuildID = 99
	crossGuild.Administrator = true
	crossGuild.Strings = map[string]string{"feeder": "local", "name": "Other guild"}
	crossGuildResponse := &responseRecorder{}
	if err := router.HandleCommand(crossGuild, crossGuildResponse); err != nil {
		t.Fatal(err)
	}
	assertPrivateRejection(t, crossGuildResponse)

	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	local, err = reopened.Feeder(ctx, domain.FeederLocal)
	if err != nil || local.Descriptor.DisplayName != "Palm Beach Radar" {
		t.Fatalf("renamed feeder did not survive reopen: %+v, err=%v", local.Descriptor, err)
	}
}
