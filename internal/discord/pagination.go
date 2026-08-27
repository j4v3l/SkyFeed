package discord

import (
	"context"
	"errors"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	viewFeedersList          = "feeders-list"
	viewWatchRulesList       = "watch-rules-list"
	viewAlertConfigsList     = "alert-configs-list"
	viewReportSchedulesList  = "report-schedules-list"
	viewModerationHistory    = "moderation-history"
	maximumStoredListRecords = 250
)

func isStoredListView(view string) bool {
	switch view {
	case viewFeedersList, viewWatchRulesList, viewAlertConfigsList, viewReportSchedulesList, viewModerationHistory:
		return true
	default:
		return false
	}
}

func (router *Router) newStoredListSession(request CommandRequest, view string) (Session, error) {
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, view, "", "", "")
	if err != nil {
		return Session{}, errors.New("too many active views; close an older SkyFeed view and try again")
	}
	session.PageSize = render.DefaultPageSize
	session.FeederID = requestFeederID(request)
	session.TargetID = request.IDs["user"]
	if err := router.sessions.Update(session); err != nil {
		router.sessions.Delete(session.ID)
		return Session{}, err
	}
	return session, nil
}

func (router *Router) handleStoredListComponent(request ComponentRequest, responder InteractionResponder, session Session, action string) error {
	if !isStoredListView(session.View) {
		return responder.CreateMessage(errorMessage("This list control is not available."))
	}
	if action == "close" {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(disgocord.NewMessageUpdate().WithContent("SkyFeed view closed.").ClearEmbeds().ClearComponents())
	}
	if action != "previous" && action != "next" && action != "refresh" {
		return responder.CreateMessage(errorMessage("This list control is not available."))
	}
	if err := router.authorizeStoredListComponent(request, session); err != nil {
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(messageUpdate(errorMessage(err.Error())).ClearComponents())
	}
	if action == "previous" {
		session.Page = max(0, session.Page-1)
	} else if action == "next" {
		session.Page++
	}
	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("This list expired while it was being updated."))
	}
	message, err := router.storedListMessage(session)
	if err != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage("This list could not be refreshed. Try again shortly.")))
	}
	return responder.UpdateMessage(messageUpdate(message))
}

func (router *Router) authorizeStoredListComponent(request ComponentRequest, session Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch session.View {
	case viewModerationHistory:
		if !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "moderator") ||
			!hasNativeModerationPermission(request.Permissions, request.Administrator, "history") {
			return errors.New("your moderation permissions changed; run `/moderation history` again")
		}
	case viewAlertConfigsList, viewReportSchedulesList:
		if (!request.Administrator && !request.Permissions.Has(disgocord.PermissionManageGuild)) ||
			!router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "operator") {
			return errors.New("your Operator or Admin access changed; run the command again")
		}
	}
	return nil
}

func (router *Router) storedListMessage(session Session) (disgocord.MessageCreate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := router.now()
	private := session.View != viewFeedersList
	var embed disgocord.Embed
	total := 0
	switch session.View {
	case viewFeedersList:
		configuredLimit := router.feederAdmin.MaxFeeders
		if configuredLimit <= 0 {
			configuredLimit = 100
		}
		limit := min(configuredLimit+1, maximumStoredListRecords)
		feeders, err := router.repository.Feeders(ctx, session.GuildID, limit)
		if err != nil {
			return disgocord.MessageCreate{}, err
		}
		health := router.feederSummaries()
		items := make([]render.FeederListItem, 0, len(feeders))
		for _, feeder := range feeders {
			summary := health[feeder.Descriptor.ID]
			state := "offline"
			if !feeder.Descriptor.Enabled {
				state = "disabled"
			} else if !summary.LastPublished.IsZero() {
				state = string(summary.Health)
			}
			items = append(items, render.FeederListItem{
				Name: feeder.Descriptor.DisplayName, Area: firstNonEmpty(feeder.Descriptor.PublicArea, feeder.Descriptor.AirportICAO, "Area not set"),
				State: state, Aircraft: summary.Aircraft,
			})
		}
		total = len(items)
		embed = render.FeedersPage(items, session.Page, session.PageSize, now)
	case viewWatchRulesList:
		rules, err := router.repository.WatchRules(ctx, session.GuildID, session.UserID, maximumStoredListRecords)
		if err != nil {
			return disgocord.MessageCreate{}, err
		}
		if session.FeederID != "" && session.FeederID != domain.FeederAll {
			filtered := rules[:0]
			for _, rule := range rules {
				if rule.FeederScope == session.FeederID {
					filtered = append(filtered, rule)
				}
			}
			rules = filtered
		}
		total = len(rules)
		embed = render.WatchRulesPage(rules, session.Page, session.PageSize, now)
	case viewAlertConfigsList:
		values, err := router.repository.AlertConfigs(ctx, session.GuildID)
		if err != nil {
			return disgocord.MessageCreate{}, err
		}
		total = len(values)
		embed = render.AlertConfigsPage(values, session.Page, session.PageSize, now)
	case viewReportSchedulesList:
		values, err := router.repository.ReportSchedules(ctx, session.GuildID)
		if err != nil {
			return disgocord.MessageCreate{}, err
		}
		total = len(values)
		embed = render.ReportSchedulesPage(values, session.Page, session.PageSize, now)
	case viewModerationHistory:
		values, err := router.repository.ModerationCases(ctx, session.GuildID, session.TargetID, 100)
		if err != nil {
			return disgocord.MessageCreate{}, err
		}
		total = len(values)
		embed = render.ModerationHistoryPage(values, session.Page, session.PageSize, now)
	default:
		return disgocord.MessageCreate{}, errors.New("unsupported stored list")
	}
	_, _, page, maxPage := render.PageBounds(total, session.Page, session.PageSize)
	if page != session.Page {
		session.Page = page
		_ = router.sessions.Update(session)
	}
	return router.pagedControls(render.SafeMessage(embed, private), session, maxPage), nil
}

func (router *Router) pagedControls(message disgocord.MessageCreate, session Session, maxPage int) disgocord.MessageCreate {
	previousID, _ := CustomID(session.ID, "previous")
	nextID, _ := CustomID(session.ID, "next")
	refreshID, _ := CustomID(session.ID, "refresh")
	closeID, _ := CustomID(session.ID, "close")
	return message.AddActionRow(
		disgocord.NewSecondaryButton("Previous", previousID).WithDisabled(session.Page <= 0),
		disgocord.NewSecondaryButton("Refresh", refreshID),
		disgocord.NewSecondaryButton("Next", nextID).WithDisabled(session.Page >= maxPage),
		disgocord.NewDangerButton("Close", closeID),
	)
}
