package discord

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

type moderationExecutorStub struct {
	calls  []ModerationExecution
	result ModerationResult
	err    error
}

func (stub *moderationExecutorStub) ExecuteModeration(_ context.Context, execution ModerationExecution) (ModerationResult, error) {
	stub.calls = append(stub.calls, execution)
	return stub.result, stub.err
}

func TestModerationRequiresRoleAndNativePermission(t *testing.T) {
	router, repository, executor := moderationTestRouter(t)
	base := CommandRequest{
		Name: "moderation", Subcommand: "warn", UserID: 7, GuildID: 42, ChannelID: 9,
		Strings: map[string]string{"reason": "Repeated disruption"}, IDs: map[string]uint64{"user": 8},
	}
	withoutRole := &responseRecorder{}
	base.Permissions = disgocord.PermissionModerateMembers
	if err := router.HandleCommand(base, withoutRole); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 0 || len(withoutRole.created) != 1 || withoutRole.created[0].Flags&disgocord.MessageFlagEphemeral == 0 {
		t.Fatal("role-free moderation was not privately rejected")
	}
	base.Administrator = true
	administratorWithoutBinding := &responseRecorder{}
	if err := router.HandleCommand(base, administratorWithoutBinding); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 0 {
		t.Fatal("Discord Administrator bypassed the configured moderation role")
	}
	base.Administrator = false

	withoutNative := &responseRecorder{}
	base.RoleIDs = []uint64{77}
	base.Permissions = 0
	if err := router.HandleCommand(base, withoutNative); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 0 {
		t.Fatal("moderation ran without native permission")
	}

	executor.result.DMStatus = "delivered"
	authorized := &responseRecorder{}
	base.Permissions = disgocord.PermissionModerateMembers
	if err := router.HandleCommand(base, authorized); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 1 || executor.calls[0].CaseID == 0 {
		t.Fatalf("calls=%+v", executor.calls)
	}
	cases, err := repository.ModerationCases(context.Background(), 42, 8, 10)
	if err != nil || len(cases) != 1 || cases[0].Status != "succeeded" || cases[0].DMStatus != "delivered" {
		t.Fatalf("cases=%+v err=%v", cases, err)
	}
	logs, err := repository.PendingModerationLogs(context.Background(), time.Now().Add(time.Minute), 10)
	if err != nil || len(logs) != 1 || logs[0].Case.ID != cases[0].ID {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}

func TestKickConfirmationIsBoundAndReauthorizes(t *testing.T) {
	router, _, executor := moderationTestRouter(t)
	request := CommandRequest{
		Name: "moderation", Subcommand: "kick", UserID: 7, GuildID: 42, ChannelID: 9,
		Permissions: disgocord.PermissionKickMembers, RoleIDs: []uint64{77},
		Strings: map[string]string{"reason": "Repeated disruption"}, IDs: map[string]uint64{"user": 8},
	}
	confirmation := &responseRecorder{}
	if err := router.HandleCommand(request, confirmation); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 0 || len(confirmation.created) != 1 || len(confirmation.created[0].Components) != 1 {
		t.Fatalf("calls=%d response=%+v", len(executor.calls), confirmation.created)
	}
	row := confirmation.created[0].Components[0].(disgocord.ActionRowComponent)
	confirmID := row.Components[0].(disgocord.ButtonComponent).CustomID

	changedAuthorization := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: confirmID, UserID: 7, GuildID: 42, ChannelID: 9, Permissions: disgocord.PermissionKickMembers}, changedAuthorization); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 0 || len(changedAuthorization.updated) != 1 {
		t.Fatal("confirmation did not re-check the bound role")
	}

	confirmation = &responseRecorder{}
	if err := router.HandleCommand(request, confirmation); err != nil {
		t.Fatal(err)
	}
	row = confirmation.created[0].Components[0].(disgocord.ActionRowComponent)
	confirmID = row.Components[0].(disgocord.ButtonComponent).CustomID
	completed := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: confirmID, UserID: 7, GuildID: 42, ChannelID: 9, Permissions: disgocord.PermissionKickMembers, RoleIDs: []uint64{77}}, completed); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 1 || executor.calls[0].Action != "kick" || len(completed.updated) != 1 {
		t.Fatalf("calls=%+v updates=%d", executor.calls, len(completed.updated))
	}
}

func moderationTestRouter(t *testing.T) (*Router, *sqlite.Store, *moderationExecutorStub) {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := repository.EnsureGuild(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertRoleBinding(context.Background(), storage.RoleBinding{GuildID: 42, Tier: "moderator", RoleID: 77}); err != nil {
		t.Fatal(err)
	}
	executor := &moderationExecutorStub{}
	router := NewRouter(snapshotStub{testSnapshot(time.Now())}, NewSessionManager(100, 10, 15*time.Minute), 42, time.Now())
	router.SetRepository(repository)
	router.SetModeration(executor)
	return router, repository, executor
}
