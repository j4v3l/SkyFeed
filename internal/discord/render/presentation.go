package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const (
	Radar     = 0x35D07F
	Scope     = 0x37B5FF
	Caution   = 0xF3B63A
	Emergency = 0xF05252
	Muted     = 0x6B7280

	footer = "Live readsb data • ADSBDB enrichment when shown"
)

func SafeMessage(embed discord.Embed, ephemeral bool) discord.MessageCreate {
	mentions := discord.AllowedMentions{}
	return discord.NewMessageCreate().WithEmbeds(BoundEmbed(embed)).WithEphemeral(ephemeral).WithAllowedMentions(&mentions)
}

func Status(snapshot *domain.Snapshot, uptime time.Duration, now time.Time, enrichmentEnabled bool) discord.Embed {
	if snapshot == nil {
		return base("Status", Muted, now).WithDescription("⚪ **UNKNOWN** — waiting for the first receiver payload.")
	}
	status, color := overallHealth(snapshot.Health)
	description := fmt.Sprintf("%s **%s** — waiting for the first aircraft payload.", badge(status), strings.ToUpper(string(status)))
	if !snapshot.FetchedAt.IsZero() {
		age := now.Sub(snapshot.FetchedAt)
		if age < 0 {
			age = 0
		}
		description = fmt.Sprintf("%s **%s** — live state refreshed %s ago.", badge(status), strings.ToUpper(string(status)), conciseDuration(age))
	}
	embed := base("Status", color, snapshot.PublishedAt).WithDescription(description)
	enrichmentStatus := "Disabled"
	if enrichmentEnabled {
		enrichmentStatus = "Enabled • presentation-only cache"
	}
	tracked, messageRate, maximumRange := "Unavailable", "Unavailable", "Unavailable"
	if !snapshot.Health.Aircraft.LastSuccess.IsZero() {
		tracked = fmt.Sprintf("%d aircraft", len(snapshot.Aircraft))
	}
	if !snapshot.Health.Stats.LastSuccess.IsZero() {
		messageRate = fmt.Sprintf("%.1f msg/s", snapshot.Statistics.MessageRate)
		maximumRange = fmt.Sprintf("%.1f NM", snapshot.Statistics.MaxRangeNM)
	}
	embed.Fields = []discord.EmbedField{
		{Name: "Tracked", Value: tracked, Inline: ptr(true)},
		{Name: "Recent message rate", Value: messageRate, Inline: ptr(true)},
		{Name: "Recent maximum range", Value: maximumRange, Inline: ptr(true)},
		{Name: "Aircraft source", Value: sourceLabel(snapshot.Health.Aircraft), Inline: ptr(true)},
		{Name: "Receiver source", Value: sourceLabel(snapshot.Health.Receiver), Inline: ptr(true)},
		{Name: "Statistics source", Value: sourceLabel(snapshot.Health.Stats), Inline: ptr(true)},
		{Name: "Bot uptime", Value: conciseDuration(uptime), Inline: ptr(true)},
		{Name: "Enrichment", Value: enrichmentStatus, Inline: ptr(true)},
	}
	return BoundEmbed(embed)
}

func Feeder(snapshot *domain.Snapshot, now time.Time) discord.Embed {
	if snapshot == nil {
		return base("Feeder", Muted, now).WithDescription("⚪ **UNKNOWN** — waiting for receiver diagnostics.")
	}
	status, color := overallHealth(snapshot.Health)
	embed := base("Feeder", color, snapshot.PublishedAt).
		WithDescription(fmt.Sprintf("%s **%s** — independent readsb source diagnostics.", badge(status), strings.ToUpper(string(status))))
	positionState := "Unavailable"
	receiverAvailable := !snapshot.Health.Receiver.LastSuccess.IsZero()
	statsAvailable := !snapshot.Health.Stats.LastSuccess.IsZero()
	if receiverAvailable && snapshot.Receiver.HasPosition {
		positionState = "Configured"
	}
	refresh := "Unavailable"
	if receiverAvailable && snapshot.Receiver.Refresh > 0 {
		refresh = conciseDuration(snapshot.Receiver.Refresh)
	}
	window := "Unavailable"
	if statsAvailable && !snapshot.Statistics.WindowStart.IsZero() && !snapshot.Statistics.WindowEnd.IsZero() {
		window = fmt.Sprintf("<t:%d:T>–<t:%d:T>", snapshot.Statistics.WindowStart.Unix(), snapshot.Statistics.WindowEnd.Unix())
	}
	receiverVersion, messages, tracks, maximumRange := "Version unavailable", "Unavailable", "Unavailable", "Unavailable"
	if receiverAvailable {
		receiverVersion = valueOr(snapshot.Receiver.Version, "Version unavailable")
	}
	if statsAvailable {
		messages = fmt.Sprintf("%d", snapshot.Statistics.Messages)
		tracks = fmt.Sprintf("%d", snapshot.Statistics.TrackedAircraft)
		maximumRange = fmt.Sprintf("%.1f NM", snapshot.Statistics.MaxRangeNM)
	}
	embed.Fields = []discord.EmbedField{
		{Name: "Receiver", Value: receiverVersion, Inline: ptr(true)},
		{Name: "Refresh", Value: refresh, Inline: ptr(true)},
		{Name: "Receiver position", Value: positionState, Inline: ptr(true)},
		{Name: "Statistics window", Value: window, Inline: ptr(true)},
		{Name: "Messages in window", Value: messages, Inline: ptr(true)},
		{Name: "Tracks reported", Value: tracks, Inline: ptr(true)},
		{Name: "Max range in window", Value: maximumRange, Inline: ptr(true)},
		{Name: "Aircraft JSON", Value: sourceLabel(snapshot.Health.Aircraft), Inline: ptr(true)},
		{Name: "Receiver JSON", Value: sourceLabel(snapshot.Health.Receiver), Inline: ptr(true)},
		{Name: "Stats JSON", Value: sourceLabel(snapshot.Health.Stats), Inline: ptr(true)},
	}
	return BoundEmbed(embed)
}

func ModerationCase(value storage.ModerationCase) discord.Embed {
	color := Radar
	if value.Status == "failed" {
		color = Emergency
	} else if value.Status == "pending" {
		color = Caution
	}
	embed := discord.NewEmbed().WithTitle(fmt.Sprintf("SkyFeed • Moderation case %d", value.ID)).WithColor(color).
		WithDescription(fmt.Sprintf("**%s** • %s", strings.ToUpper(value.Action), strings.ToUpper(value.Status)))
	embed.Fields = []discord.EmbedField{
		{Name: "Target user", Value: fmt.Sprintf("`%d`", value.TargetUserID), Inline: ptr(true)},
		{Name: "Moderator", Value: fmt.Sprintf("`%d`", value.ModeratorID), Inline: ptr(true)},
		{Name: "Created", Value: fmt.Sprintf("<t:%d:f>", value.CreatedAt.Unix()), Inline: ptr(true)},
		{Name: "Reason", Value: Truncate(PlainText(value.Reason), 400)},
		{Name: "DM delivery", Value: value.DMStatus, Inline: ptr(true)},
	}
	if value.Duration > 0 {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Duration", Value: conciseDuration(value.Duration), Inline: ptr(true)})
	}
	if value.DeleteMessageDuration > 0 {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Message deletion", Value: conciseDuration(value.DeleteMessageDuration), Inline: ptr(true)})
	}
	if value.ErrorCode != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Failure class", Value: value.ErrorCode, Inline: ptr(true)})
	}
	if !value.CompletedAt.IsZero() {
		embed.Timestamp = &value.CompletedAt
	}
	return BoundEmbed(embed)
}

func Aircraft(aircraft domain.Aircraft, snapshot *domain.Snapshot, now time.Time) discord.Embed {
	return AircraftWithEnrichment(aircraft, snapshot, nil, now)
}

func AircraftWithEnrichment(aircraft domain.Aircraft, snapshot *domain.Snapshot, enrichment *domain.Enrichment, now time.Time) discord.Embed {
	color := Scope
	alert := "None"
	if aircraft.Emergency != "" && aircraft.Emergency != "none" {
		color = Emergency
		alert = "🔴 " + strings.ToUpper(aircraft.Emergency)
	}
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Aircraft • "+identity, color, now)
	embed.Description = fmt.Sprintf("`%s` • %s • %s", aircraft.ICAO, valueOr(aircraft.Registration, "registration unknown"), valueOr(aircraft.AircraftType, "type unknown"))
	embed.Fields = []discord.EmbedField{
		{Name: "Position", Value: position(aircraft), Inline: ptr(true)},
		{Name: "Altitude", Value: altitude(aircraft), Inline: ptr(true)},
		{Name: "Ground speed", Value: groundSpeed(aircraft), Inline: ptr(true)},
		{Name: "Track", Value: track(aircraft), Inline: ptr(true)},
		{Name: "Vertical rate", Value: verticalRate(aircraft), Inline: ptr(true)},
		{Name: "Squawk", Value: valueOr(aircraft.Squawk, "Unknown"), Inline: ptr(true)},
		{Name: "Alert state", Value: alert, Inline: ptr(false)},
	}
	if snapshot != nil {
		embed.Footer = &discord.EmbedFooter{Text: fmt.Sprintf("Live readsb data • observation age %s", conciseDuration(aircraft.Seen))}
	}
	if enrichment != nil && enrichment.Found {
		if metadata := enrichment.Aircraft; metadata != nil {
			embed.Fields = append(embed.Fields,
				discord.EmbedField{Name: "Aircraft metadata", Value: strings.Join(nonEmpty(metadata.Manufacturer, metadata.AircraftType, metadata.Registration), " • ")},
				discord.EmbedField{Name: "Owner / operator", Value: strings.Join(nonEmpty(metadata.Owner, metadata.OwnerCountry), " • ")},
			)
		}
		if route := enrichment.Route; route != nil {
			routeText := airportLabel(route.Origin) + " → " + airportLabel(route.Destination)
			if route.Midpoint != nil {
				routeText = airportLabel(route.Origin) + " → " + airportLabel(*route.Midpoint) + " → " + airportLabel(route.Destination)
			}
			embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Route (ADSBDB)", Value: routeText})
		}
		stale := ""
		if enrichment.Stale {
			stale = " • cached/stale"
		}
		embed.Footer = &discord.EmbedFooter{Text: "Live readsb data • ADSBDB enrichment shown" + stale}
	}
	return BoundEmbed(embed)
}

func Nearby(aircraft []domain.Aircraft, page, pageSize int, now time.Time) discord.Embed {
	if pageSize < 1 {
		pageSize = 10
	}
	if page < 0 {
		page = 0
	}
	copyAircraft := append([]domain.Aircraft(nil), aircraft...)
	start := page * pageSize
	if start > len(copyAircraft) {
		start = len(copyAircraft)
	}
	end := min(start+pageSize, len(copyAircraft))
	embed := base("Nearby", Scope, now)
	embed.Description = fmt.Sprintf("Showing %d–%d of %d aircraft • page %d", min(start+1, len(copyAircraft)), end, len(copyAircraft), page+1)
	for _, item := range copyAircraft[start:end] {
		name := firstNonEmpty(item.Callsign, item.Registration, item.ICAO)
		value := fmt.Sprintf("`%s` • %s • %s • %s", item.ICAO, position(item), altitude(item), groundSpeed(item))
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: name, Value: value})
	}
	if len(copyAircraft) == 0 {
		embed.Description = "No current aircraft match this view."
	}
	return BoundEmbed(embed)
}

func Help(now time.Time, manageGuild bool) discord.Embed {
	embed := base("Help", Scope, now).WithDescription("Use SkyFeed’s application commands to inspect live receiver data. Administrative actions respond privately.")
	embed.Fields = []discord.EmbedField{
		{Name: "/status", Value: "Receiver and bot health at a glance."},
		{Name: "/nearby", Value: "A paginated nearby-aircraft view with bounded filters."},
		{Name: "/aircraft", Value: "Look up a live aircraft by ICAO, registration, or callsign."},
		{Name: "/watch and /alerts", Value: "Manage watch rules and notification behavior."},
		{Name: "/reports and /feeder", Value: "Build summaries or inspect source diagnostics."},
	}
	if manageGuild {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "/settings", Value: "Configure channels and send permission-safe destination tests."})
	}
	return BoundEmbed(embed)
}

func Alert(alert domain.Alert) discord.Embed {
	color := Caution
	view := "Alert"
	if alert.Priority == domain.AlertEmergency {
		color = Emergency
		view = "Emergency"
	}
	embed := base(view, color, alert.ObservedAt).WithDescription(alert.Description)
	embed.Fields = []discord.EmbedField{
		{Name: "Aircraft", Value: valueOr(alert.AircraftICAO, "Unknown"), Inline: ptr(true)},
		{Name: "Rule", Value: string(alert.Type), Inline: ptr(true)},
		{Name: "Priority", Value: map[bool]string{true: "EMERGENCY", false: "NORMAL"}[alert.Priority == domain.AlertEmergency], Inline: ptr(true)},
	}
	return BoundEmbed(embed)
}

func Report(summary storage.ReportSummary) discord.Embed {
	embed := base("Report", Scope, summary.To).
		WithDescription(fmt.Sprintf("<t:%d:f> to <t:%d:f>", summary.From.Unix(), summary.To.Unix()))
	embed.Fields = []discord.EmbedField{
		{Name: "Aircraft observations", Value: fmt.Sprintf("%d", summary.AircraftSeen), Inline: ptr(true)},
		{Name: "Peak tracked aircraft", Value: fmt.Sprintf("%d", summary.DistinctICAOs), Inline: ptr(true)},
		{Name: "Messages", Value: fmt.Sprintf("%d", summary.Messages), Inline: ptr(true)},
		{Name: "Emergency observations", Value: fmt.Sprintf("%d", summary.Emergencies), Inline: ptr(true)},
		{Name: "Maximum range", Value: fmt.Sprintf("%.1f NM", summary.MaximumRangeNM), Inline: ptr(true)},
	}
	return BoundEmbed(embed)
}

func DestinationTest(purpose string) discord.Embed {
	return BoundEmbed(base("Destination test", Radar, time.Now()).
		WithDescription(fmt.Sprintf("🟢 **LIVE** — `%s` delivery is configured and allowed mentions are disabled.", purpose)))
}

func base(view string, color int, timestamp time.Time) discord.Embed {
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return discord.NewEmbed().WithTitle("SkyFeed • "+view).WithColor(color).WithTimestamp(timestamp).WithFooter(footer, "")
}

func overallHealth(health domain.Health) (domain.HealthStatus, int) {
	statuses := []domain.HealthStatus{health.Aircraft.Status, health.Receiver.Status, health.Stats.Status}
	for _, status := range statuses {
		if status == domain.HealthOffline {
			return status, Emergency
		}
	}
	for _, status := range statuses {
		if status == domain.HealthDegraded || status == domain.HealthStale {
			return status, Caution
		}
	}
	for _, status := range statuses {
		if status == domain.HealthUnknown {
			return status, Muted
		}
	}
	return domain.HealthHealthy, Radar
}

func badge(status domain.HealthStatus) string {
	switch status {
	case domain.HealthHealthy:
		return "🟢"
	case domain.HealthStale, domain.HealthDegraded:
		return "🟡"
	case domain.HealthOffline:
		return "🔴"
	default:
		return "⚪"
	}
}

func sourceLabel(health domain.SourceHealth) string {
	label := badge(health.Status) + " " + strings.ToUpper(string(health.Status))
	if health.ErrorClass != "" {
		label += " (`" + Truncate(health.ErrorClass, 48) + "`)"
	}
	return label
}

func position(aircraft domain.Aircraft) string {
	if !aircraft.HasDistance {
		return "Position unavailable"
	}
	return fmt.Sprintf("%.1f NM • %03.0f°", aircraft.DistanceNM, aircraft.BearingDegrees)
}

func altitude(aircraft domain.Aircraft) string {
	if aircraft.OnGround {
		return "Ground"
	}
	if !aircraft.HasAltitude {
		return "Unknown"
	}
	return fmt.Sprintf("%d ft", aircraft.AltitudeFeet)
}

func groundSpeed(aircraft domain.Aircraft) string {
	if !aircraft.HasGroundSpeed {
		return "Unknown"
	}
	return fmt.Sprintf("%.0f kt", aircraft.GroundSpeedKts)
}

func track(aircraft domain.Aircraft) string {
	if !aircraft.HasTrack {
		return "Unknown"
	}
	return fmt.Sprintf("%03.0f°", aircraft.TrackDegrees)
}

func verticalRate(aircraft domain.Aircraft) string {
	if !aircraft.HasVerticalRate {
		return "Unknown"
	}
	return fmt.Sprintf("%+d ft/min", aircraft.VerticalRateFPM)
}

func conciseDuration(value time.Duration) string {
	if value < time.Second {
		return "<1s"
	}
	return value.Round(time.Second).String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Unknown"
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	if len(result) == 0 {
		return []string{"Unavailable"}
	}
	return result
}

func airportLabel(airport domain.Airport) string {
	return firstNonEmpty(airport.ICAO, airport.IATA, airport.Name, "Unknown")
}

func ptr[T any](value T) *T { return &value }
