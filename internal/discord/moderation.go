package discord

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const moderationConfirmationTTL = time.Minute

type ModerationExecution struct {
	CaseID                int64
	GuildID               uint64
	ChannelID             uint64
	ModeratorID           uint64
	TargetUserID          uint64
	Action                string
	Reason                string
	Duration              time.Duration
	DeleteMessageDuration time.Duration
}

type ModerationResult struct {
	DMStatus string
}

type ModerationExecutor interface {
	ExecuteModeration(context.Context, ModerationExecution) (ModerationResult, error)
}

func (router *Router) handleModeration(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil || router.moderation == nil {
		return responder.CreateMessage(errorMessage("Moderation is not available while durable storage or Discord delivery is offline."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Moderation storage is temporarily unavailable."))
	}
	if !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "moderator") {
		return responder.CreateMessage(errorMessage("A configured Moderator or Admin role is required for moderation."))
	}
	if !hasNativeModerationPermission(request.Permissions, request.Administrator, request.Subcommand) {
		return responder.CreateMessage(errorMessage(nativeModerationPermissionMessage(request.Subcommand)))
	}

	switch request.Subcommand {
	case "case":
		return router.showModerationCase(ctx, request, responder)
	case "history":
		return router.showModerationHistory(ctx, request, responder)
	}

	execution, err := moderationExecutionFromCommand(request)
	if err != nil {
		return responder.CreateMessage(errorMessage(err.Error()))
	}
	if execution.TargetUserID == request.UserID {
		return responder.CreateMessage(errorMessage("You cannot moderate yourself."))
	}
	if execution.Action == "kick" || execution.Action == "ban" {
		return router.confirmModeration(execution, responder)
	}
	return router.executeModeration(execution, responder, false)
}

func (router *Router) handleModerationComponent(request ComponentRequest, responder InteractionResponder, session Session, action string) error {
	if action == "moderate-cancel" {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(infoMessage("Moderation cancelled", "No Discord action was performed and no case was created.")).ClearComponents())
	}
	if action != "moderate-confirm" {
		return responder.CreateMessage(errorMessage("This moderation control is invalid or expired."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "moderator") {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(errorMessage("Your Moderator or Admin role authorization changed before confirmation.")).ClearComponents())
	}
	if !hasNativeModerationPermission(request.Permissions, request.Administrator, session.Action) {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(errorMessage(nativeModerationPermissionMessage(session.Action))).ClearComponents())
	}
	router.sessions.Delete(session.ID)
	execution := ModerationExecution{
		GuildID: request.GuildID, ChannelID: request.ChannelID, ModeratorID: request.UserID,
		TargetUserID: session.TargetID, Action: session.Action, Reason: session.Reason, Duration: session.Duration, DeleteMessageDuration: session.DeleteMessageDuration,
	}
	return router.executeModeration(execution, responder, true)
}

func (router *Router) confirmModeration(execution ModerationExecution, responder InteractionResponder) error {
	session, err := router.sessions.CreateWithTTL(execution.ModeratorID, execution.GuildID, execution.ChannelID, "moderation", "", "", "", moderationConfirmationTTL)
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active confirmation requests. Try again after an older request expires."))
	}
	session.Action = execution.Action
	session.TargetID = execution.TargetUserID
	session.Reason = execution.Reason
	session.Duration = execution.Duration
	session.DeleteMessageDuration = execution.DeleteMessageDuration
	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("The confirmation session could not be created."))
	}
	confirmID, err := CustomID(session.ID, "moderate-confirm")
	if err != nil {
		return err
	}
	cancelID, err := CustomID(session.ID, "moderate-cancel")
	if err != nil {
		return err
	}
	embed := render.Card(render.CardModel{
		View: "Confirm moderation", Status: "🔴 **CONFIRM REQUIRED**",
		Purpose: fmt.Sprintf("Review this private %s action before it is sent to Discord.", render.PlainText(execution.Action)),
		Color:   render.EmergencyColor, Timestamp: router.now(),
		Sections: []render.FactGroup{
			{Title: "👤 Member", Lines: []string{fmt.Sprintf("**User ID** `%d`", execution.TargetUserID), fmt.Sprintf("**Action** %s", render.PlainText(execution.Action))}},
			{Title: "📝 Reason", Lines: []string{render.Truncate(render.PlainText(execution.Reason), 400)}},
			{Title: "⏳ Confirmation window", Lines: []string{fmt.Sprintf("Expires <t:%d:R>", session.ExpiresAt.Unix())}},
		},
	})
	message := render.SafeMessage(render.BoundEmbed(embed), true).
		AddActionRow(disgocord.NewDangerButton("Confirm "+execution.Action, confirmID), disgocord.NewSecondaryButton("Cancel", cancelID))
	return responder.CreateMessage(message)
}

func (router *Router) executeModeration(execution ModerationExecution, responder InteractionResponder, update bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	moderationCase, err := router.repository.CreateModerationCase(ctx, storage.ModerationCase{
		GuildID: execution.GuildID, ModeratorID: execution.ModeratorID, TargetUserID: execution.TargetUserID,
		Action: execution.Action, Reason: execution.Reason, Duration: execution.Duration, DeleteMessageDuration: execution.DeleteMessageDuration, CreatedAt: router.now().UTC(),
	})
	if err != nil {
		return router.respondModeration(responder, errorMessage("The moderation case could not be created; no Discord action was attempted."), update)
	}
	execution.CaseID = moderationCase.ID
	result, actionErr := router.moderation.ExecuteModeration(ctx, execution)
	status, errorCode := "succeeded", ""
	if actionErr != nil {
		status, errorCode = "failed", moderationErrorCode(actionErr)
	}
	dmStatus := result.DMStatus
	if dmStatus == "" {
		dmStatus = "not-attempted"
	}
	if err := router.repository.CompleteModerationCase(ctx, moderationCase.ID, execution.GuildID, status, dmStatus, errorCode, router.now().UTC()); err != nil {
		return router.respondModeration(responder, errorMessage(fmt.Sprintf("Discord may have processed the action, but case %d could not be finalized. Contact an administrator before retrying.", moderationCase.ID)), update)
	}
	if actionErr != nil {
		return router.respondModeration(responder, errorMessage(fmt.Sprintf("Case %d recorded a failed %s action. Check role hierarchy and bot permissions before retrying.", moderationCase.ID, execution.Action)), update)
	}
	description := fmt.Sprintf("Case %d completed **%s** for user ID `%d`. Reason: %s", moderationCase.ID, execution.Action, execution.TargetUserID, render.Truncate(render.PlainText(execution.Reason), 250))
	if execution.Action == "warn" {
		description += " DM delivery: **" + dmStatus + "**."
	}
	return router.respondModeration(responder, infoMessage("Moderation complete", description), update)
}

func (router *Router) respondModeration(responder InteractionResponder, message disgocord.MessageCreate, update bool) error {
	if update {
		return responder.UpdateMessage(messageUpdate(message).ClearComponents())
	}
	return responder.CreateMessage(message)
}

func (router *Router) showModerationCase(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	id := int64(request.Ints["case-id"])
	if id <= 0 {
		return responder.CreateMessage(errorMessage("Select a valid moderation case number."))
	}
	value, err := router.repository.ModerationCase(ctx, id, request.GuildID)
	if err != nil {
		return responder.CreateMessage(errorMessage("That moderation case does not exist in this server."))
	}
	return responder.CreateMessage(render.SafeMessage(render.ModerationCase(value), true))
}

func (router *Router) showModerationHistory(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	if _, err := router.repository.ModerationCases(ctx, request.GuildID, request.IDs["user"], 1); err != nil {
		return responder.CreateMessage(errorMessage("Moderation history could not be loaded."))
	}
	session, err := router.newStoredListSession(request, viewModerationHistory)
	if err != nil {
		return responder.CreateMessage(errorMessage(err.Error()))
	}
	message, err := router.storedListMessage(session)
	if err != nil {
		router.sessions.Delete(session.ID)
		return responder.CreateMessage(errorMessage("Moderation history could not be loaded."))
	}
	return responder.CreateMessage(message)
}

func moderationExecutionFromCommand(request CommandRequest) (ModerationExecution, error) {
	target := request.IDs["user"]
	if request.Subcommand == "unban" {
		parsed, err := strconv.ParseUint(strings.TrimSpace(request.Strings["user-id"]), 10, 64)
		if err != nil || parsed == 0 {
			return ModerationExecution{}, errors.New("enter a valid Discord user ID to unban")
		}
		target = parsed
	}
	if target == 0 {
		return ModerationExecution{}, errors.New("select a valid server member")
	}
	reason := strings.TrimSpace(request.Strings["reason"])
	if strings.IndexFunc(reason, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return ModerationExecution{}, errors.New("the reason contains unsupported control characters")
	}
	if count := utf8.RuneCountInString(reason); count < 3 || count > 400 {
		return ModerationExecution{}, errors.New("the reason must contain 3–400 characters")
	}
	duration := time.Duration(0)
	if request.Subcommand == "timeout" {
		var err error
		duration, err = time.ParseDuration(request.Strings["duration"])
		if err != nil || duration < time.Minute || duration > 28*24*time.Hour {
			return ModerationExecution{}, errors.New("choose a timeout duration from 1 minute through 28 days")
		}
	}
	deleteDuration := time.Duration(boundedInt(request.Ints["delete-message-days"], 0, 7, 0)) * 24 * time.Hour
	return ModerationExecution{
		GuildID: request.GuildID, ChannelID: request.ChannelID, ModeratorID: request.UserID,
		TargetUserID: target, Action: request.Subcommand, Reason: reason, Duration: duration, DeleteMessageDuration: deleteDuration,
	}, nil
}

func (router *Router) authorizedTier(ctx context.Context, guildID uint64, roleIDs []uint64, administrator bool, required string) bool {
	if administrator && required == "admin" {
		return true
	}
	bindings, err := router.repository.RoleBindings(ctx, guildID)
	if err != nil {
		return false
	}
	requiredRank := tierRank(required)
	for _, binding := range bindings {
		if tierRank(binding.Tier) < requiredRank || !containsID(roleIDs, binding.RoleID) {
			continue
		}
		return true
	}
	return false
}

func tierRank(tier string) int {
	switch tier {
	case "operator":
		return 1
	case "moderator":
		return 2
	case "admin":
		return 3
	default:
		return 0
	}
}

func containsID(values []uint64, wanted uint64) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hasNativeModerationPermission(permissions disgocord.Permissions, administrator bool, action string) bool {
	if administrator || permissions.Has(disgocord.PermissionAdministrator) {
		return true
	}
	switch action {
	case "kick":
		return permissions.Has(disgocord.PermissionKickMembers)
	case "ban", "unban":
		return permissions.Has(disgocord.PermissionBanMembers)
	case "case", "history":
		return permissions.Has(disgocord.PermissionModerateMembers) || permissions.Has(disgocord.PermissionKickMembers) || permissions.Has(disgocord.PermissionBanMembers)
	default:
		return permissions.Has(disgocord.PermissionModerateMembers)
	}
}

func nativeModerationPermissionMessage(action string) string {
	switch action {
	case "kick":
		return "Discord's Kick Members permission is required for this action."
	case "ban", "unban":
		return "Discord's Ban Members permission is required for this action."
	case "case", "history":
		return "A native Discord moderation permission is required to view cases."
	default:
		return "Discord's Moderate Members permission is required for this action."
	}
}

func moderationErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return "discord-action-failed"
}
