package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

func TestParseMessageTargetAcceptsSafeInputsOnly(t *testing.T) {
	const guild = uint64(123456789012345670)
	tests := []struct {
		name    string
		lookup  MessageLookup
		channel uint64
		message uint64
		wantErr bool
	}{
		{name: "raw", lookup: MessageLookup{GuildID: guild, Input: "123456789012345678"}, message: 123456789012345678},
		{name: "raw with channel", lookup: MessageLookup{GuildID: guild, Input: "123456789012345678", ChannelID: 123456789012345679}, channel: 123456789012345679, message: 123456789012345678},
		{name: "link", lookup: MessageLookup{GuildID: guild, Input: "https://discord.com/channels/123456789012345670/123456789012345679/123456789012345678"}, channel: 123456789012345679, message: 123456789012345678},
		{name: "canary", lookup: MessageLookup{GuildID: guild, Input: "https://canary.discord.com/channels/123456789012345670/123456789012345679/123456789012345678"}, channel: 123456789012345679, message: 123456789012345678},
		{name: "other guild", lookup: MessageLookup{GuildID: guild, Input: "https://discord.com/channels/223456789012345670/123456789012345679/123456789012345678"}, wantErr: true},
		{name: "untrusted host", lookup: MessageLookup{GuildID: guild, Input: "https://example.com/channels/123456789012345670/123456789012345679/123456789012345678"}, wantErr: true},
		{name: "query", lookup: MessageLookup{GuildID: guild, Input: "https://discord.com/channels/123456789012345670/123456789012345679/123456789012345678?x=1"}, wantErr: true},
		{name: "malformed", lookup: MessageLookup{GuildID: guild, Input: "not-an-id"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, channel, message, err := parseMessageTarget(test.lookup)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v", err)
			}
			if err == nil && (uint64(channel) != test.channel || uint64(message) != test.message) {
				t.Fatalf("channel=%d message=%d", channel, message)
			}
		})
	}
}

type messageDeletionRESTStub struct {
	guild    *disgocord.RestGuild
	members  map[snowflake.ID]*disgocord.Member
	channels []disgocord.GuildChannel
	messages map[snowflake.ID]*disgocord.Message
	getCalls atomic.Int64
	deleted  *disgocord.Message
}

func (stub *messageDeletionRESTStub) GetGuild(snowflake.ID, bool, ...rest.RequestOpt) (*disgocord.RestGuild, error) {
	return stub.guild, nil
}
func (stub *messageDeletionRESTStub) GetMember(_ snowflake.ID, id snowflake.ID, _ ...rest.RequestOpt) (*disgocord.Member, error) {
	return stub.members[id], nil
}
func (stub *messageDeletionRESTStub) GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]disgocord.GuildChannel, error) {
	return stub.channels, nil
}
func (stub *messageDeletionRESTStub) GetActiveGuildThreads(snowflake.ID, ...rest.RequestOpt) (*disgocord.GuildActiveThreads, error) {
	return &disgocord.GuildActiveThreads{}, nil
}
func (stub *messageDeletionRESTStub) GetChannel(id snowflake.ID, _ ...rest.RequestOpt) (disgocord.Channel, error) {
	for _, channel := range stub.channels {
		if channel.ID() == id {
			return channel, nil
		}
	}
	return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownChannel}
}
func (stub *messageDeletionRESTStub) GetMessage(channelID, _ snowflake.ID, _ ...rest.RequestOpt) (*disgocord.Message, error) {
	stub.getCalls.Add(1)
	if message := stub.messages[channelID]; message != nil {
		copyMessage := *message
		return &copyMessage, nil
	}
	return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownMessage}
}
func (stub *messageDeletionRESTStub) DeleteMessage(channelID, messageID snowflake.ID, _ ...rest.RequestOpt) error {
	message := stub.messages[channelID]
	if message == nil || message.ID != messageID {
		return &rest.Error{Code: rest.JSONErrorCodeUnknownMessage}
	}
	stub.deleted = message
	delete(stub.messages, channelID)
	return nil
}

func TestDiscordMessageDeletionSearchesBoundedlyAndRevalidates(t *testing.T) {
	api := newMessageDeletionRESTStub(t, 6)
	executor := discordMessageDeletion{rest: api, botUserID: 6}
	target, err := executor.ResolveMessage(context.Background(), MessageLookup{GuildID: 1, ModeratorID: 2, Input: "123456789012345678"})
	if err != nil {
		t.Fatal(err)
	}
	if target.ChannelID != 101 || target.AuthorID != 4 || api.getCalls.Load() > int64(len(api.channels)) {
		t.Fatalf("target=%+v calls=%d", target, api.getCalls.Load())
	}
	if err := executor.DeleteMessage(context.Background(), MessageDeleteExecution{CaseID: 7, ModeratorID: 2, Target: target, Reason: "Remove spam"}); err != nil {
		t.Fatal(err)
	}
	if api.deleted == nil {
		t.Fatal("message was not deleted")
	}
}

func TestDiscordMessageDeletionRequiresBotAndModeratorPermission(t *testing.T) {
	api := newMessageDeletionRESTStub(t, 6)
	api.guild.Roles[1].Permissions = disgocord.PermissionViewChannel | disgocord.PermissionReadMessageHistory
	executor := discordMessageDeletion{rest: api, botUserID: 6}
	_, err := executor.ResolveMessage(context.Background(), MessageLookup{GuildID: 1, ModeratorID: 2, ChannelID: 101, MessageID: 123456789012345678})
	if !errors.Is(err, ErrMessageAccess) {
		t.Fatalf("expected bot access error, got %v", err)
	}

	api = newMessageDeletionRESTStub(t, 6)
	api.guild.Roles[0].Permissions = 0
	executor.rest = api
	_, err = executor.ResolveMessage(context.Background(), MessageLookup{GuildID: 1, ModeratorID: 2, ChannelID: 101, MessageID: 123456789012345678})
	if !errors.Is(err, ErrModerationPermission) {
		t.Fatalf("expected moderator permission error, got %v", err)
	}
}

func TestDiscordMessageDeletionAuditReportsPermissionGaps(t *testing.T) {
	api := newMessageDeletionRESTStub(t, 6)
	executor := discordMessageDeletion{rest: api, botUserID: 6}
	access, err := executor.AuditMessageDeletionAccess(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if access.Channels != 3 || access.Gaps != 0 {
		t.Fatalf("access = %+v", access)
	}
	api.guild.Roles[1].Permissions = disgocord.PermissionViewChannel | disgocord.PermissionReadMessageHistory
	access, err = executor.AuditMessageDeletionAccess(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if access.Gaps != access.Channels {
		t.Fatalf("access = %+v, want every channel reported", access)
	}
}

func TestMessageDeletionClassifiesDiscordResponses(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusForbidden, want: ErrMessageAccess},
		{status: http.StatusNotFound, want: ErrMessageNotFound},
		{status: http.StatusTooManyRequests, want: ErrMessageRateLimited},
		{status: http.StatusBadGateway, want: ErrMessageTemporary},
	}
	for _, test := range tests {
		err := classifyMessageRESTError(&rest.Error{Response: &http.Response{StatusCode: test.status}})
		if !errors.Is(err, test.want) {
			t.Fatalf("status %d classified as %v, want %v", test.status, err, test.want)
		}
	}
}

func newMessageDeletionRESTStub(t *testing.T, botID snowflake.ID) *messageDeletionRESTStub {
	t.Helper()
	channels := []disgocord.GuildChannel{testGuildChannel(t, 100), testGuildChannel(t, 101), testGuildChannel(t, 102)}
	permissions := disgocord.PermissionViewChannel | disgocord.PermissionReadMessageHistory | disgocord.PermissionManageMessages
	created := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	guildID := snowflake.ID(1)
	messageID := snowflake.ID(123456789012345678)
	return &messageDeletionRESTStub{
		guild: &disgocord.RestGuild{Guild: disgocord.Guild{ID: guildID, OwnerID: 3}, Roles: []disgocord.Role{
			{ID: 10, Permissions: permissions}, {ID: 11, Permissions: permissions},
		}},
		members: map[snowflake.ID]*disgocord.Member{
			2:     {User: disgocord.User{ID: 2}, RoleIDs: []snowflake.ID{10}},
			botID: {User: disgocord.User{ID: botID}, RoleIDs: []snowflake.ID{11}},
		},
		channels: channels,
		messages: map[snowflake.ID]*disgocord.Message{
			101: {ID: messageID, GuildID: &guildID, ChannelID: 101, Author: disgocord.User{ID: 4}, CreatedAt: created, Content: "spam"},
		},
	}
}

func testGuildChannel(t *testing.T, id snowflake.ID) disgocord.GuildChannel {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"%d","guild_id":"1","type":0,"name":"channel-%d","position":0,"permission_overwrites":[]}`, id, id)
	var value disgocord.UnmarshalChannel
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	channel, ok := value.Channel.(disgocord.GuildChannel)
	if !ok {
		t.Fatalf("not a guild channel: %T", value.Channel)
	}
	return channel
}
