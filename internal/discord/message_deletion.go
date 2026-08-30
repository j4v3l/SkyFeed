package discord

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

var (
	ErrMessageNotFound       = errors.New("message not found")
	ErrMessageAccess         = errors.New("message channel is not accessible")
	ErrMessageSearchTooLarge = errors.New("message search exceeds the channel limit")
	ErrMessageInvalidTarget  = errors.New("invalid message target")
	ErrMessageRateLimited    = errors.New("message operation rate limited")
	ErrMessageTemporary      = errors.New("message operation temporarily unavailable")
)

type MessageLookup struct {
	GuildID     uint64
	ModeratorID uint64
	Input       string
	ChannelID   uint64
	MessageID   uint64
}

type MessageTarget struct {
	GuildID   uint64
	ChannelID uint64
	MessageID uint64
	AuthorID  uint64
	CreatedAt time.Time
	Preview   string
	Pinned    bool
}

type MessageDeleteExecution struct {
	CaseID      int64
	ModeratorID uint64
	Target      MessageTarget
	Reason      string
}

type MessageDeletionExecutor interface {
	ResolveMessage(context.Context, MessageLookup) (MessageTarget, error)
	DeleteMessage(context.Context, MessageDeleteExecution) error
}

type MessageDeletionAccess struct {
	Channels int
	Gaps     int
}

type MessageDeletionAuditor interface {
	AuditMessageDeletionAccess(context.Context, uint64) (MessageDeletionAccess, error)
}

func (router *Router) handleDeleteMessageContext(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil || router.messageDeletion == nil {
		return responder.CreateMessage(errorMessage("Message deletion is unavailable while storage or Discord delivery is offline."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Message deletion storage is temporarily unavailable."))
	}
	if !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "admin") {
		return responder.CreateMessage(errorMessage("A configured SkyFeed Admin role is required to delete messages."))
	}
	if !request.Administrator && !request.Permissions.Has(disgocord.PermissionManageMessages) {
		return responder.CreateMessage(errorMessage("Discord's Manage Messages permission is required in this channel."))
	}
	if request.IDs["message"] == 0 || request.IDs["channel"] == 0 {
		return responder.CreateMessage(errorMessage("That message is not available to this interaction."))
	}
	session, err := router.sessions.CreateWithTTL(request.UserID, request.GuildID, request.ChannelID, "message-delete", "", "", "", moderationConfirmationTTL)
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active confirmation requests. Try again shortly."))
	}
	session.TargetChannelID = request.IDs["channel"]
	session.TargetMessageID = request.IDs["message"]
	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("The deletion form could not be created."))
	}
	modalID, err := CustomID(session.ID, "delete-reason")
	if err != nil {
		return err
	}
	modal := disgocord.NewModalCreate(modalID, "Delete with SkyFeed").
		AddLabel("Reason", disgocord.NewParagraphTextInput("reason").WithPlaceholder("Why should this message be removed?").WithMinLength(3).WithMaxLength(400).WithRequired(true))
	return responder.ShowModal(modal)
}

func (router *Router) handleDeleteReasonModal(request ModalRequest, responder InteractionResponder, sessionID string) error {
	session, err := router.sessions.Get(sessionID, request.UserID, request.GuildID, request.ChannelID)
	if err != nil || session.View != "message-delete" {
		return responder.CreateMessage(errorMessage("This deletion form expired. Open the message action again."))
	}
	reason, err := validateModerationReason(request.Values["reason"])
	if err != nil {
		return responder.CreateMessage(errorMessage(err.Error()))
	}
	return router.prepareMessageDeletion(session, reason, "", responder)
}

func (router *Router) beginMessageDeletion(request CommandRequest, responder InteractionResponder) error {
	reason, err := validateModerationReason(request.Strings["reason"])
	if err != nil {
		return responder.CreateMessage(errorMessage(err.Error()))
	}
	session, err := router.sessions.CreateWithTTL(request.UserID, request.GuildID, request.ChannelID, "message-delete", "", "", "", moderationConfirmationTTL)
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active confirmation requests. Try again shortly."))
	}
	session.TargetChannelID = request.IDs["channel"]
	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("The deletion confirmation could not be created."))
	}
	return router.prepareMessageDeletion(session, reason, request.Strings["message"], responder)
}

func (router *Router) prepareMessageDeletion(session Session, reason, input string, responder InteractionResponder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	target, err := router.messageDeletion.ResolveMessage(ctx, MessageLookup{
		GuildID: session.GuildID, ModeratorID: session.UserID, Input: input,
		ChannelID: session.TargetChannelID, MessageID: session.TargetMessageID,
	})
	if err != nil {
		router.sessions.Delete(session.ID)
		return responder.CreateMessage(errorMessage(messageLookupError(err)))
	}
	session.TargetID = target.AuthorID
	session.TargetChannelID = target.ChannelID
	session.TargetMessageID = target.MessageID
	session.TargetMessageCreatedAt = target.CreatedAt
	session.TargetPreview = render.Truncate(strings.TrimSpace(target.Preview), 300)
	session.Reason = reason
	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("The deletion confirmation expired while the message was being loaded."))
	}
	confirmID, _ := CustomID(session.ID, "delete-confirm")
	cancelID, _ := CustomID(session.ID, "delete-cancel")
	preview := session.TargetPreview
	if preview == "" {
		preview = "No text preview (the message may contain only embeds or attachments)."
	}
	warnings := []string{"This action permanently removes one Discord message."}
	if target.Pinned {
		warnings = append(warnings, "The target message is pinned.")
	}
	if router.managedMessage(ctx, target) {
		warnings = append(warnings, "SkyFeed manages this message and may recreate it on its next refresh.")
	}
	embed := render.Card(render.CardModel{
		View: "Confirm message deletion", Status: "🔴 **CONFIRM REQUIRED**", Purpose: strings.Join(warnings, " "), Color: render.EmergencyColor, Timestamp: router.now(),
		Sections: []render.FactGroup{
			{Title: "💬 Target", Lines: []string{fmt.Sprintf("**Channel** <#%d>", target.ChannelID), fmt.Sprintf("**Message ID** `%d`", target.MessageID), fmt.Sprintf("**Author** `%d`", target.AuthorID)}},
			{Title: "📝 Preview", Lines: []string{render.PlainText(preview)}},
			{Title: "🧹 Reason", Lines: []string{render.PlainText(reason)}},
			{Title: "⏳ Confirmation window", Lines: []string{fmt.Sprintf("Expires <t:%d:R>", session.ExpiresAt.Unix())}},
		},
	})
	return responder.CreateMessage(render.SafeMessage(embed, true).AddActionRow(
		disgocord.NewDangerButton("Delete message", confirmID),
		disgocord.NewSecondaryButton("Cancel", cancelID),
	))
}

func (router *Router) handleDeleteMessageComponent(request ComponentRequest, responder InteractionResponder, session Session, action string) error {
	if action == "delete-cancel" {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(infoMessage("Deletion cancelled", "No Discord message was deleted and no moderation case was created.")).ClearComponents())
	}
	if action != "delete-confirm" {
		return responder.CreateMessage(errorMessage("This deletion control is invalid or expired."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "admin") {
		cancel()
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(errorMessage("Your SkyFeed Admin authorization changed before confirmation.")).ClearComponents())
	}
	cancel()
	router.sessions.Delete(session.ID)
	return router.executeMessageDeletion(session, responder)
}

func (router *Router) executeMessageDeletion(session Session, responder InteractionResponder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	moderationCase, err := router.repository.CreateModerationCase(ctx, storage.ModerationCase{
		GuildID: session.GuildID, ModeratorID: session.UserID, TargetUserID: session.TargetID,
		TargetChannelID: session.TargetChannelID, TargetMessageID: session.TargetMessageID, TargetMessageCreatedAt: session.TargetMessageCreatedAt,
		Action: "delete-message", Reason: session.Reason, CreatedAt: router.now().UTC(),
	})
	if err != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage("The moderation case could not be created, so no message was deleted.")).ClearComponents())
	}
	target, err := router.messageDeletion.ResolveMessage(ctx, MessageLookup{
		GuildID: session.GuildID, ModeratorID: session.UserID, ChannelID: session.TargetChannelID, MessageID: session.TargetMessageID,
	})
	if err != nil {
		return router.completeFailedMessageDeletion(ctx, moderationCase.ID, session.GuildID, err, messageLookupError(err), responder)
	}
	if target.AuthorID != session.TargetID || target.CreatedAt != session.TargetMessageCreatedAt {
		return router.completeFailedMessageDeletion(ctx, moderationCase.ID, session.GuildID, ErrMessageInvalidTarget, "The target message changed identity before confirmation; nothing was deleted.", responder)
	}
	deleteErr := router.messageDeletion.DeleteMessage(ctx, MessageDeleteExecution{CaseID: moderationCase.ID, ModeratorID: session.UserID, Target: target, Reason: session.Reason})
	status, errorCode := "succeeded", ""
	if deleteErr != nil {
		status, errorCode = "failed", messageDeletionErrorCode(deleteErr)
	}
	if err := router.repository.CompleteModerationCase(ctx, moderationCase.ID, session.GuildID, status, "not-attempted", errorCode, router.now().UTC()); err != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage(fmt.Sprintf("Discord may have processed the deletion, but case %d could not be finalized. Check the moderation log before retrying.", moderationCase.ID))).ClearComponents())
	}
	if deleteErr != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage(fmt.Sprintf("Case %d recorded a failed deletion: %s", moderationCase.ID, messageLookupError(deleteErr)))).ClearComponents())
	}
	if err := router.repository.DeleteMessageBindingByTarget(ctx, session.GuildID, target.ChannelID, target.MessageID); err != nil {
		return responder.UpdateMessage(messageUpdate(infoMessage("Message deleted", fmt.Sprintf("Case %d succeeded. The message binding cleanup will be retried by the owning service.", moderationCase.ID))).ClearComponents())
	}
	return responder.UpdateMessage(messageUpdate(infoMessage("Message deleted", fmt.Sprintf("Case %d removed message `%d` from <#%d>.", moderationCase.ID, target.MessageID, target.ChannelID))).ClearComponents())
}

func (router *Router) completeFailedMessageDeletion(ctx context.Context, caseID int64, guildID uint64, actionErr error, detail string, responder InteractionResponder) error {
	if err := router.repository.CompleteModerationCase(ctx, caseID, guildID, "failed", "not-attempted", messageDeletionErrorCode(actionErr), router.now().UTC()); err != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage(fmt.Sprintf("Nothing was deleted, but case %d could not be finalized. Check the moderation log before retrying.", caseID))).ClearComponents())
	}
	return responder.UpdateMessage(messageUpdate(errorMessage(fmt.Sprintf("Case %d recorded a failed deletion: %s", caseID, detail))).ClearComponents())
}

func (router *Router) managedMessage(ctx context.Context, target MessageTarget) bool {
	for _, purpose := range []string{"dashboard", "flight-leaders"} {
		binding, found, err := router.repository.MessageBinding(ctx, target.GuildID, purpose)
		if err == nil && found && binding.ChannelID == target.ChannelID && binding.MessageID == target.MessageID {
			return true
		}
	}
	return false
}

func messageLookupError(err error) string {
	switch {
	case errors.Is(err, ErrMessageNotFound):
		return "That message could not be found. Choose its channel or paste the full Discord message link."
	case errors.Is(err, ErrMessageAccess), errors.Is(err, ErrModerationPermission):
		return "The admin or SkyFeed bot lacks Manage Messages access in the target channel."
	case errors.Is(err, ErrMessageSearchTooLarge):
		return "The server has too many searchable channels for an ID-only lookup. Paste the message link or choose its channel."
	case errors.Is(err, ErrMessageInvalidTarget):
		return "Enter a valid Discord message link or numeric message ID from this server."
	case errors.Is(err, ErrMessageRateLimited):
		return "Discord is rate-limiting message operations. Wait a moment, then try again."
	case errors.Is(err, ErrMessageTemporary):
		return "Discord's message service is temporarily unavailable. Try again shortly."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "Discord did not finish the message lookup in time. Try again with the message link."
	default:
		return "Discord could not complete the message operation. Try again shortly."
	}
}

func messageDeletionErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrMessageNotFound):
		return "message-missing"
	case errors.Is(err, ErrMessageAccess), errors.Is(err, ErrModerationPermission):
		return "manage-messages-missing"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	case errors.Is(err, ErrMessageRateLimited):
		return "discord-rate-limited"
	case errors.Is(err, ErrMessageTemporary):
		return "discord-temporary-failure"
	case errors.Is(err, ErrMessageInvalidTarget):
		return "message-identity-changed"
	default:
		return "discord-delete-failed"
	}
}
