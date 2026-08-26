package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

type AuditComponent struct {
	Name    string
	Status  string
	Message string
}

type SystemAuditData struct {
	GeneratedAt           time.Time
	Uptime                time.Duration
	OverallStatus         string
	Live                  bool
	Ready                 bool
	Components            []AuditComponent
	AircraftCount         int
	ActiveProvider        string
	SnapshotAge           time.Duration
	MessageRate           float64
	MaxRangeNM            float64
	AlertsPaused          bool
	MutedSquawks          string
	Channels              []string
	Roles                 []string
	WatchRules            int
	AlertConfigs          int
	ReportSchedules       int
	Report24h             storage.ReportSummary
	RouteCatalog          int64
	RouteSightings24h     int64
	InterestingSeen       int
	RecentFeeders         []string
	PendingModerationLogs int
	PlaneAlertRecords     int
	ADSBDBEnabled         bool
	ADSBDBCache           int
	ADSBDBHits            uint64
	ADSBDBMisses          uint64
	ADSBDBFailures        uint64
	ADSBDBCircuitRejects  uint64
	AdsbLolEnabled        bool
	AdsbLolRouteCache     int
	AdsbLolAirportCache   int
	AdsbLolBatches        uint64
	AdsbLolFailures       uint64
	AdsbLolCircuitRejects uint64
}

func SystemAudit(data SystemAuditData) discord.Embed {
	now := data.GeneratedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := strings.ToUpper(strings.TrimSpace(valueOr(data.OverallStatus, "unknown")))
	color := auditColor(data.OverallStatus)
	embed := base("System audit", color, now).WithDescription(
		fmt.Sprintf("%s **%s** — live `%t` · ready `%t` · up %s", auditBadge(data.OverallStatus), status, data.Live, data.Ready, conciseDuration(data.Uptime)),
	)
	embed.Fields = []discord.EmbedField{
		section("Live traffic", fmt.Sprintf("%d aircraft · provider `%s` · age %s\n%.1f msg/s · max %.1f NM",
			data.AircraftCount, PlainText(valueOr(data.ActiveProvider, "unknown")), conciseDuration(data.SnapshotAge), data.MessageRate, data.MaxRangeNM)),
		section("Components", formatAuditComponents(data.Components)),
		section("Guild ops", formatGuildOps(data)),
		section("Bindings", formatBindings(data)),
		section("Last 24h", formatReportWindow(data)),
		section("Enrichment", formatEnrichment(data)),
	}
	embed.Footer = &discord.EmbedFooter{Text: "Admin-only · no coordinates or secrets included"}
	return BoundEmbed(embed)
}

func AdminDigest(data SystemAuditData, interval time.Duration) discord.Embed {
	now := data.GeneratedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := strings.ToUpper(strings.TrimSpace(valueOr(data.OverallStatus, "unknown")))
	embed := base("Admin digest", auditColor(data.OverallStatus), now).WithDescription(
		fmt.Sprintf("%s **%s** — scheduled every %s", auditBadge(data.OverallStatus), status, conciseDuration(interval)),
	)
	embed.Fields = []discord.EmbedField{
		section("Health", fmt.Sprintf("live `%t` · ready `%t` · up %s\n%s", data.Live, data.Ready, conciseDuration(data.Uptime), formatAuditComponents(data.Components))),
		section("Traffic", fmt.Sprintf("%d aircraft · `%s` · %.1f msg/s · max %.1f NM", data.AircraftCount, PlainText(valueOr(data.ActiveProvider, "unknown")), data.MessageRate, data.MaxRangeNM)),
		section("Ops", formatGuildOps(data)),
		section("24h rollup", formatReportWindow(data)),
		section("Enrichment", formatEnrichment(data)),
	}
	embed.Footer = &discord.EmbedFooter{Text: "Posted to Administration channel · run /audit for a full on-demand snapshot"}
	return BoundEmbed(embed)
}

func auditColor(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "healthy":
		return Radar
	case "degraded", "stale", "not_ready":
		return Caution
	case "offline":
		return EmergencyColor
	default:
		return Muted
	}
}

func auditBadge(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "healthy":
		return "🟢"
	case "degraded", "stale", "not_ready":
		return "🟡"
	case "offline":
		return "🔴"
	default:
		return "⚪"
	}
}

func formatAuditComponents(components []AuditComponent) string {
	if len(components) == 0 {
		return "No component health registered yet."
	}
	lines := make([]string, 0, len(components))
	for _, component := range components {
		line := fmt.Sprintf("`%s` %s", PlainText(component.Name), PlainText(component.Status))
		if message := strings.TrimSpace(component.Message); message != "" {
			line = line + " — " + PlainText(message)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatGuildOps(data SystemAuditData) string {
	paused := "active"
	if data.AlertsPaused {
		paused = "paused"
	}
	muted := strings.TrimSpace(data.MutedSquawks)
	if muted == "" {
		muted = "none"
	}
	return fmt.Sprintf("alerts %s · muted squawks `%s`\nwatch %d · alert configs %d · report schedules %d\ninteresting seen %d · pending mod logs %d · plane-alert %d",
		paused, PlainText(muted), data.WatchRules, data.AlertConfigs, data.ReportSchedules, data.InterestingSeen, data.PendingModerationLogs, data.PlaneAlertRecords)
}

func formatBindings(data SystemAuditData) string {
	parts := make([]string, 0, 2)
	if len(data.Channels) == 0 {
		parts = append(parts, "channels: none")
	} else {
		parts = append(parts, "channels:\n"+strings.Join(data.Channels, "\n"))
	}
	if len(data.Roles) == 0 {
		parts = append(parts, "roles: none")
	} else {
		parts = append(parts, "roles:\n"+strings.Join(data.Roles, "\n"))
	}
	if len(data.RecentFeeders) > 0 {
		parts = append(parts, "recent feeder:\n"+strings.Join(data.RecentFeeders, "\n"))
	}
	return strings.Join(parts, "\n")
}

func formatReportWindow(data SystemAuditData) string {
	return fmt.Sprintf("%d aircraft observations · %d peak tracked · %d msgs\nmax %.1f NM · %d emergency events\nroute catalog %d · route sightings %d",
		data.Report24h.AircraftObservations, data.Report24h.PeakTracked, data.Report24h.Messages, data.Report24h.MaximumRangeNM, data.Report24h.EmergencyEvents, data.RouteCatalog, data.RouteSightings24h)
}

func formatEnrichment(data SystemAuditData) string {
	lines := make([]string, 0, 2)
	if data.ADSBDBEnabled {
		lines = append(lines, fmt.Sprintf("ADSBDB cache %d · hits %d · misses %d · fail %d · circuit %d",
			data.ADSBDBCache, data.ADSBDBHits, data.ADSBDBMisses, data.ADSBDBFailures, data.ADSBDBCircuitRejects))
	} else {
		lines = append(lines, "ADSBDB disabled")
	}
	if data.AdsbLolEnabled {
		lines = append(lines, fmt.Sprintf("adsb.lol routes %d · airports %d · batches %d · fail %d · circuit %d",
			data.AdsbLolRouteCache, data.AdsbLolAirportCache, data.AdsbLolBatches, data.AdsbLolFailures, data.AdsbLolCircuitRejects))
	} else {
		lines = append(lines, "adsb.lol disabled")
	}
	return strings.Join(lines, "\n")
}
