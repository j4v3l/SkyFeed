package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

type messageDeletionStub struct {
	target       MessageTarget
	resolveErr   error
	deleteErr    error
	resolveCalls []MessageLookup
	deleteCalls  []MessageDeleteExecution
}

func (stub *messageDeletionStub) ResolveMessage(_ context.Context, lookup MessageLookup) (MessageTarget, error) {
	stub.resolveCalls = append(stub.resolveCalls, lookup)
	return stub.target, stub.resolveErr
}

func (stub *messageDeletionStub) DeleteMessage(_ context.Context, execution MessageDeleteExecution) error {
	stub.deleteCalls = append(stub.deleteCalls, execution)
	return stub.deleteErr
}

func TestAdminDeletesMessageAfterBoundConfirmation(t *testing.T) {
	router, repository, executor := messageDeletionTestRouter(t)
	request := CommandRequest{
		Name: "moderation", Subcommand: "delete-message", UserID: 7, GuildID: 42, ChannelID: 9,
		Permissions: disgocord.PermissionManageMessages, RoleIDs: []uint64{88},
		Strings: map[string]string{"message": "123456789012345678", "reason": "Remove repeated spam"}, IDs: map[string]uint64{"channel": 10},
	}
	confirmation := &responseRecorder{}
	if err := router.HandleCommand(request, confirmation); err != nil {
		t.Fatal(err)
	}
	if len(confirmation.created) != 1 || len(executor.deleteCalls) != 0 {
		t.Fatalf("created=%d deletes=%d", len(confirmation.created), len(executor.deleteCalls))
	}
	row := confirmation.created[0].Components[0].(disgocord.ActionRowComponent)
	confirmID := row.Components[0].(disgocord.ButtonComponent).CustomID
	completed := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: confirmID, UserID: 7, GuildID: 42, ChannelID: 9, RoleIDs: []uint64{88}}, completed); err != nil {
		t.Fatal(err)
	}
	if len(executor.deleteCalls) != 1 || len(completed.updated) != 1 {
		t.Fatalf("deletes=%+v updates=%d", executor.deleteCalls, len(completed.updated))
	}
	cases, err := repository.ModerationCases(context.Background(), 42, executor.target.AuthorID, 10)
	if err != nil || len(cases) != 1 {
		t.Fatalf("cases=%+v err=%v", cases, err)
	}
	value := cases[0]
	if value.Action != "delete-message" || value.Status != "succeeded" || value.TargetChannelID != 10 || value.TargetMessageID != 123456789012345678 || value.Reason != "Remove repeated spam" {
		t.Fatalf("case=%+v", value)
	}
	if strings.Contains(strings.ToLower(value.Reason+value.ErrorCode), "private message preview") {
		t.Fatal("message preview leaked into durable moderation fields")
	}
}

func TestDeleteMessageContextUsesReasonModalAndAdminRole(t *testing.T) {
	router, _, executor := messageDeletionTestRouter(t)
	denied := &responseRecorder{}
	request := CommandRequest{
		Name: "delete-message-context", UserID: 7, GuildID: 42, ChannelID: 10,
		Permissions: disgocord.PermissionManageMessages, IDs: map[string]uint64{"channel": 10, "message": 123456789012345678},
	}
	if err := router.HandleCommand(request, denied); err != nil {
		t.Fatal(err)
	}
	if len(denied.modals) != 0 || len(denied.created) != 1 {
		t.Fatal("role-free context deletion was not rejected")
	}
	request.RoleIDs = []uint64{88}
	modalResponse := &responseRecorder{}
	if err := router.HandleCommand(request, modalResponse); err != nil {
		t.Fatal(err)
	}
	if len(modalResponse.modals) != 1 {
		t.Fatalf("modals=%d", len(modalResponse.modals))
	}
	confirmation := &responseRecorder{}
	if err := router.HandleModal(ModalRequest{
		CustomID: modalResponse.modals[0].CustomID, UserID: 7, GuildID: 42, ChannelID: 10,
		Values: map[string]string{"reason": "Remove unsafe link"},
	}, confirmation); err != nil {
		t.Fatal(err)
	}
	if len(confirmation.created) != 1 || len(executor.resolveCalls) != 1 {
		t.Fatalf("created=%d resolves=%d", len(confirmation.created), len(executor.resolveCalls))
	}
}

func TestConfirmedMissingMessageRecordsFailedCase(t *testing.T) {
	router, repository, executor := messageDeletionTestRouter(t)
	request := CommandRequest{
		Name: "moderation", Subcommand: "delete-message", UserID: 7, GuildID: 42, ChannelID: 9,
		Permissions: disgocord.PermissionManageMessages, RoleIDs: []uint64{88},
		Strings: map[string]string{"message": "123456789012345678", "reason": "Remove repeated spam"}, IDs: map[string]uint64{"channel": 10},
	}
	confirmation := &responseRecorder{}
	if err := router.HandleCommand(request, confirmation); err != nil {
		t.Fatal(err)
	}
	row := confirmation.created[0].Components[0].(disgocord.ActionRowComponent)
	confirmID := row.Components[0].(disgocord.ButtonComponent).CustomID
	executor.resolveErr = ErrMessageNotFound
	completed := &responseRecorder{}
	if err := router.HandleComponent(ComponentRequest{CustomID: confirmID, UserID: 7, GuildID: 42, ChannelID: 9, RoleIDs: []uint64{88}}, completed); err != nil {
		t.Fatal(err)
	}
	cases, err := repository.ModerationCases(context.Background(), 42, executor.target.AuthorID, 10)
	if err != nil || len(cases) != 1 {
		t.Fatalf("cases=%+v err=%v", cases, err)
	}
	if cases[0].Status != "failed" || cases[0].ErrorCode != "message-missing" || len(executor.deleteCalls) != 0 {
		t.Fatalf("case=%+v deletes=%d", cases[0], len(executor.deleteCalls))
	}
}

func messageDeletionTestRouter(t *testing.T) (*Router, *sqlite.Store, *messageDeletionStub) {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := repository.EnsureGuild(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertRoleBinding(context.Background(), storage.RoleBinding{GuildID: 42, Tier: "admin", RoleID: 88}); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	executor := &messageDeletionStub{target: MessageTarget{
		GuildID: 42, ChannelID: 10, MessageID: 123456789012345678, AuthorID: 55, CreatedAt: created, Preview: "private message preview",
	}}
	router := NewRouter(snapshotStub{testSnapshot(created)}, NewSessionManager(100, 10, 15*time.Minute), 42, created)
	router.now = func() time.Time { return created.Add(time.Minute) }
	router.SetRepository(repository)
	router.SetMessageDeletion(executor)
	return router, repository, executor
}
