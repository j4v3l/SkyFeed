package discord

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/health"
)

type HealthViewer interface {
	View(now time.Time) health.View
}

type EnrichmentAuditor interface {
	Stats() enrichment.ServiceStats
	CacheLen() int
}

type RouteAuditor interface {
	Stats() enrichment.RouteServiceStats
	RouteCacheLen() int
	AirportCacheLen() int
}

func (router *Router) SetHealth(viewer HealthViewer) { router.health = viewer }
func (router *Router) SetEnrichmentAuditor(auditor EnrichmentAuditor) {
	router.enrichmentAudit = auditor
}
func (router *Router) SetRouteAuditor(auditor RouteAuditor) { router.routeAudit = auditor }

func (router *Router) handleAudit(request CommandRequest, responder InteractionResponder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "admin") {
		return responder.CreateMessage(errorMessage("A configured Admin role plus Manage Server permission is required for /audit."))
	}
	audit, err := router.buildSystemAudit(ctx, request.GuildID)
	if err != nil {
		return responder.CreateMessage(errorMessage("The system audit could not be assembled."))
	}
	return responder.CreateMessage(render.SafeMessage(render.SystemAudit(audit), true))
}

func (router *Router) buildSystemAudit(ctx context.Context, guildID uint64) (render.SystemAuditData, error) {
	now := router.now().UTC()
	audit := render.SystemAuditData{
		GeneratedAt: now,
		Uptime:      now.Sub(router.startedAt),
	}
	if router.health != nil {
		view := router.health.View(now)
		audit.OverallStatus = view.Status
		audit.Live = view.Live
		audit.Ready = view.Ready
		if view.Uptime > 0 {
			audit.Uptime = view.Uptime
		}
		names := make([]string, 0, len(view.Components))
		for name := range view.Components {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			component := view.Components[name]
			audit.Components = append(audit.Components, render.AuditComponent{
				Name:    name,
				Status:  component.Status,
				Message: component.Message,
			})
		}
	}
	if snapshot := router.snapshots.Current(); snapshot != nil {
		audit.AircraftCount = len(snapshot.Aircraft)
		if snapshot.ActiveProvider.Known() {
			audit.ActiveProvider = string(snapshot.ActiveProvider)
		}
		if !snapshot.FetchedAt.IsZero() {
			age := now.Sub(snapshot.FetchedAt)
			if age < 0 {
				age = 0
			}
			audit.SnapshotAge = age
		}
		if !snapshot.Health.Stats.LastSuccess.IsZero() {
			audit.MessageRate = snapshot.Statistics.MessageRate
			audit.MaxRangeNM = snapshot.Statistics.MaxRangeNM
		}
		status, _ := overallHealthStatus(snapshot.Health)
		if audit.OverallStatus == "" {
			audit.OverallStatus = string(status)
		}
	}
	if router.repository == nil {
		return audit, nil
	}
	if err := router.ensureGuild(ctx, guildID); err != nil {
		return audit, err
	}
	if settings, err := router.repository.GuildSettings(ctx, guildID); err == nil {
		audit.AlertsPaused = settings.AlertsPaused
		audit.MutedSquawks = settings.MutedSquawks
	}
	if bindings, err := router.repository.ChannelBindings(ctx, guildID); err == nil {
		for _, binding := range bindings {
			audit.Channels = append(audit.Channels, fmt.Sprintf("%s → `%d`", binding.Purpose, binding.ChannelID))
		}
	}
	if roles, err := router.repository.RoleBindings(ctx, guildID); err == nil {
		for _, role := range roles {
			audit.Roles = append(audit.Roles, fmt.Sprintf("%s → `%d`", role.Tier, role.RoleID))
		}
	}
	if rules, err := router.repository.AllWatchRules(ctx, guildID, 500); err == nil {
		audit.WatchRules = len(rules)
	}
	if configs, err := router.repository.AlertConfigs(ctx, guildID); err == nil {
		audit.AlertConfigs = len(configs)
	}
	if schedules, err := router.repository.ReportSchedules(ctx, guildID); err == nil {
		audit.ReportSchedules = len(schedules)
	}
	if summary, err := router.repository.ReportSummary(ctx, guildID, now.Add(-24*time.Hour), now); err == nil {
		audit.Report24h = summary
	}
	if counts, err := router.repository.RouteTrafficCounts(ctx, guildID, now.Add(-24*time.Hour)); err == nil {
		audit.RouteCatalog = counts.CatalogEntries
		audit.RouteSightings24h = counts.Sightings
	}
	if seen, err := router.repository.InterestingSeenICAOs(ctx, guildID); err == nil {
		audit.InterestingSeen = len(seen)
	}
	if events, err := router.repository.RecentFeederEvents(ctx, guildID, 3); err == nil {
		for _, event := range events {
			audit.RecentFeeders = append(audit.RecentFeeders, fmt.Sprintf("%s · %s", event.Kind, event.Status))
		}
	}
	if logs, err := router.repository.PendingModerationLogs(ctx, now, 50); err == nil {
		audit.PendingModerationLogs = len(logs)
	}
	if count, err := router.repository.PlaneAlertReferenceCount(ctx); err == nil {
		audit.PlaneAlertRecords = count
	}
	if auditor, ok := router.messageDeletion.(MessageDeletionAuditor); ok {
		if access, err := auditor.AuditMessageDeletionAccess(ctx, guildID); err == nil {
			audit.MessageDeleteChannels = access.Channels
			audit.MessageDeleteGaps = access.Gaps
		}
	}
	if router.enrichmentAudit != nil {
		stats := router.enrichmentAudit.Stats()
		audit.ADSBDBEnabled = true
		audit.ADSBDBCache = router.enrichmentAudit.CacheLen()
		audit.ADSBDBHits = stats.Hits
		audit.ADSBDBMisses = stats.Misses
		audit.ADSBDBFailures = stats.Failures
		audit.ADSBDBCircuitRejects = stats.CircuitRejects
	}
	if router.routeAudit != nil {
		stats := router.routeAudit.Stats()
		audit.AdsbLolEnabled = true
		audit.AdsbLolRouteCache = router.routeAudit.RouteCacheLen()
		audit.AdsbLolAirportCache = router.routeAudit.AirportCacheLen()
		audit.AdsbLolBatches = stats.Batches
		audit.AdsbLolFailures = stats.Failures
		audit.AdsbLolCircuitRejects = stats.CircuitRejects
	}
	return audit, nil
}

func overallHealthStatus(healthValue domain.Health) (domain.HealthStatus, int) {
	return renderOverallHealth(healthValue)
}

// thin wrappers keep audit assembly free of render package private helpers.
func renderOverallHealth(healthValue domain.Health) (domain.HealthStatus, int) {
	statuses := []domain.HealthStatus{healthValue.Aircraft.Status, healthValue.Receiver.Status, healthValue.Stats.Status}
	for _, status := range statuses {
		if status == domain.HealthOffline {
			return status, render.EmergencyColor
		}
	}
	for _, status := range statuses {
		if status == domain.HealthDegraded || status == domain.HealthStale {
			return status, render.Caution
		}
	}
	for _, status := range statuses {
		if status == domain.HealthUnknown {
			return status, render.Muted
		}
	}
	return domain.HealthHealthy, render.Radar
}
