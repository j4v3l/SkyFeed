package discord

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

func TestAdministrativeCommandsPersistAndAuthorize(t *testing.T) {
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	router := NewRouter(snapshotStub{testSnapshot(time.Now())}, NewSessionManager(100, 10, time.Minute), 42, time.Now())
	router.SetRepository(repository)

	unauthorized := &responseRecorder{}
	request := CommandRequest{Name: "watch", Subcommand: "add", UserID: 7, GuildID: 42, ChannelID: 9, Strings: map[string]string{"kind": "icao", "value": "abc123"}, Bools: map[string]bool{"server": true}}
	if err := router.HandleCommand(request, unauthorized); err != nil {
		t.Fatal(err)
	}
	if len(unauthorized.created) != 1 || unauthorized.created[0].Flags == 0 {
		t.Fatal("unauthorized server rule was not rejected privately")
	}

	roleResponse := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Group: "roles", Subcommand: "bind", UserID: 7, GuildID: 42, ChannelID: 9, ManageGuild: true, Administrator: true, Strings: map[string]string{"tier": "admin"}, IDs: map[string]uint64{"role": 88}}, roleResponse); err != nil {
		t.Fatal(err)
	}
	request.ManageGuild = true
	request.Administrator = true
	request.RoleIDs = []uint64{88}
	created := &responseRecorder{}
	if err := router.HandleCommand(request, created); err != nil {
		t.Fatal(err)
	}
	rules, err := repository.AllWatchRules(context.Background(), 42, 10)
	if err != nil || len(rules) != 1 || rules[0].Value != "ABC123" || !rules[0].ServerScope {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}

	settings := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Subcommand: "channels", UserID: 7, GuildID: 42, ChannelID: 9, ManageGuild: true, RoleIDs: []uint64{88}, Strings: map[string]string{"purpose": "alerts"}, IDs: map[string]uint64{"channel": 99}}, settings); err != nil {
		t.Fatal(err)
	}
	bindings, err := repository.ChannelBindings(context.Background(), 42)
	if err != nil || len(bindings) != 1 || bindings[0].ChannelID != 99 {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}

	tested := ""
	router.SetTestSender(func(_ context.Context, guildID uint64, purpose string) error {
		if guildID != 42 {
			t.Fatalf("guildID=%d", guildID)
		}
		tested = purpose
		return nil
	})
	response := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Subcommand: "test", UserID: 7, GuildID: 42, ManageGuild: true, RoleIDs: []uint64{88}, Strings: map[string]string{"purpose": "alerts"}}, response); err != nil {
		t.Fatal(err)
	}
	if tested != "alerts" || len(response.created) != 1 {
		t.Fatalf("tested=%q responses=%d", tested, len(response.created))
	}

	paused := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Subcommand: "pause-alerts", UserID: 7, GuildID: 42, ManageGuild: true, RoleIDs: []uint64{88}}, paused); err != nil {
		t.Fatal(err)
	}
	settingsValue, err := repository.GuildSettings(context.Background(), 42)
	if err != nil || !settingsValue.AlertsPaused {
		t.Fatalf("paused settings=%+v err=%v", settingsValue, err)
	}
	muted := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Subcommand: "mute-squawk", UserID: 7, GuildID: 42, ManageGuild: true, RoleIDs: []uint64{88}, Strings: map[string]string{"code": "1200"}}, muted); err != nil {
		t.Fatal(err)
	}
	settingsValue, err = repository.GuildSettings(context.Background(), 42)
	if err != nil || settingsValue.MutedSquawks != "1200" {
		t.Fatalf("muted settings=%+v err=%v", settingsValue, err)
	}

	resetCalled := false
	router.SetDashboardReset(func(context.Context) error {
		resetCalled = true
		return nil
	})
	reset := &responseRecorder{}
	if err := router.HandleCommand(CommandRequest{Name: "settings", Subcommand: "recreate-dashboard", UserID: 7, GuildID: 42, ManageGuild: true, RoleIDs: []uint64{88}}, reset); err != nil {
		t.Fatal(err)
	}
	if !resetCalled || len(reset.created) != 1 {
		t.Fatalf("resetCalled=%v responses=%d", resetCalled, len(reset.created))
	}
}
