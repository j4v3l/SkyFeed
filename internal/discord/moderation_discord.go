package discord

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode"

	"github.com/disgoorg/disgo/bot"
	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
)

var (
	ErrModerationPermission = errors.New("required Discord permission is missing")
	ErrModerationHierarchy  = errors.New("discord role hierarchy blocks the action")
	ErrModerationOwner      = errors.New("the server owner cannot be moderated")
	ErrModerationSelf       = errors.New("the moderator, bot, and target must be different users")
)

type moderationREST interface {
	GetGuild(snowflake.ID, bool, ...rest.RequestOpt) (*disgocord.RestGuild, error)
	GetMember(snowflake.ID, snowflake.ID, ...rest.RequestOpt) (*disgocord.Member, error)
	UpdateMember(snowflake.ID, snowflake.ID, disgocord.MemberUpdate, ...rest.RequestOpt) (*disgocord.Member, error)
	RemoveMember(snowflake.ID, snowflake.ID, ...rest.RequestOpt) error
	AddBan(snowflake.ID, snowflake.ID, time.Duration, ...rest.RequestOpt) error
	DeleteBan(snowflake.ID, snowflake.ID, ...rest.RequestOpt) error
	CreateDMChannel(snowflake.ID, ...rest.RequestOpt) (*disgocord.DMChannel, error)
	CreateMessage(snowflake.ID, disgocord.MessageCreate, ...rest.RequestOpt) (*disgocord.Message, error)
}

type discordModerationExecutor struct {
	rest      moderationREST
	botUserID snowflake.ID
}

func (service *GatewayService) ExecuteModeration(ctx context.Context, execution ModerationExecution) (ModerationResult, error) {
	client := service.client.Load()
	if client == nil {
		return ModerationResult{}, errors.New("discord moderation is not ready")
	}
	executor := discordModerationExecutor{rest: client.Rest, botUserID: botUserID(client)}
	return executor.ExecuteModeration(ctx, execution)
}

func botUserID(client *bot.Client) snowflake.ID {
	if id := client.ID(); id != 0 {
		return id
	}
	return client.ApplicationID
}

func (executor discordModerationExecutor) ExecuteModeration(ctx context.Context, execution ModerationExecution) (ModerationResult, error) {
	guildID := snowflake.ID(execution.GuildID)
	moderatorID := snowflake.ID(execution.ModeratorID)
	targetID := snowflake.ID(execution.TargetUserID)
	if moderatorID == targetID || targetID == executor.botUserID {
		return ModerationResult{}, ErrModerationSelf
	}
	guild, err := executor.rest.GetGuild(guildID, false, rest.WithCtx(ctx))
	if err != nil {
		return ModerationResult{}, fmt.Errorf("load guild moderation state: %w", err)
	}
	moderator, err := executor.rest.GetMember(guildID, moderatorID, rest.WithCtx(ctx))
	if err != nil {
		return ModerationResult{}, fmt.Errorf("load moderator state: %w", err)
	}
	botMember, err := executor.rest.GetMember(guildID, executor.botUserID, rest.WithCtx(ctx))
	if err != nil {
		return ModerationResult{}, fmt.Errorf("load bot state: %w", err)
	}
	required := requiredDiscordPermission(execution.Action)
	if required == 0 || (!memberPermissions(guildID, guild.Roles, moderator.RoleIDs).Has(required) && moderatorID != guild.OwnerID) {
		return ModerationResult{}, ErrModerationPermission
	}
	if !memberPermissions(guildID, guild.Roles, botMember.RoleIDs).Has(required) && execution.Action != "warn" {
		return ModerationResult{}, ErrModerationPermission
	}

	var target *disgocord.Member
	if execution.Action != "unban" {
		target, err = executor.rest.GetMember(guildID, targetID, rest.WithCtx(ctx))
		if err != nil {
			return ModerationResult{}, fmt.Errorf("load target state: %w", err)
		}
		if targetID == guild.OwnerID {
			return ModerationResult{}, ErrModerationOwner
		}
		if moderatorID != guild.OwnerID && highestRolePosition(guild.Roles, moderator.RoleIDs) <= highestRolePosition(guild.Roles, target.RoleIDs) {
			return ModerationResult{}, ErrModerationHierarchy
		}
		if highestRolePosition(guild.Roles, botMember.RoleIDs) <= highestRolePosition(guild.Roles, target.RoleIDs) {
			return ModerationResult{}, ErrModerationHierarchy
		}
		if execution.Action == "timeout" && memberPermissions(guildID, guild.Roles, target.RoleIDs).Has(disgocord.PermissionAdministrator) {
			return ModerationResult{}, ErrModerationHierarchy
		}
	}

	auditReason := render.Truncate(stringsMapNoControls(fmt.Sprintf("SkyFeed case %d: %s", execution.CaseID, execution.Reason)), 480)
	requestOptions := []rest.RequestOpt{rest.WithCtx(ctx), rest.WithReason(auditReason)}
	switch execution.Action {
	case "warn":
		dmStatus := "delivered"
		channel, dmErr := executor.rest.CreateDMChannel(targetID, rest.WithCtx(ctx))
		if dmErr == nil {
			mentions := disgocord.AllowedMentions{}
			message := disgocord.NewMessageCreate().WithContent(fmt.Sprintf("SkyFeed moderation warning • case %d\nServer: `%d`\nReason: %s", execution.CaseID, execution.GuildID, render.PlainText(execution.Reason))).WithAllowedMentions(&mentions)
			_, dmErr = executor.rest.CreateMessage(channel.ID(), message, rest.WithCtx(ctx))
		}
		if dmErr != nil {
			dmStatus = "failed"
		}
		return ModerationResult{DMStatus: dmStatus}, nil
	case "timeout":
		until := time.Now().UTC().Add(execution.Duration)
		_, err = executor.rest.UpdateMember(guildID, targetID, disgocord.MemberUpdate{CommunicationDisabledUntil: omit.New(&until)}, requestOptions...)
	case "remove-timeout":
		_, err = executor.rest.UpdateMember(guildID, targetID, disgocord.MemberUpdate{CommunicationDisabledUntil: omit.NewNilPtr[time.Time]()}, requestOptions...)
	case "kick":
		err = executor.rest.RemoveMember(guildID, targetID, requestOptions...)
	case "ban":
		err = executor.rest.AddBan(guildID, targetID, execution.DeleteMessageDuration, requestOptions...)
	case "unban":
		err = executor.rest.DeleteBan(guildID, targetID, requestOptions...)
	default:
		err = errors.New("unsupported moderation action")
	}
	if err != nil {
		return ModerationResult{DMStatus: "not-attempted"}, fmt.Errorf("execute %s: %w", execution.Action, err)
	}
	return ModerationResult{DMStatus: "not-attempted"}, nil
}

func stringsMapNoControls(value string) string {
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if !unicode.IsControl(r) {
			result = append(result, r)
		}
	}
	return string(result)
}

func requiredDiscordPermission(action string) disgocord.Permissions {
	switch action {
	case "warn", "timeout", "remove-timeout":
		return disgocord.PermissionModerateMembers
	case "kick":
		return disgocord.PermissionKickMembers
	case "ban", "unban":
		return disgocord.PermissionBanMembers
	default:
		return 0
	}
}

func memberPermissions(guildID snowflake.ID, roles []disgocord.Role, roleIDs []snowflake.ID) disgocord.Permissions {
	permissions := disgocord.Permissions(0)
	for _, role := range roles {
		if role.ID == guildID || containsSnowflake(roleIDs, role.ID) {
			permissions = permissions.Add(role.Permissions)
		}
	}
	if permissions.Has(disgocord.PermissionAdministrator) {
		return disgocord.PermissionsAll
	}
	return permissions
}

func highestRolePosition(roles []disgocord.Role, roleIDs []snowflake.ID) int {
	highest := 0
	for _, role := range roles {
		if containsSnowflake(roleIDs, role.ID) && role.Position > highest {
			highest = role.Position
		}
	}
	return highest
}

func containsSnowflake(values []snowflake.ID, wanted snowflake.ID) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
