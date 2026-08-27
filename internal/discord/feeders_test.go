package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
