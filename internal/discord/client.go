package discord

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

type ReadyFunc func(bool)

type GatewayService struct {
	config            config.Discord
	router            *Router
	logger            *slog.Logger
	onReady           ReadyFunc
	repository        storage.Repository
	outbound          *OutboundScheduler
	client            atomic.Pointer[bot.Client]
	dashboardInterval time.Duration
	reportInterval    time.Duration
	interactionMetric func(time.Duration)
	cooldownMu        sync.Mutex
	lastDelivered     map[string]time.Time
}

func NewGatewayService(cfg config.Discord, router *Router, logger *slog.Logger, onReady ReadyFunc) *GatewayService {
	if onReady == nil {
		onReady = func(bool) {}
	}
	return &GatewayService{config: cfg, router: router, logger: logger, onReady: onReady, outbound: NewOutboundScheduler(64, 128, 256, 32), reportInterval: time.Minute, lastDelivered: make(map[string]time.Time)}
}

func RegisterCommands(ctx context.Context, cfg config.Discord, logger *slog.Logger) (RegistrationStats, error) {
	client, err := disgo.New(cfg.Token.Reveal(), bot.WithLogger(logger.With("component", "discord")))
	if err != nil {
		return RegistrationStats{}, fmt.Errorf("create Discord REST client: %w", err)
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Close(closeContext)
	}()
	if client.ApplicationID != snowflake.ID(cfg.ApplicationID) {
		return RegistrationStats{}, errors.New("discord token application ID does not match configured application ID")
	}
	if cfg.GlobalCommands {
		return SyncGlobalCommands(ctx, client.Rest, client.ApplicationID)
	}
	return SyncGuildCommands(ctx, client.Rest, client.ApplicationID, snowflake.ID(cfg.GuildID))
}

func (service *GatewayService) SetRepository(repository storage.Repository) {
	service.repository = repository
}
func (service *GatewayService) SetDashboardInterval(interval time.Duration) {
	service.dashboardInterval = interval
}

func (service *GatewayService) SetReportInterval(interval time.Duration) {
	service.reportInterval = interval
}

func (service *GatewayService) SetInteractionObserver(observer func(time.Duration)) {
	service.interactionMetric = observer
}

func (service *GatewayService) OutboundStats() QueueStats { return service.outbound.Stats() }

func (service *GatewayService) SubmitAlert(ctx context.Context, alert domain.Alert) error {
	priority := PriorityAlert
	if alert.Priority == domain.AlertEmergency {
		priority = PriorityEmergency
	}
	return service.outbound.Enqueue(ctx, OutboundJob{
		Key: alert.ConditionFingerprint + ":" + alert.AircraftICAO, Priority: priority, Retryable: true,
		Run: func(jobContext context.Context) error { return service.sendAlert(jobContext, alert) },
	})
}

func (service *GatewayService) SubmitDestinationTest(ctx context.Context, guildID uint64, purpose string) error {
	switch purpose {
	case "alerts", "emergencies", "reports", "admin":
	default:
		return errors.New("unsupported destination purpose")
	}
	return service.outbound.Enqueue(ctx, OutboundJob{
		Key: fmt.Sprintf("test:%d:%s", guildID, purpose), Priority: PriorityInteraction,
		Run: func(jobContext context.Context) error {
			return service.sendDestinationTest(jobContext, guildID, purpose)
		},
	})
}

func (service *GatewayService) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	client, err := disgo.New(service.config.Token.Reveal(),
		bot.WithLogger(service.logger.With("component", "discord")),
		bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentsNone)),
		bot.WithEventListenerFunc(service.commandEvent),
		bot.WithEventListenerFunc(service.componentEvent),
		bot.WithEventListenerFunc(service.autocompleteEvent),
		bot.WithEventListenerFunc(service.modalEvent),
		bot.WithEventListenerFunc(func(*events.Ready) { service.onReady(true) }),
		bot.WithEventListenerFunc(func(*events.Resumed) { service.onReady(true) }),
	)
	if err != nil {
		return fmt.Errorf("create Discord client: %w", err)
	}
	service.client.Store(client)
	defer func() {
		service.client.Store(nil)
		service.onReady(false)
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Close(shutdownContext)
	}()
	if configured := snowflake.ID(service.config.ApplicationID); client.ApplicationID != configured {
		return fmt.Errorf("discord token application ID %s does not match configured application ID %s", client.ApplicationID, configured)
	}
	var stats RegistrationStats
	if service.config.GlobalCommands {
		stats, err = SyncGlobalCommands(ctx, client.Rest, client.ApplicationID)
	} else {
		stats, err = SyncGuildCommands(ctx, client.Rest, client.ApplicationID, snowflake.ID(service.config.GuildID))
	}
	if err != nil {
		return err
	}
	scope := "guild"
	if service.config.GlobalCommands {
		scope = "global"
	}
	service.logger.Info("Discord commands synchronized", "component", "discord", "event", "commands_sync", "scope", scope, "created", stats.Created, "updated", stats.Updated, "deleted", stats.Deleted, "kept", stats.Kept, "ignored", stats.Ignored, "schema_version", CommandSchemaVersion)
	if err := client.OpenGateway(ctx); err != nil {
		return fmt.Errorf("open Discord Gateway: %w", err)
	}
	outboundDone := make(chan error, 1)
	go func() { outboundDone <- service.outbound.Run(ctx) }()
	dashboardDone := make(chan struct{})
	go func() {
		service.runDashboard(ctx)
		close(dashboardDone)
	}()
	reportsDone := make(chan struct{})
	go func() {
		service.runReportScheduler(ctx)
		close(reportsDone)
	}()
	moderationDone := make(chan struct{})
	go func() {
		service.runModerationMaintenance(ctx)
		close(moderationDone)
	}()
	<-ctx.Done()
	err = <-outboundDone
	<-dashboardDone
	<-reportsDone
	<-moderationDone
	return err
}

func (service *GatewayService) runModerationMaintenance(ctx context.Context) {
	if service.repository == nil {
		<-ctx.Done()
		return
	}
	deliveryTicker := time.NewTicker(5 * time.Second)
	retentionTicker := time.NewTicker(6 * time.Hour)
	defer deliveryTicker.Stop()
	defer retentionTicker.Stop()
	service.deliverModerationLogs(time.Now().UTC())
	service.purgeModerationCases(time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-deliveryTicker.C:
			service.deliverModerationLogs(now.UTC())
		case now := <-retentionTicker.C:
			service.purgeModerationCases(now.UTC())
		}
	}
}

func (service *GatewayService) deliverModerationLogs(now time.Time) {
	client := service.client.Load()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logs, err := service.repository.PendingModerationLogs(ctx, now, 20)
	if err != nil {
		service.logger.Error("moderation log outbox could not be loaded", "component", "discord", "event", "moderation_outbox_load", "error", err)
		return
	}
	if len(logs) == 0 {
		return
	}
	channels := make(map[uint64]uint64)
	for _, pending := range logs {
		channelID, known := channels[pending.Case.GuildID]
		if !known {
			bindings, bindingErr := service.repository.ChannelBindings(ctx, pending.Case.GuildID)
			if bindingErr != nil {
				service.deferModerationLog(ctx, pending, bindingErr, now.Add(moderationLogBackoff(pending.Attempts)))
				continue
			}
			for _, binding := range bindings {
				if binding.Purpose == "moderation" {
					channelID = binding.ChannelID
					break
				}
			}
			channels[pending.Case.GuildID] = channelID
		}
		if channelID == 0 {
			service.deferModerationLog(ctx, pending, errors.New("moderation log channel is not configured"), now.Add(5*time.Minute))
			continue
		}
		message := render.SafeMessage(render.ModerationCase(pending.Case), false).
			WithNonce(boundedNonce(fmt.Sprintf("skyfeed-moderation-%d", pending.Case.ID))).WithEnforceNonce(true)
		if _, err := client.Rest.CreateMessage(snowflake.ID(channelID), message, rest.WithCtx(ctx)); err != nil {
			service.deferModerationLog(ctx, pending, err, now.Add(moderationLogBackoff(pending.Attempts)))
			continue
		}
		if err := service.repository.MarkModerationLogDelivered(ctx, pending.ID, now); err != nil {
			service.logger.Error("moderation log delivery could not be recorded", "component", "storage", "event", "moderation_outbox_complete", "case_id", pending.Case.ID, "error", err)
		}
	}
}

func (service *GatewayService) deferModerationLog(ctx context.Context, pending storage.ModerationLog, deliveryErr error, next time.Time) {
	if err := service.repository.MarkModerationLogFailed(ctx, pending.ID, deliveryErr.Error(), next); err != nil {
		service.logger.Error("moderation log retry could not be recorded", "component", "storage", "event", "moderation_outbox_retry", "case_id", pending.Case.ID, "error", err)
	}
}

func moderationLogBackoff(attempts int) time.Duration {
	attempts = min(max(attempts, 0), 9)
	return min(5*time.Second*time.Duration(1<<attempts), 30*time.Minute)
}

func (service *GatewayService) purgeModerationCases(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	removed, err := service.repository.PurgeModerationCases(ctx, now.AddDate(-1, 0, 0), 500)
	if err != nil {
		service.logger.Error("moderation retention purge failed", "component", "storage", "event", "moderation_retention_failure", "error", err)
		return
	}
	if removed > 0 {
		service.logger.Info("expired moderation cases purged", "component", "storage", "event", "moderation_retention", "count", removed)
	}
}

func (service *GatewayService) runDashboard(ctx context.Context) {
	if service.dashboardInterval <= 0 || service.repository == nil {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(service.dashboardInterval)
	defer ticker.Stop()
	service.enqueueDashboard()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.enqueueDashboard()
		}
	}
}

func (service *GatewayService) EnqueueDashboard() {
	service.enqueueDashboard()
}

func (service *GatewayService) enqueueDashboard() {
	if err := service.outbound.Enqueue(context.Background(), OutboundJob{Key: "dashboard", Priority: PriorityDashboard, Retryable: true, Run: service.updateDashboard}); err != nil {
		service.logger.Warn("dashboard refresh coalesced or dropped", "component", "discord", "event", "dashboard_enqueue", "error", err)
	}
}

func (service *GatewayService) runReportScheduler(ctx context.Context) {
	if service.reportInterval <= 0 || service.repository == nil {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(service.reportInterval)
	defer ticker.Stop()
	service.enqueueDueReports(time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			service.enqueueDueReports(now.UTC())
		}
	}
}

func (service *GatewayService) enqueueDueReports(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	schedules, err := service.repository.ReportSchedules(ctx, service.config.GuildID)
	cancel()
	if err != nil {
		service.logger.Error("report schedules could not be loaded", "component", "discord", "event", "report_schedule_load_failure", "error", err)
		return
	}
	for _, schedule := range schedules {
		if !schedule.Enabled || !reportDue(schedule, now) {
			continue
		}
		schedule := schedule
		if err := service.outbound.Enqueue(context.Background(), OutboundJob{
			Key:      fmt.Sprintf("report:%d:%d", schedule.ID, reportPeriodStart(schedule.Cadence, now).Unix()),
			Priority: PriorityReport, Retryable: true,
			Run: func(jobContext context.Context) error { return service.sendScheduledReport(jobContext, schedule, now) },
		}); err != nil {
			service.logger.Warn("scheduled report was coalesced or dropped", "component", "discord", "event", "report_enqueue", "schedule_id", schedule.ID, "error", err)
		}
	}
}

func reportDue(schedule storage.ReportSchedule, now time.Time) bool {
	now = now.UTC()
	start := reportPeriodStart(schedule.Cadence, now)
	return !start.IsZero() && schedule.LastRun.Before(start)
}

func reportPeriodStart(cadence string, now time.Time) time.Time {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch cadence {
	case "daily":
		return day
	case "weekly":
		daysSinceMonday := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -daysSinceMonday)
	default:
		return time.Time{}
	}
}

func completedReportPeriod(cadence string, now time.Time) (time.Time, time.Time) {
	to := reportPeriodStart(cadence, now)
	switch cadence {
	case "daily":
		return to.AddDate(0, 0, -1), to
	case "weekly":
		return to.AddDate(0, 0, -7), to
	default:
		return time.Time{}, time.Time{}
	}
}

func (service *GatewayService) sendScheduledReport(ctx context.Context, schedule storage.ReportSchedule, now time.Time) error {
	client := service.client.Load()
	if client == nil {
		return errors.New("discord report delivery is not ready")
	}
	from, to := completedReportPeriod(schedule.Cadence, now)
	if from.IsZero() || to.IsZero() {
		return fmt.Errorf("unsupported report cadence %q", schedule.Cadence)
	}
	summary, err := service.repository.ReportSummary(ctx, schedule.GuildID, from, to)
	if err != nil {
		return err
	}
	message := render.SafeMessage(render.Report(summary), false).
		WithNonce(boundedNonce(fmt.Sprintf("skyfeed-report-%d-%d", schedule.ID, reportPeriodStart(schedule.Cadence, now).Unix()))).
		WithEnforceNonce(true)
	if _, err := client.Rest.CreateMessage(snowflake.ID(schedule.Destination), message, rest.WithCtx(ctx)); err != nil {
		return err
	}
	return service.repository.MarkReportScheduleRun(ctx, schedule.ID, schedule.GuildID, now)
}

func (service *GatewayService) sendDestinationTest(ctx context.Context, guildID uint64, purpose string) error {
	client := service.client.Load()
	if client == nil || service.repository == nil {
		return errors.New("discord test delivery is not ready")
	}
	bindings, err := service.repository.ChannelBindings(ctx, guildID)
	if err != nil {
		return err
	}
	var destination uint64
	for _, binding := range bindings {
		if binding.Purpose == purpose {
			destination = binding.ChannelID
			break
		}
	}
	if destination == 0 {
		return fmt.Errorf("no %s channel is configured", purpose)
	}
	message := render.SafeMessage(render.DestinationTest(purpose), false)
	_, err = client.Rest.CreateMessage(snowflake.ID(destination), message, rest.WithCtx(ctx))
	return err
}

func (service *GatewayService) updateDashboard(ctx context.Context) error {
	client := service.client.Load()
	if client == nil {
		return errors.New("discord dashboard is not ready")
	}
	guildID := service.config.GuildID
	bindings, err := service.repository.ChannelBindings(ctx, guildID)
	if err != nil {
		return err
	}
	var channel uint64
	for _, binding := range bindings {
		if binding.Purpose == "live" {
			channel = binding.ChannelID
			break
		}
	}
	if channel == 0 {
		return nil
	}
	message := render.SafeMessage(render.Status(service.router.snapshots.Current(), time.Since(service.router.startedAt), time.Now(), service.router.enrichment != nil), false)
	binding, found, err := service.repository.MessageBinding(ctx, guildID, "dashboard")
	if err != nil {
		return err
	}
	if found && binding.ChannelID == channel {
		_, err = client.Rest.UpdateMessage(snowflake.ID(channel), snowflake.ID(binding.MessageID), messageUpdate(message), rest.WithCtx(ctx))
		if err == nil {
			return nil
		}
		if !isUnknownDiscordMessage(err) {
			return err
		}
	}
	nonceKey := fmt.Sprintf("skyfeed-dashboard-%d", guildID)
	if found {
		nonceKey = fmt.Sprintf("%s-%d", nonceKey, binding.MessageID)
	}
	message = message.WithNonce(boundedNonce(nonceKey)).WithEnforceNonce(true)
	created, err := client.Rest.CreateMessage(snowflake.ID(channel), message, rest.WithCtx(ctx))
	if err != nil {
		return err
	}
	return service.repository.UpsertMessageBinding(ctx, storage.MessageBinding{GuildID: guildID, Purpose: "dashboard", ChannelID: channel, MessageID: uint64(created.ID)})
}

func isUnknownDiscordMessage(err error) bool {
	var discordError *rest.Error
	return errors.As(err, &discordError) && discordError.Code == rest.JSONErrorCodeUnknownMessage
}

func (service *GatewayService) sendAlert(ctx context.Context, alert domain.Alert) error {
	client := service.client.Load()
	if client == nil || service.repository == nil {
		return errors.New("discord alert delivery is not ready")
	}
	settings, err := service.repository.GuildSettings(ctx, alert.GuildID)
	if err == nil {
		if settings.AlertsPaused && alert.Priority != domain.AlertEmergency {
			return nil
		}
		muted := mutedSquawkSet(settings.MutedSquawks)
		if len(muted) > 0 {
			if _, ok := muted[extractAlertSquawk(alert)]; ok && alert.Priority != domain.AlertEmergency {
				return nil
			}
			// Still allow emergencies unless the mute is specifically for that emergency squawk and operator muted it intentionally.
			if alert.Priority == domain.AlertEmergency {
				if code := extractAlertSquawk(alert); code != "" {
					if _, ok := muted[code]; ok {
						return nil
					}
				}
			}
		}
	}
	if service.router != nil && service.router.routes != nil && alert.RouteSummary == "" {
		callsign := strings.ToUpper(strings.TrimSpace(alert.Callsign))
		if callsign != "" {
			if route, found, routeErr := service.router.routes.CachedRoute(callsign); routeErr == nil && found {
				alert.RouteSummary = routeSummary(route)
			}
		}
	}
	bindings, err := service.repository.ChannelBindings(ctx, alert.GuildID)
	if err != nil {
		return err
	}
	purpose := "alerts"
	category := "watch"
	if alert.Priority == domain.AlertEmergency {
		purpose = "emergencies"
		category = "emergency"
	} else if alert.Type == domain.RuleInteresting {
		purpose = "interesting"
		category = "interesting"
	} else if alert.Type == domain.RuleFeeder {
		category = "feeder"
	}
	var destination uint64
	for _, binding := range bindings {
		if binding.Purpose == purpose {
			destination = binding.ChannelID
			break
		}
	}
	configs, err := service.repository.AlertConfigs(ctx, alert.GuildID)
	if err != nil {
		return err
	}
	cooldown := time.Duration(0)
	for _, configured := range configs {
		if configured.Category != category {
			continue
		}
		if !configured.Enabled {
			return nil
		}
		if configured.Destination != 0 {
			destination = configured.Destination
		}
		cooldown = configured.Cooldown
		break
	}
	if destination == 0 {
		return fmt.Errorf("no %s channel is configured", purpose)
	}
	cooldownKey := category + ":" + alert.ConditionFingerprint + ":" + alert.AircraftICAO
	if alert.Priority != domain.AlertEmergency && cooldown > 0 && service.inCooldown(cooldownKey, alert.ObservedAt, cooldown) {
		return nil
	}
	message := render.SafeMessage(render.Alert(alert), false)
	if alert.Type == domain.RuleInteresting {
		message = render.SafeMessage(render.InterestingAlert(alert), false)
	}
	message = message.WithNonce(boundedNonce(alert.ID)).WithEnforceNonce(true)
	_, err = client.Rest.CreateMessage(snowflake.ID(destination), message, rest.WithCtx(ctx))
	if err == nil && alert.Priority != domain.AlertEmergency && cooldown > 0 {
		service.markDelivered(cooldownKey, alert.ObservedAt)
	}
	return err
}

func extractAlertSquawk(alert domain.Alert) string {
	if alert.Type == domain.RuleSquawk {
		parts := strings.Split(alert.ConditionFingerprint, ":")
		if len(parts) > 0 {
			code := parts[len(parts)-1]
			if squawkPattern.MatchString(code) {
				return code
			}
		}
	}
	if strings.HasPrefix(alert.ConditionFingerprint, "emergency:") {
		parts := strings.Split(alert.ConditionFingerprint, ":")
		if len(parts) >= 2 && squawkPattern.MatchString(parts[1]) {
			return parts[1]
		}
	}
	return ""
}

func boundedNonce(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:12])
}

func (service *GatewayService) inCooldown(key string, now time.Time, cooldown time.Duration) bool {
	service.cooldownMu.Lock()
	defer service.cooldownMu.Unlock()
	last := service.lastDelivered[key]
	return !last.IsZero() && now.Sub(last) < cooldown
}

func (service *GatewayService) markDelivered(key string, now time.Time) {
	service.cooldownMu.Lock()
	service.lastDelivered[key] = now
	if len(service.lastDelivered) > 10_000 {
		cutoff := now.Add(-24 * time.Hour)
		for candidate, delivered := range service.lastDelivered {
			if delivered.Before(cutoff) {
				delete(service.lastDelivered, candidate)
			}
		}
		if len(service.lastDelivered) > 10_000 {
			oldestKey := ""
			oldestAt := now
			for candidate, delivered := range service.lastDelivered {
				if oldestKey == "" || delivered.Before(oldestAt) {
					oldestKey, oldestAt = candidate, delivered
				}
			}
			delete(service.lastDelivered, oldestKey)
		}
	}
	service.cooldownMu.Unlock()
}

func (service *GatewayService) commandEvent(event *events.ApplicationCommandInteractionCreate) {
	started := time.Now()
	data := event.SlashCommandInteractionData()
	request := commandRequest(event.ApplicationCommandInteraction, data)
	responder := eventResponder{create: event.CreateMessage}
	observed := false
	defer func() {
		if !observed {
			service.observeInteraction(started)
		}
	}()
	if deferCommand(request) {
		if err := event.DeferCreateMessage(deferredEphemeral(request)); err != nil {
			service.logInteractionError("command_defer", data.CommandName(), err)
			return
		}
		service.observeInteraction(started)
		observed = true
		updateResponse := func(update disgocord.MessageUpdate, opts ...rest.RequestOpt) error {
			_, err := event.Client().Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), update, opts...)
			return err
		}
		responder.create = func(message disgocord.MessageCreate, opts ...rest.RequestOpt) error {
			return updateResponse(messageUpdate(message), opts...)
		}
		responder.update = updateResponse
	}
	if err := service.router.HandleCommand(request, responder); err != nil {
		service.logInteractionError("command", data.CommandName(), err)
	}
}

func (service *GatewayService) componentEvent(event *events.ComponentInteractionCreate) {
	started := time.Now()
	request := ComponentRequest{CustomID: event.Data.CustomID(), UserID: uint64(event.User().ID), GuildID: guildID(event.GuildID()), ChannelID: channelID(event.Channel())}
	if member := event.Member(); member != nil {
		request.Permissions = member.Permissions
		request.Administrator = member.Permissions.Has(disgocord.PermissionAdministrator)
		request.RoleIDs = make([]uint64, len(member.RoleIDs))
		for index, roleID := range member.RoleIDs {
			request.RoleIDs[index] = uint64(roleID)
		}
	}
	if data, ok := event.Data.(disgocord.StringSelectMenuInteractionData); ok {
		request.Values = append([]string(nil), data.Values...)
	}
	responder := eventResponder{create: event.CreateMessage, update: event.UpdateMessage, modal: event.Modal}
	_, action, parseErr := ParseCustomID(request.CustomID)
	if parseErr == nil && action == "moderate-confirm" {
		if err := event.DeferUpdateMessage(); err != nil {
			service.logInteractionError("component_defer", request.CustomID, err)
			service.observeInteraction(started)
			return
		}
		service.observeInteraction(started)
		updateResponse := func(update disgocord.MessageUpdate, opts ...rest.RequestOpt) error {
			_, err := event.Client().Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), update, opts...)
			return err
		}
		responder.update = updateResponse
		responder.create = func(message disgocord.MessageCreate, opts ...rest.RequestOpt) error {
			return updateResponse(messageUpdate(message), opts...)
		}
	} else {
		defer service.observeInteraction(started)
	}
	if err := service.router.HandleComponent(request, responder); err != nil {
		service.logInteractionError("component", request.CustomID, err)
	}
}

func (service *GatewayService) autocompleteEvent(event *events.AutocompleteInteractionCreate) {
	started := time.Now()
	defer service.observeInteraction(started)
	query := ""
	option := ""
	if focused := event.Data.Focused(); focused.Type == disgocord.ApplicationCommandOptionTypeString {
		query = focused.String()
		option = focused.Name
	}
	responder := eventResponder{autocomplete: event.AutocompleteResult}
	request := AutocompleteRequest{Name: event.Data.CommandName, Option: option, Query: query, UserID: uint64(event.User().ID), GuildID: guildID(event.GuildID())}
	if event.Data.SubCommandName != nil {
		request.Subcommand = *event.Data.SubCommandName
	}
	if err := service.router.HandleAutocomplete(request, responder); err != nil {
		service.logInteractionError("autocomplete", event.Data.CommandName, err)
	}
}

func (service *GatewayService) modalEvent(event *events.ModalSubmitInteractionCreate) {
	started := time.Now()
	if err := event.DeferCreateMessage(true); err != nil {
		service.logInteractionError("modal_defer", event.Data.CustomID, err)
		return
	}
	service.observeInteraction(started)
	request := ModalRequest{
		CustomID: event.Data.CustomID, UserID: uint64(event.User().ID), GuildID: guildID(event.GuildID()), ChannelID: channelID(event.Channel()),
		Values: map[string]string{"label": event.Data.Text("label"), "cooldown": event.Data.Text("cooldown")},
	}
	responder := eventResponder{create: func(message disgocord.MessageCreate, opts ...rest.RequestOpt) error {
		_, err := event.Client().Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate(message), opts...)
		return err
	}, update: event.UpdateMessage}
	if err := service.router.HandleModal(request, responder); err != nil {
		service.logInteractionError("modal", request.CustomID, err)
	}
}

func deferCommand(request CommandRequest) bool {
	switch request.Name {
	case "watch", "alerts", "settings", "reports", "moderation", "route", "airport":
		return true
	default:
		return false
	}
}

func deferredEphemeral(request CommandRequest) bool {
	switch request.Name {
	case "privacy", "watch", "alerts", "settings", "moderation":
		return true
	case "reports":
		return request.Subcommand != "generate"
	default:
		return false
	}
}

func (service *GatewayService) observeInteraction(started time.Time) {
	if service.interactionMetric != nil {
		service.interactionMetric(time.Since(started))
	}
}

func (service *GatewayService) logInteractionError(kind, name string, err error) {
	service.logger.Error("Discord interaction failed", "component", "discord", "event", "interaction_error", "kind", kind, "name", name, "error", err)
}

func commandRequest(interaction disgocord.ApplicationCommandInteraction, data disgocord.SlashCommandInteractionData) CommandRequest {
	request := CommandRequest{
		Name: data.CommandName(), UserID: uint64(interaction.User().ID), GuildID: guildID(interaction.GuildID()), ChannelID: channelID(interaction.Channel()),
		Strings: map[string]string{}, Ints: map[string]int{}, Floats: map[string]float64{}, Bools: map[string]bool{}, IDs: map[string]uint64{},
	}
	if data.SubCommandName != nil {
		request.Subcommand = *data.SubCommandName
	}
	if data.SubCommandGroupName != nil {
		request.Group = *data.SubCommandGroupName
	}
	if member := interaction.Member(); member != nil {
		request.Permissions = member.Permissions
		request.Administrator = member.Permissions.Has(disgocord.PermissionAdministrator)
		request.ManageGuild = member.Permissions.Has(disgocord.PermissionManageGuild) || request.Administrator
		request.RoleIDs = make([]uint64, len(member.RoleIDs))
		for index, roleID := range member.RoleIDs {
			request.RoleIDs[index] = uint64(roleID)
		}
	}
	for _, key := range []string{"query", "sort", "kind", "value", "rule", "category", "period", "cadence", "purpose", "tier", "reason", "duration", "user-id", "flight", "code", "metric", "squawk"} {
		if value, ok := data.OptString(key); ok {
			request.Strings[key] = value
		}
	}
	for _, key := range []string{"altitude-min", "altitude-max", "limit", "cooldown-minutes", "delete-message-days", "case-id"} {
		if value, ok := data.OptInt(key); ok {
			request.Ints[key] = value
		}
	}
	if value, ok := data.OptFloat("radius-nm"); ok {
		request.Floats["radius-nm"] = value
	}
	for _, key := range []string{"server", "enabled"} {
		if value, ok := data.OptBool(key); ok {
			request.Bools[key] = value
		}
	}
	for _, key := range []string{"destination", "channel"} {
		if value, ok := data.OptSnowflake(key); ok {
			request.IDs[key] = uint64(value)
		}
	}
	if value, ok := data.OptUser("user"); ok {
		request.IDs["user"] = uint64(value.ID)
	}
	if value, ok := data.OptRole("role"); ok {
		request.IDs["role"] = uint64(value.ID)
	}
	return request
}

type eventResponder struct {
	create       func(disgocord.MessageCreate, ...rest.RequestOpt) error
	update       func(disgocord.MessageUpdate, ...rest.RequestOpt) error
	modal        func(disgocord.ModalCreate, ...rest.RequestOpt) error
	autocomplete func([]disgocord.AutocompleteChoice, ...rest.RequestOpt) error
}

func (responder eventResponder) CreateMessage(message disgocord.MessageCreate) error {
	if responder.create == nil {
		return errors.New("message response is unavailable for this interaction")
	}
	return responder.create(message)
}

func (responder eventResponder) UpdateMessage(message disgocord.MessageUpdate) error {
	if responder.update == nil {
		return errors.New("message update is unavailable for this interaction")
	}
	return responder.update(message)
}

func (responder eventResponder) ShowModal(modal disgocord.ModalCreate) error {
	if responder.modal == nil {
		return errors.New("modal response is unavailable for this interaction")
	}
	return responder.modal(modal)
}

func (responder eventResponder) Autocomplete(choices []disgocord.AutocompleteChoice) error {
	if responder.autocomplete == nil {
		return errors.New("autocomplete response is unavailable for this interaction")
	}
	return responder.autocomplete(choices)
}

func guildID(id *snowflake.ID) uint64 {
	if id == nil {
		return 0
	}
	return uint64(*id)
}

func channelID(channel disgocord.InteractionChannel) uint64 {
	if channel.MessageChannel == nil {
		return 0
	}
	return uint64(channel.ID())
}
