package discord

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/health"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

func TestAuditCommandRequiresAdminAndIsEphemeral(t *testing.T) {
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	router := NewRouter(snapshotStub{testSnapshot(time.Now())}, NewSessionManager(100, 10, time.Minute), 42, time.Now())
	router.SetRepository(repository)
	state := health.NewState(time.Now())
	state.SetReady(true)
	state.SetComponent("discord", "healthy", "Gateway ready")
	router.SetHealth(state)

	denied := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "audit", UserID: 7, GuildID: 42, ChannelID: 9, ManageGuild: true}, denied); err != nil {
		t.Fatal(err)
	}
	if len(denied.created) != 1 || denied.created[0].Flags == 0 {
		t.Fatalf("expected ephemeral denial, got %#v", denied.created)
	}

	if err := router.HandleCommand(CommandRequest{
		Name: "settings", Group: "roles", Subcommand: "bind", UserID: 7, GuildID: 42, ChannelID: 9,
		ManageGuild: true, Administrator: true,
		Strings: map[string]string{"tier": "admin"}, IDs: map[string]uint64{"role": 88},
	}, &responseRecorder{}); err != nil {
		t.Fatal(err)
	}
	allowed := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{
		Name: "audit", UserID: 7, GuildID: 42, ChannelID: 9,
		ManageGuild: true, RoleIDs: []uint64{88},
	}, allowed); err != nil {
		t.Fatal(err)
	}
	if len(allowed.created) != 1 || allowed.created[0].Flags == 0 {
		t.Fatalf("expected ephemeral audit, got %#v", allowed.created)
	}
	if len(allowed.created[0].Embeds) != 1 || allowed.created[0].Embeds[0].Title != "SkyFeed • System audit" {
		t.Fatalf("embeds=%#v", allowed.created[0].Embeds)
	}
}

func TestAdminDigestPeriodStart(t *testing.T) {
	now := time.Date(2026, 8, 24, 19, 30, 0, 0, time.UTC)
	got := adminDigestPeriodStart(now, 6*time.Hour)
	want := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestAuditDefersEphemerally(t *testing.T) {
	request := CommandRequest{Name: "audit"}
	if !deferCommand(request) || !deferredEphemeral(request) {
		t.Fatal("audit should defer ephemerally")
	}
}
