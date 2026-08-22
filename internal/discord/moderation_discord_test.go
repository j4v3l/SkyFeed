package discord

import (
	"context"
	"errors"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

type moderationRESTStub struct {
	guild        *disgocord.RestGuild
	members      map[snowflake.ID]*disgocord.Member
	dmErr        error
	banDuration  time.Duration
	removed      snowflake.ID
	updated      snowflake.ID
	messageCount int
}

func (stub *moderationRESTStub) GetGuild(snowflake.ID, bool, ...rest.RequestOpt) (*disgocord.RestGuild, error) {
	return stub.guild, nil
}

func (stub *moderationRESTStub) GetMember(_ snowflake.ID, userID snowflake.ID, _ ...rest.RequestOpt) (*disgocord.Member, error) {
	member, ok := stub.members[userID]
	if !ok {
		return nil, errors.New("member not found")
	}
	return member, nil
}

func (stub *moderationRESTStub) UpdateMember(_ snowflake.ID, userID snowflake.ID, _ disgocord.MemberUpdate, _ ...rest.RequestOpt) (*disgocord.Member, error) {
	stub.updated = userID
	return stub.members[userID], nil
}

func (stub *moderationRESTStub) RemoveMember(_ snowflake.ID, userID snowflake.ID, _ ...rest.RequestOpt) error {
	stub.removed = userID
	return nil
}

func (stub *moderationRESTStub) AddBan(_ snowflake.ID, userID snowflake.ID, duration time.Duration, _ ...rest.RequestOpt) error {
	stub.removed = userID
	stub.banDuration = duration
	return nil
}

func (stub *moderationRESTStub) DeleteBan(_ snowflake.ID, userID snowflake.ID, _ ...rest.RequestOpt) error {
	stub.removed = userID
	return nil
}

func (stub *moderationRESTStub) CreateDMChannel(snowflake.ID, ...rest.RequestOpt) (*disgocord.DMChannel, error) {
	if stub.dmErr != nil {
		return nil, stub.dmErr
	}
	return &disgocord.DMChannel{}, nil
}

func (stub *moderationRESTStub) CreateMessage(snowflake.ID, disgocord.MessageCreate, ...rest.RequestOpt) (*disgocord.Message, error) {
	stub.messageCount++
	return &disgocord.Message{}, nil
}

func TestDiscordModerationEnforcesHierarchyAndExecutesBan(t *testing.T) {
	api := newModerationRESTStub(5)
	executor := discordModerationExecutor{rest: api, botUserID: 6}
	result, err := executor.ExecuteModeration(context.Background(), ModerationExecution{
		CaseID: 1, GuildID: 1, ModeratorID: 2, TargetUserID: 4, Action: "ban", Reason: "Repeated disruption", DeleteMessageDuration: 24 * time.Hour,
	})
	if err != nil || result.DMStatus != "not-attempted" || api.removed != 4 || api.banDuration != 24*time.Hour {
		t.Fatalf("result=%+v removed=%d duration=%s err=%v", result, api.removed, api.banDuration, err)
	}

	api = newModerationRESTStub(25)
	executor.rest = api
	if _, err := executor.ExecuteModeration(context.Background(), ModerationExecution{CaseID: 2, GuildID: 1, ModeratorID: 2, TargetUserID: 4, Action: "kick", Reason: "Repeated disruption"}); !errors.Is(err, ErrModerationHierarchy) {
		t.Fatalf("expected hierarchy error, got %v", err)
	}
	if api.removed != 0 {
		t.Fatal("hierarchy-blocked action reached Discord")
	}
}

func TestDiscordWarningDMIsBestEffortAndRecorded(t *testing.T) {
	api := newModerationRESTStub(5)
	api.dmErr = errors.New("DMs closed")
	executor := discordModerationExecutor{rest: api, botUserID: 6}
	result, err := executor.ExecuteModeration(context.Background(), ModerationExecution{CaseID: 1, GuildID: 1, ModeratorID: 2, TargetUserID: 4, Action: "warn", Reason: "Repeated disruption"})
	if err != nil || result.DMStatus != "failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func newModerationRESTStub(targetPosition int) *moderationRESTStub {
	roles := []disgocord.Role{
		{ID: 1, Position: 0},
		{ID: 10, Position: 20, Permissions: disgocord.PermissionModerateMembers | disgocord.PermissionKickMembers | disgocord.PermissionBanMembers},
		{ID: 11, Position: 30, Permissions: disgocord.PermissionModerateMembers | disgocord.PermissionKickMembers | disgocord.PermissionBanMembers},
		{ID: 12, Position: targetPosition},
	}
	return &moderationRESTStub{
		guild: &disgocord.RestGuild{Guild: disgocord.Guild{ID: 1, OwnerID: 3}, Roles: roles},
		members: map[snowflake.ID]*disgocord.Member{
			2: {User: disgocord.User{ID: 2}, RoleIDs: []snowflake.ID{10}},
			4: {User: disgocord.User{ID: 4}, RoleIDs: []snowflake.ID{12}},
			6: {User: disgocord.User{ID: 6}, RoleIDs: []snowflake.ID{11}},
		},
	}
}
