package discord

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/disgoorg/disgo/bot"
	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
)

const (
	messageSearchLimit   = 100
	messageSearchWorkers = 4
)

type messageDeletionREST interface {
	GetGuild(snowflake.ID, bool, ...rest.RequestOpt) (*disgocord.RestGuild, error)
	GetMember(snowflake.ID, snowflake.ID, ...rest.RequestOpt) (*disgocord.Member, error)
	GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]disgocord.GuildChannel, error)
	GetActiveGuildThreads(snowflake.ID, ...rest.RequestOpt) (*disgocord.GuildActiveThreads, error)
	GetChannel(snowflake.ID, ...rest.RequestOpt) (disgocord.Channel, error)
	GetMessage(snowflake.ID, snowflake.ID, ...rest.RequestOpt) (*disgocord.Message, error)
	DeleteMessage(snowflake.ID, snowflake.ID, ...rest.RequestOpt) error
}

type discordMessageDeletion struct {
	rest      messageDeletionREST
	botUserID snowflake.ID
}

func (service *GatewayService) ResolveMessage(ctx context.Context, lookup MessageLookup) (MessageTarget, error) {
	client := service.client.Load()
	if client == nil {
		return MessageTarget{}, errors.New("discord message deletion is not ready")
	}
	executor := discordMessageDeletion{rest: client.Rest, botUserID: botUserID(client)}
	return executor.ResolveMessage(ctx, lookup)
}

func (service *GatewayService) DeleteMessage(ctx context.Context, execution MessageDeleteExecution) error {
	client := service.client.Load()
	if client == nil {
		return errors.New("discord message deletion is not ready")
	}
	executor := discordMessageDeletion{rest: client.Rest, botUserID: botUserID(client)}
	return executor.DeleteMessage(ctx, execution)
}

func (service *GatewayService) AuditMessageDeletionAccess(ctx context.Context, guildID uint64) (MessageDeletionAccess, error) {
	client := service.client.Load()
	if client == nil {
		return MessageDeletionAccess{}, errors.New("discord message deletion is not ready")
	}
	executor := discordMessageDeletion{rest: client.Rest, botUserID: botUserID(client)}
	return executor.AuditMessageDeletionAccess(ctx, snowflake.ID(guildID))
}

func (executor discordMessageDeletion) ResolveMessage(ctx context.Context, lookup MessageLookup) (MessageTarget, error) {
	guildID, channelID, messageID, err := parseMessageTarget(lookup)
	if err != nil {
		return MessageTarget{}, err
	}
	var message *disgocord.Message
	if channelID != 0 {
		message, err = executor.rest.GetMessage(channelID, messageID, rest.WithCtx(ctx))
		if err != nil {
			return MessageTarget{}, classifyMessageRESTError(err)
		}
	} else {
		channelID, message, err = executor.searchMessage(ctx, guildID, messageID)
		if err != nil {
			return MessageTarget{}, err
		}
	}
	if message == nil || message.ID != messageID || message.ChannelID != channelID || message.GuildID == nil || *message.GuildID != guildID {
		return MessageTarget{}, ErrMessageInvalidTarget
	}
	if err := executor.requireManageMessages(ctx, guildID, channelID, snowflake.ID(lookup.ModeratorID)); err != nil {
		return MessageTarget{}, err
	}
	return messageTarget(*message), nil
}

func (executor discordMessageDeletion) DeleteMessage(ctx context.Context, execution MessageDeleteExecution) error {
	target := execution.Target
	if target.GuildID == 0 || target.ChannelID == 0 || target.MessageID == 0 || execution.ModeratorID == 0 {
		return ErrMessageInvalidTarget
	}
	if err := executor.requireManageMessages(ctx, snowflake.ID(target.GuildID), snowflake.ID(target.ChannelID), snowflake.ID(execution.ModeratorID)); err != nil {
		return err
	}
	message, err := executor.rest.GetMessage(snowflake.ID(target.ChannelID), snowflake.ID(target.MessageID), rest.WithCtx(ctx))
	if err != nil {
		return classifyMessageRESTError(err)
	}
	if uint64(message.Author.ID) != target.AuthorID || !message.CreatedAt.Equal(target.CreatedAt) {
		return ErrMessageInvalidTarget
	}
	auditReason := render.Truncate(stringsMapNoControls(fmt.Sprintf("SkyFeed case %d: %s", execution.CaseID, execution.Reason)), 480)
	if err := executor.rest.DeleteMessage(snowflake.ID(target.ChannelID), snowflake.ID(target.MessageID), rest.WithCtx(ctx), rest.WithReason(auditReason)); err != nil {
		return classifyMessageRESTError(err)
	}
	return nil
}

func (executor discordMessageDeletion) AuditMessageDeletionAccess(ctx context.Context, guildID snowflake.ID) (MessageDeletionAccess, error) {
	guild, err := executor.rest.GetGuild(guildID, false, rest.WithCtx(ctx))
	if err != nil {
		return MessageDeletionAccess{}, err
	}
	botMember, err := executor.rest.GetMember(guildID, executor.botUserID, rest.WithCtx(ctx))
	if err != nil {
		return MessageDeletionAccess{}, err
	}
	channels, err := executor.rest.GetGuildChannels(guildID, rest.WithCtx(ctx))
	if err != nil {
		return MessageDeletionAccess{}, err
	}
	active, err := executor.rest.GetActiveGuildThreads(guildID, rest.WithCtx(ctx))
	if err != nil {
		return MessageDeletionAccess{}, err
	}
	byID := make(map[snowflake.ID]disgocord.GuildChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID()] = channel
	}
	result := MessageDeletionAccess{}
	check := func(channel disgocord.GuildChannel) {
		result.Channels++
		permissionChannel := channel
		if isThreadType(channel.Type()) && channel.ParentID() != nil {
			if parent := byID[*channel.ParentID()]; parent != nil {
				permissionChannel = parent
			}
		}
		permissions := effectivePermissions(guildID, guild.Roles, *botMember, permissionChannel)
		if !permissions.Has(disgocord.PermissionViewChannel, disgocord.PermissionReadMessageHistory, disgocord.PermissionManageMessages) {
			result.Gaps++
		}
	}
	for _, channel := range channels {
		if _, ok := channel.(disgocord.GuildMessageChannel); ok {
			check(channel)
		}
	}
	if active != nil {
		for index := range active.Threads {
			check(active.Threads[index])
		}
	}
	return result, nil
}

func parseMessageTarget(lookup MessageLookup) (guildID, channelID, messageID snowflake.ID, err error) {
	guildID = snowflake.ID(lookup.GuildID)
	channelID = snowflake.ID(lookup.ChannelID)
	messageID = snowflake.ID(lookup.MessageID)
	if guildID == 0 {
		return 0, 0, 0, ErrMessageInvalidTarget
	}
	input := strings.TrimSpace(lookup.Input)
	if messageID != 0 {
		return guildID, channelID, messageID, nil
	}
	if input == "" {
		return 0, 0, 0, ErrMessageInvalidTarget
	}
	if strings.Contains(input, "://") {
		parsed, parseErr := url.Parse(input)
		if parseErr != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			!slices.Contains([]string{"discord.com", "www.discord.com", "ptb.discord.com", "canary.discord.com"}, strings.ToLower(parsed.Hostname())) {
			return 0, 0, 0, ErrMessageInvalidTarget
		}
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) != 4 || parts[0] != "channels" {
			return 0, 0, 0, ErrMessageInvalidTarget
		}
		linkGuild, guildErr := snowflake.Parse(parts[1])
		linkChannel, channelErr := snowflake.Parse(parts[2])
		linkMessage, messageErr := snowflake.Parse(parts[3])
		if guildErr != nil || channelErr != nil || messageErr != nil || linkGuild != guildID || (channelID != 0 && channelID != linkChannel) {
			return 0, 0, 0, ErrMessageInvalidTarget
		}
		return guildID, linkChannel, linkMessage, nil
	}
	parsedMessage, parseErr := snowflake.Parse(input)
	if parseErr != nil || parsedMessage == 0 {
		return 0, 0, 0, ErrMessageInvalidTarget
	}
	return guildID, channelID, parsedMessage, nil
}

func (executor discordMessageDeletion) searchMessage(ctx context.Context, guildID, messageID snowflake.ID) (snowflake.ID, *disgocord.Message, error) {
	channels, err := executor.rest.GetGuildChannels(guildID, rest.WithCtx(ctx))
	if err != nil {
		return 0, nil, classifyMessageRESTError(err)
	}
	active, activeErr := executor.rest.GetActiveGuildThreads(guildID, rest.WithCtx(ctx))
	if activeErr != nil && !isMessageMissingREST(activeErr) {
		return 0, nil, classifyMessageRESTError(activeErr)
	}
	ids := make([]snowflake.ID, 0, min(len(channels)+32, messageSearchLimit+1))
	for _, channel := range channels {
		if _, ok := channel.(disgocord.GuildMessageChannel); ok {
			ids = append(ids, channel.ID())
		}
	}
	if active != nil {
		for _, thread := range active.Threads {
			ids = append(ids, thread.ID())
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > messageSearchLimit {
		return 0, nil, ErrMessageSearchTooLarge
	}
	type foundMessage struct {
		channel snowflake.ID
		message *disgocord.Message
	}
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan snowflake.ID)
	found := make(chan foundMessage, 1)
	failure := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Add(messageSearchWorkers)
	for range messageSearchWorkers {
		go func() {
			defer workers.Done()
			for channel := range jobs {
				message, getErr := executor.rest.GetMessage(channel, messageID, rest.WithCtx(searchCtx))
				if getErr == nil {
					select {
					case found <- foundMessage{channel: channel, message: message}:
						cancel()
					default:
					}
					return
				}
				if searchCtx.Err() != nil {
					return
				}
				if !isMessageMissingREST(getErr) && !isMessageAccessREST(getErr) {
					select {
					case failure <- getErr:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, channel := range ids {
			select {
			case jobs <- channel:
			case <-searchCtx.Done():
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case result := <-found:
		cancel()
		<-done
		return result.channel, result.message, nil
	case searchErr := <-failure:
		cancel()
		<-done
		return 0, nil, classifyMessageRESTError(searchErr)
	case <-done:
		return 0, nil, ErrMessageNotFound
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (executor discordMessageDeletion) requireManageMessages(ctx context.Context, guildID, channelID, moderatorID snowflake.ID) error {
	guild, err := executor.rest.GetGuild(guildID, false, rest.WithCtx(ctx))
	if err != nil {
		return classifyMessageRESTError(err)
	}
	moderator, err := executor.rest.GetMember(guildID, moderatorID, rest.WithCtx(ctx))
	if err != nil {
		return classifyMessageRESTError(err)
	}
	botMember, err := executor.rest.GetMember(guildID, executor.botUserID, rest.WithCtx(ctx))
	if err != nil {
		return classifyMessageRESTError(err)
	}
	channel, err := executor.rest.GetChannel(channelID, rest.WithCtx(ctx))
	if err != nil {
		return classifyMessageRESTError(err)
	}
	guildChannel, ok := channel.(disgocord.GuildChannel)
	if !ok || guildChannel.GuildID() != guildID {
		return ErrMessageInvalidTarget
	}
	permissionChannel := guildChannel
	if isThreadType(guildChannel.Type()) && guildChannel.ParentID() != nil {
		parent, parentErr := executor.rest.GetChannel(*guildChannel.ParentID(), rest.WithCtx(ctx))
		if parentErr != nil {
			return classifyMessageRESTError(parentErr)
		}
		if parentGuild, parentOK := parent.(disgocord.GuildChannel); parentOK {
			permissionChannel = parentGuild
		}
	}
	if moderatorID != guild.OwnerID && !effectivePermissions(guildID, guild.Roles, *moderator, permissionChannel).Has(disgocord.PermissionManageMessages) {
		return ErrModerationPermission
	}
	botPermissions := effectivePermissions(guildID, guild.Roles, *botMember, permissionChannel)
	if !botPermissions.Has(disgocord.PermissionViewChannel, disgocord.PermissionReadMessageHistory, disgocord.PermissionManageMessages) {
		return ErrMessageAccess
	}
	return nil
}

func effectivePermissions(guildID snowflake.ID, roles []disgocord.Role, member disgocord.Member, channel disgocord.GuildChannel) disgocord.Permissions {
	permissions := memberPermissions(guildID, roles, member.RoleIDs)
	if permissions.Has(disgocord.PermissionAdministrator) {
		return disgocord.PermissionsAll
	}
	if overwrite, ok := channel.PermissionOverwrites().Role(guildID); ok {
		permissions &= ^overwrite.Deny
		permissions |= overwrite.Allow
	}
	var allow, deny disgocord.Permissions
	for _, roleID := range member.RoleIDs {
		if overwrite, ok := channel.PermissionOverwrites().Role(roleID); ok {
			allow |= overwrite.Allow
			deny |= overwrite.Deny
		}
	}
	permissions &= ^deny
	permissions |= allow
	if overwrite, ok := channel.PermissionOverwrites().Member(member.User.ID); ok {
		permissions &= ^overwrite.Deny
		permissions |= overwrite.Allow
	}
	return permissions
}

func isThreadType(channelType disgocord.ChannelType) bool {
	return channelType == disgocord.ChannelTypeGuildNewsThread || channelType == disgocord.ChannelTypeGuildPublicThread || channelType == disgocord.ChannelTypeGuildPrivateThread
}

func messageTarget(message disgocord.Message) MessageTarget {
	guildID := uint64(0)
	if message.GuildID != nil {
		guildID = uint64(*message.GuildID)
	}
	preview := strings.TrimSpace(message.Content)
	if preview == "" && len(message.Embeds) > 0 {
		preview = strings.TrimSpace(message.Embeds[0].Title + " " + message.Embeds[0].Description)
	}
	if preview == "" && len(message.Attachments) > 0 {
		preview = fmt.Sprintf("%d attachment(s)", len(message.Attachments))
	}
	return MessageTarget{
		GuildID: guildID, ChannelID: uint64(message.ChannelID), MessageID: uint64(message.ID), AuthorID: uint64(message.Author.ID),
		CreatedAt: message.CreatedAt.UTC(), Preview: preview, Pinned: message.Pinned,
	}
}

func classifyMessageRESTError(err error) error {
	if isMessageMissingREST(err) {
		return errors.Join(ErrMessageNotFound, err)
	}
	if isMessageAccessREST(err) {
		return errors.Join(ErrMessageAccess, err)
	}
	var discordErr *rest.Error
	if errors.As(err, &discordErr) && discordErr.Response != nil {
		switch {
		case discordErr.Response.StatusCode == http.StatusTooManyRequests:
			return errors.Join(ErrMessageRateLimited, err)
		case discordErr.Response.StatusCode >= 500:
			return errors.Join(ErrMessageTemporary, err)
		}
	}
	return err
}

func isMessageMissingREST(err error) bool {
	var discordErr *rest.Error
	return errors.As(err, &discordErr) && (discordErr.Code == rest.JSONErrorCodeUnknownMessage || discordErr.Code == rest.JSONErrorCodeUnknownChannel || discordErr.Response != nil && discordErr.Response.StatusCode == http.StatusNotFound)
}

func isMessageAccessREST(err error) bool {
	var discordErr *rest.Error
	return errors.As(err, &discordErr) && (discordErr.Code == rest.JSONErrorCodeMissingAccess || discordErr.Code == rest.JSONErrorCodeLackPermissionsToPerformAction || discordErr.Response != nil && discordErr.Response.StatusCode == http.StatusForbidden)
}

var _ MessageDeletionExecutor = (*GatewayService)(nil)
var _ MessageDeletionAuditor = (*GatewayService)(nil)
var _ messageDeletionREST = (bot.Client{}).Rest
