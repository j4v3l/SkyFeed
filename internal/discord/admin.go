package discord

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func (router *Router) handleWatch(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Persistent watch storage is unavailable."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Watch storage is temporarily unavailable."))
	}
	switch request.Subcommand {
	case "add":
		kind := domain.RuleType(request.Strings["kind"])
		if !validRuleType(kind) {
			return responder.CreateMessage(errorMessage("That watch rule type is not supported."))
		}
		serverScope := request.Bools["server"]
		if serverScope && (!request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "operator")) {
			return responder.CreateMessage(errorMessage("An Operator or Admin role plus Manage Server permission is required for server watch rules."))
		}
		value := strings.ToUpper(strings.TrimSpace(request.Strings["value"]))
		if value == "" || len(value) > 64 {
			return responder.CreateMessage(errorMessage("Watch values must contain 1–64 characters."))
		}
		bestEffort := kind == domain.RuleOperator || kind == domain.RuleOwner || kind == domain.RuleAircraftType
		minimum := 2
		if bestEffort {
			minimum = 1
		}
		rule, err := router.repository.CreateWatchRule(ctx, domain.WatchRule{GuildID: request.GuildID, UserID: request.UserID, ServerScope: serverScope, FeederScope: requestFeederID(request), Type: kind, Value: value, Enabled: true, Cooldown: 15 * time.Minute, MinimumObservations: minimum, BestEffortEnrichment: bestEffort})
		if err != nil {
			return responder.CreateMessage(errorMessage("The watch rule could not be saved."))
		}
		router.requestRuleReload()
		return responder.CreateMessage(infoMessage("Watch rule saved", fmt.Sprintf("Rule %d watches %s %s.", rule.ID, rule.Type, rule.Value)))
	case "list":
		rules, err := router.repository.WatchRules(ctx, request.GuildID, request.UserID, 100)
		if err != nil {
			return responder.CreateMessage(errorMessage("Watch rules could not be loaded."))
		}
		embed := disgocord.NewEmbed().WithTitle("SkyFeed • Watch rules").WithColor(render.Scope)
		if len(rules) == 0 {
			embed.Description = "No personal or server watch rules are configured."
		}
		selected := requestFeederID(request)
		for _, rule := range rules {
			if selected != domain.FeederAll && rule.FeederScope != selected {
				continue
			}
			state := "disabled"
			if rule.Enabled {
				state = "enabled"
			}
			scope := "personal"
			if rule.ServerScope {
				scope = "server"
			}
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: fmt.Sprintf("#%d • %s", rule.ID, rule.Type), Value: fmt.Sprintf("%s • %s • %s • feeder: %s", rule.Value, scope, state, rule.FeederScope)})
		}
		return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), true))
	case "remove", "enable", "disable":
		return router.changeWatch(ctx, request, responder)
	default:
		return responder.CreateMessage(errorMessage("Choose a watch subcommand."))
	}
}

func (router *Router) changeWatch(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	id, err := strconv.ParseInt(request.Strings["rule"], 10, 64)
	if err != nil || id <= 0 {
		return responder.CreateMessage(errorMessage("Select a valid saved watch rule."))
	}
	rules, err := router.repository.WatchRules(ctx, request.GuildID, request.UserID, 500)
	if err != nil {
		return responder.CreateMessage(errorMessage("Watch rules could not be loaded."))
	}
	for _, rule := range rules {
		if rule.ID != id {
			continue
		}
		if rule.ServerScope && (!request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "operator")) {
			return responder.CreateMessage(errorMessage("An Operator or Admin role plus Manage Server permission is required for that rule."))
		}
		switch request.Subcommand {
		case "remove":
			err = router.repository.DeleteWatchRule(ctx, id, request.GuildID)
		case "enable":
			rule.Enabled = true
			err = router.repository.UpdateWatchRule(ctx, rule)
		case "disable":
			rule.Enabled = false
			err = router.repository.UpdateWatchRule(ctx, rule)
		}
		if err != nil {
			return responder.CreateMessage(errorMessage("The watch rule could not be changed."))
		}
		router.requestRuleReload()
		return responder.CreateMessage(infoMessage("Watch rule updated", fmt.Sprintf("Rule %d was %s.", id, request.Subcommand)))
	}
	return responder.CreateMessage(errorMessage("That rule does not exist or is not visible to you."))
}

func (router *Router) handleAlerts(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Persistent alert configuration is unavailable."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Alert configuration is temporarily unavailable."))
	}
	if request.Subcommand == "configure" {
		if !request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "operator") {
			return responder.CreateMessage(errorMessage("An Operator or Admin role plus Manage Server permission is required to configure alerts."))
		}
		cooldown := time.Duration(request.Ints["cooldown-minutes"]) * time.Minute
		if cooldown == 0 {
			cooldown = 15 * time.Minute
		}
		value := storage.AlertConfig{GuildID: request.GuildID, Category: request.Strings["category"], Enabled: request.Bools["enabled"], Cooldown: cooldown, Destination: request.IDs["destination"]}
		if err := router.repository.UpsertAlertConfig(ctx, value); err != nil {
			return responder.CreateMessage(errorMessage("Alert configuration could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Alerts configured", fmt.Sprintf("%s alerts enabled: %t.", value.Category, value.Enabled)))
	}
	values, err := router.repository.AlertConfigs(ctx, request.GuildID)
	if err != nil {
		return responder.CreateMessage(errorMessage("Alert configuration could not be loaded."))
	}
	embed := disgocord.NewEmbed().WithTitle("SkyFeed • Alerts").WithColor(render.Scope)
	if len(values) == 0 {
		embed.Description = "No alert overrides are configured; safe defaults apply."
	}
	for _, value := range values {
		embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: value.Category, Value: fmt.Sprintf("enabled: %t • cooldown: %s • destination: %d", value.Enabled, value.Cooldown, value.Destination)})
	}
	return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), true))
}

func (router *Router) handleReports(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Report storage is unavailable."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Report storage is temporarily unavailable."))
	}
	switch request.Subcommand {
	case "generate":
		period, err := time.ParseDuration(request.Strings["period"])
		if err != nil || period < time.Hour || period > 7*24*time.Hour {
			return responder.CreateMessage(errorMessage("Choose a valid bounded report period."))
		}
		to := router.now().UTC()
		feederScope := requestFeederID(request)
		if feederScope != domain.FeederAll && !feederScope.Valid() {
			return responder.CreateMessage(errorMessage("Choose a valid approved feeder."))
		}
		summary, err := router.repository.ReportSummaryForScope(ctx, request.GuildID, feederScope, to.Add(-period), to)
		if err != nil {
			return responder.CreateMessage(errorMessage("The report could not be generated."))
		}
		return responder.CreateMessage(render.SafeMessage(render.ReportWithUnits(summary, router.effectiveUnits(request.GuildID, request.UserID)), false))
	case "schedule":
		if !request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "operator") {
			return responder.CreateMessage(errorMessage("An Operator or Admin role plus Manage Server permission is required to schedule reports."))
		}
		value, err := router.repository.UpsertReportSchedule(ctx, storage.ReportSchedule{GuildID: request.GuildID, Cadence: request.Strings["cadence"], Destination: request.IDs["destination"], Enabled: true, LastRun: router.now().UTC()})
		if err != nil {
			return responder.CreateMessage(errorMessage("The report schedule could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Report scheduled", fmt.Sprintf("Schedule %d runs %s.", value.ID, value.Cadence)))
	case "list":
		values, err := router.repository.ReportSchedules(ctx, request.GuildID)
		if err != nil {
			return responder.CreateMessage(errorMessage("Report schedules could not be loaded."))
		}
		embed := disgocord.NewEmbed().WithTitle("SkyFeed • Report schedules").WithColor(render.Scope)
		if len(values) == 0 {
			embed.Description = "No scheduled reports are configured."
		}
		for _, value := range values {
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: fmt.Sprintf("#%d • %s", value.ID, value.Cadence), Value: fmt.Sprintf("channel: %d • enabled: %t", value.Destination, value.Enabled)})
		}
		return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), true))
	default:
		return responder.CreateMessage(errorMessage("Choose a reports subcommand."))
	}
}

func (router *Router) handleSettings(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Persistent server settings are unavailable."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Server settings are temporarily unavailable."))
	}
	if !request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "admin") {
		return responder.CreateMessage(errorMessage("A configured Admin role plus Manage Server permission is required. A Discord Administrator can bootstrap the first binding."))
	}
	if request.Group == "roles" {
		if (request.Subcommand == "bind" || request.Subcommand == "remove") && !request.Administrator && !request.Permissions.Has(disgocord.PermissionManageRoles) {
			return responder.CreateMessage(errorMessage("Manage Roles permission is additionally required to change SkyFeed role bindings."))
		}
		return router.handleRoleSettings(ctx, request, responder)
	}
	if request.Subcommand == "channels" {
		purpose, channel := request.Strings["purpose"], request.IDs["channel"]
		if channel == 0 {
			return responder.CreateMessage(errorMessage("Select a valid destination channel."))
		}
		if err := router.repository.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: request.GuildID, Purpose: purpose, ChannelID: channel}); err != nil {
			return responder.CreateMessage(errorMessage("The channel binding could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Channel configured", fmt.Sprintf("%s now uses channel %d.", purpose, channel)))
	}
	if request.Subcommand == "test" {
		if router.testSend == nil {
			return responder.CreateMessage(errorMessage("Outbound delivery is not ready."))
		}
		if err := router.testSend(ctx, request.GuildID, request.Strings["purpose"]); err != nil {
			return responder.CreateMessage(errorMessage("The destination test could not be queued."))
		}
		return responder.CreateMessage(infoMessage("Destination test queued", "The outbound scheduler accepted the test with mentions disabled."))
	}
	return router.handleOpsSettings(ctx, request, responder)
}

func (router *Router) handleOpsSettings(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	settings, err := router.repository.GuildSettings(ctx, request.GuildID)
	if err != nil {
		return responder.CreateMessage(errorMessage("Server settings are temporarily unavailable."))
	}
	switch request.Subcommand {
	case "units":
		units, ok := domain.ParseUnitSystem(request.Strings["system"])
		if !ok {
			return responder.CreateMessage(errorMessage("Choose aviation or metric units."))
		}
		settings.Units = string(units)
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("The server unit default could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Server units updated", fmt.Sprintf("SkyFeed now defaults to %s units. Personal preferences still take priority.", units)))
	case "pause-alerts":
		settings.AlertsPaused = true
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("Alert pause could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Alerts paused", "Non-emergency alert delivery is paused until `/settings resume-alerts`."))
	case "resume-alerts":
		settings.AlertsPaused = false
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("Alert resume could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Alerts resumed", "Alert delivery is active again."))
	case "mute-squawk":
		code := strings.TrimSpace(request.Strings["code"])
		if !squawkPattern.MatchString(code) {
			return responder.CreateMessage(errorMessage("Squawk codes must be exactly four octal digits (0–7)."))
		}
		settings.MutedSquawks = joinMutedSquawk(settings.MutedSquawks, code)
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("Muted squawk could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Squawk muted", fmt.Sprintf("Alerts for squawk `%s` are muted.", code)))
	case "unmute-squawk":
		code := strings.TrimSpace(request.Strings["code"])
		if !squawkPattern.MatchString(code) {
			return responder.CreateMessage(errorMessage("Squawk codes must be exactly four octal digits (0–7)."))
		}
		settings.MutedSquawks = removeMutedSquawk(settings.MutedSquawks, code)
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("Muted squawk could not be updated."))
		}
		return responder.CreateMessage(infoMessage("Squawk unmuted", fmt.Sprintf("Alerts for squawk `%s` are active again.", code)))
	case "recreate-dashboard":
		if router.dashboardReset == nil {
			return responder.CreateMessage(errorMessage("Dashboard recreation is not ready."))
		}
		if err := router.dashboardReset(ctx); err != nil {
			return responder.CreateMessage(errorMessage("The live dashboard could not be recreated."))
		}
		return responder.CreateMessage(infoMessage("Dashboard reset", "The stored dashboard binding was cleared. SkyFeed will post a fresh live message on the next refresh."))
	default:
		return responder.CreateMessage(errorMessage("Choose a settings subcommand."))
	}
}

func joinMutedSquawk(existing, code string) string {
	parts := splitMutedSquawks(existing)
	for _, part := range parts {
		if part == code {
			return strings.Join(parts, ",")
		}
	}
	parts = append(parts, code)
	return strings.Join(parts, ",")
}

func removeMutedSquawk(existing, code string) string {
	parts := splitMutedSquawks(existing)
	next := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != code {
			next = append(next, part)
		}
	}
	return strings.Join(next, ",")
}

func splitMutedSquawks(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if !squawkPattern.MatchString(code) {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func mutedSquawkSet(raw string) map[string]struct{} {
	parts := splitMutedSquawks(raw)
	result := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		result[part] = struct{}{}
	}
	return result
}

func (router *Router) handleRoleSettings(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	tier := request.Strings["tier"]
	switch request.Subcommand {
	case "bind":
		if tierRank(tier) == 0 {
			return responder.CreateMessage(errorMessage("Choose Operator, Moderator, or Admin."))
		}
		roleID := request.IDs["role"]
		if roleID == 0 || roleID == request.GuildID {
			return responder.CreateMessage(errorMessage("Select an existing non-everyone Discord role."))
		}
		if err := router.repository.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: request.GuildID, Tier: tier, RoleID: roleID}); err != nil {
			return responder.CreateMessage(errorMessage("The role binding could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Role bound", fmt.Sprintf("The existing role `%d` now grants the %s tier. SkyFeed did not create or modify the role.", roleID, tier)))
	case "remove":
		if tierRank(tier) == 0 {
			return responder.CreateMessage(errorMessage("Choose Operator, Moderator, or Admin."))
		}
		if err := router.repository.DeleteRoleBinding(ctx, request.GuildID, tier); err != nil {
			return responder.CreateMessage(errorMessage("That role binding does not exist or could not be removed."))
		}
		return responder.CreateMessage(infoMessage("Role binding removed", fmt.Sprintf("The %s tier no longer has a bound role.", tier)))
	case "list":
		bindings, err := router.repository.RoleBindings(ctx, request.GuildID)
		if err != nil {
			return responder.CreateMessage(errorMessage("Role bindings could not be loaded."))
		}
		embed := disgocord.NewEmbed().WithTitle("SkyFeed • Role access").WithColor(render.Scope).
			WithDescription("Viewer access is implicit. Privileged actions also require the matching native Discord permission.")
		if len(bindings) == 0 {
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: "No bindings", Value: "A Discord Administrator can bind the first Admin role."})
		}
		for _, binding := range bindings {
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: strings.ToUpper(binding.Tier), Value: fmt.Sprintf("Role `%d`", binding.RoleID)})
		}
		return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), true))
	default:
		return responder.CreateMessage(errorMessage("Choose a roles subcommand."))
	}
}

func (router *Router) autocompleteRules(request AutocompleteRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.Autocomplete(nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	rules, err := router.repository.WatchRules(ctx, request.GuildID, request.UserID, 100)
	if err != nil {
		return responder.Autocomplete(nil)
	}
	query := strings.ToUpper(strings.TrimSpace(request.Query))
	choices := make([]disgocord.AutocompleteChoice, 0, min(25, len(rules)))
	for _, rule := range rules {
		label := fmt.Sprintf("#%d • %s • %s", rule.ID, rule.Type, rule.Value)
		if query != "" && !strings.Contains(strings.ToUpper(label), query) {
			continue
		}
		choices = append(choices, disgocord.AutocompleteChoiceString{Name: render.Truncate(label, 100), Value: strconv.FormatInt(rule.ID, 10)})
		if len(choices) == 25 {
			break
		}
	}
	return responder.Autocomplete(choices)
}

func (router *Router) ensureGuild(ctx context.Context, guildID uint64) error {
	return router.repository.EnsureGuild(ctx, guildID)
}

func validRuleType(value domain.RuleType) bool {
	switch value {
	case domain.RuleICAO, domain.RuleRegistration, domain.RuleCallsign, domain.RuleCallsignPrefix, domain.RuleSquawk, domain.RuleOperator, domain.RuleOwner, domain.RuleAircraftType:
		return true
	default:
		return false
	}
}
