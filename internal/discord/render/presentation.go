package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const (
	Radar          = 0x35D07F
	Scope          = 0x37B5FF
	Caution        = 0xF3B63A
	EmergencyColor = 0xF05252
	Muted          = 0x6B7280

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
	activeProvider := "Unknown"
	if snapshot.ActiveProvider.Known() {
		activeProvider = string(snapshot.ActiveProvider)
	}
	providerAge := "Never"
	if !snapshot.ProviderChangedAt.IsZero() {
		age := now.Sub(snapshot.ProviderChangedAt)
		if age < 0 {
			age = 0
		}
		providerAge = conciseDuration(age) + " ago"
	}
	embed.Fields = []discord.EmbedField{
		{Name: "Tracked", Value: tracked, Inline: ptr(true)},
		{Name: "Recent message rate", Value: messageRate, Inline: ptr(true)},
		{Name: "Recent maximum range", Value: maximumRange, Inline: ptr(true)},
		{Name: "Active aircraft provider", Value: PlainText(activeProvider), Inline: ptr(true)},
		{Name: "Provider active since", Value: providerAge, Inline: ptr(true)},
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
	activeProvider := "unknown"
	if snapshot.ActiveProvider.Known() {
		activeProvider = string(snapshot.ActiveProvider)
	}
	embed := base("Feeder", color, snapshot.PublishedAt).
		WithDescription(fmt.Sprintf("%s **%s** — active aircraft provider `%s`.", badge(status), strings.ToUpper(string(status)), PlainText(activeProvider)))
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
	providerField := "Unknown"
	if snapshot.ActiveProvider.Known() {
		providerField = string(snapshot.ActiveProvider)
	}
	embed.Fields = []discord.EmbedField{
		{Name: "Active provider", Value: PlainText(providerField), Inline: ptr(true)},
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
		color = EmergencyColor
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
	return AircraftWithEnrichment(aircraft, snapshot, nil, nil, now)
}

func Route(route domain.Route, aircraft domain.Aircraft, now time.Time) discord.Embed {
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Route • "+PlainText(identity), Scope, now)
	embed.Description = routeText(route)
	fields := []discord.EmbedField{
		{Name: "Callsign", Value: PlainText(valueOr(route.Callsign, aircraft.Callsign)), Inline: ptr(true)},
		{Name: "Origin", Value: airportSummary(route.Origin), Inline: ptr(true)},
		{Name: "Destination", Value: airportSummary(route.Destination), Inline: ptr(true)},
	}
	if route.Midpoint != nil {
		fields = append(fields, discord.EmbedField{Name: "Midpoint", Value: airportSummary(*route.Midpoint), Inline: ptr(true)})
	}
	if route.PlausibilityKnown {
		plausibility := "Unknown"
		if route.Plausible {
			plausibility = "Plausible"
		} else {
			plausibility = "Questionable"
		}
		fields = append(fields, discord.EmbedField{Name: "Plausibility", Value: plausibility, Inline: ptr(true)})
	}
	if route.AirlineName != "" || route.AirlineICAO != "" {
		fields = append(fields, discord.EmbedField{Name: "Airline", Value: PlainText(strings.TrimSpace(route.AirlineName + " " + route.AirlineICAO)), Inline: ptr(false)})
	}
	embed.Fields = fields
	if route.Attribution != "" {
		embed.Footer = &discord.EmbedFooter{Text: PlainText(route.Attribution)}
	}
	return BoundEmbed(embed)
}

func Airport(airport domain.Airport, now time.Time) discord.Embed {
	return AirportWithWeather(airport, "", "", now)
}

func AirportWithWeather(airport domain.Airport, metar, taf string, now time.Time) discord.Embed {
	title := PlainText(firstNonEmpty(airport.ICAO, airport.IATA, "Airport"))
	embed := base("Airport • "+title, Scope, now)
	location := strings.Join(nonEmpty(PlainText(airport.Municipality), PlainText(airport.CountryCode)), ", ")
	if location == "" {
		location = "Location unavailable"
	}
	elevation := "Unavailable"
	if airport.HasElevation {
		elevation = fmt.Sprintf("%d ft", int(airport.ElevationFeet))
	}
	embed.Description = PlainText(valueOr(airport.Name, "Name unavailable"))
	embed.Fields = []discord.EmbedField{
		{Name: "ICAO", Value: PlainText(valueOr(airport.ICAO, "Unknown")), Inline: ptr(true)},
		{Name: "IATA", Value: PlainText(valueOr(airport.IATA, "Unknown")), Inline: ptr(true)},
		{Name: "Location", Value: location, Inline: ptr(true)},
		{Name: "Elevation", Value: elevation, Inline: ptr(true)},
	}
	if metar != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "METAR", Value: Truncate(PlainText(metar), 900)})
	}
	if taf != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "TAF", Value: Truncate(PlainText(taf), 900)})
	}
	if airport.Attribution != "" {
		embed.Footer = &discord.EmbedFooter{Text: PlainText(airport.Attribution)}
	}
	return BoundEmbed(embed)
}

func LookingUp(kind, identity string, now time.Time) discord.Embed {
	return BoundEmbed(base(kind, Muted, now).WithDescription(fmt.Sprintf("Looking up **%s**…", PlainText(identity))))
}

func Emergency(aircraft []domain.Aircraft, page, pageSize int, now time.Time) discord.Embed {
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
	embed := base("Emergency", EmergencyColor, now)
	embed.Description = fmt.Sprintf("Active 7500 / 7600 / 7700 or emergency flags • showing %d–%d of %d", min(start+1, len(copyAircraft)), end, len(copyAircraft))
	for _, item := range copyAircraft[start:end] {
		name := PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO))
		detail := squawkMeaning(item.Squawk)
		if item.Emergency != "" && item.Emergency != "none" {
			detail = strings.ToUpper(item.Emergency)
		}
		value := fmt.Sprintf("`%s` • squawk `%s` • %s • %s • %s", PlainText(item.ICAO), PlainText(valueOr(item.Squawk, "????")), detail, position(item), altitude(item))
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: name, Value: value})
	}
	if len(copyAircraft) == 0 {
		embed.Description = "No emergency squawks or emergency flags are currently visible."
		embed.Color = Radar
	}
	return BoundEmbed(embed)
}

func Traffic(aircraft []domain.Aircraft, airportCode string, radiusNM float64, page, pageSize int, now time.Time) discord.Embed {
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
	label := PlainText(valueOr(airportCode, "public airport"))
	embed := base("Traffic • "+label, Scope, now)
	embed.Description = fmt.Sprintf("Airport-area traffic near %s within %.0f NM of the configured public center • showing %d–%d of %d", label, radiusNM, min(start+1, len(copyAircraft)), end, len(copyAircraft))
	for _, item := range copyAircraft[start:end] {
		name := PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO))
		value := fmt.Sprintf("`%s` • %s • %s • %s", PlainText(item.ICAO), position(item), altitude(item), groundSpeed(item))
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: name, Value: value})
	}
	if len(copyAircraft) == 0 {
		embed.Description = fmt.Sprintf("No visible aircraft are currently within %.0f NM of %s.", radiusNM, label)
	}
	return BoundEmbed(embed)
}

func Squawk(aircraft []domain.Aircraft, code string, page, pageSize int, now time.Time) discord.Embed {
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
	embed := base("Squawk • "+PlainText(code), Scope, now)
	embed.Description = squawkMeaning(code) + fmt.Sprintf("\nShowing %d–%d of %d aircraft • page %d", min(start+1, len(copyAircraft)), end, len(copyAircraft), page+1)
	for _, item := range copyAircraft[start:end] {
		name := PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO))
		value := fmt.Sprintf("`%s` • %s • %s • %s", PlainText(item.ICAO), position(item), altitude(item), groundSpeed(item))
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: name, Value: value})
	}
	if len(copyAircraft) == 0 {
		embed.Description = squawkMeaning(code) + "\nNo current aircraft match this squawk."
	}
	return BoundEmbed(embed)
}

func Top(aircraft []domain.Aircraft, metric string, limit int, now time.Time) discord.Embed {
	embed := base("Top aircraft • "+metricLabel(metric), Scope, now)
	if len(aircraft) == 0 {
		embed.Description = "No current aircraft are available for this ranking."
		return BoundEmbed(embed)
	}
	embed.Description = fmt.Sprintf("Top %d visible aircraft by %s", min(limit, len(aircraft)), metricLabel(metric))
	for index, item := range aircraft {
		name := fmt.Sprintf("%d. %s", index+1, PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO)))
		value := fmt.Sprintf("`%s` • %s • %s • %s", PlainText(item.ICAO), metricValue(item, metric), altitude(item), groundSpeed(item))
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: name, Value: value})
	}
	return BoundEmbed(embed)
}

func Privacy(disclosure privacy.Disclosure) discord.Embed {
	embed := base("Privacy", Scope, time.Now())
	providers := "readsb only"
	if len(disclosure.Providers) > 0 {
		providers = strings.Join(disclosure.Providers, ", ")
	}
	center := "No external point-query source is configured."
	if disclosure.PublicAirportCode != "" {
		center = fmt.Sprintf("External aircraft fallback queries use public airport %s within %d NM (airplanes.live: 1 req/s, max 250 NM).", PlainText(disclosure.PublicAirportCode), disclosure.RadiusNM)
	}
	embed.Description = "SkyFeed shares only the data described below. Receiver coordinates, fallback center coordinates, and private site values are never shown, logged, or stored."
	embed.Fields = []discord.EmbedField{
		{Name: "Providers", Value: PlainText(providers), Inline: ptr(false)},
		{Name: "Public center", Value: center, Inline: ptr(false)},
	}
	retention := make([]string, 0, len(disclosure.Retention))
	for _, item := range disclosure.Retention {
		retention = append(retention, fmt.Sprintf("%s: %s", PlainText(item.Category), PlainText(item.Period)))
	}
	if len(retention) > 0 {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Retention", Value: strings.Join(retention, "\n"), Inline: ptr(false)})
	}
	attribution := make([]string, 0, len(disclosure.Attribution))
	for _, item := range disclosure.Attribution {
		attribution = append(attribution, PlainText(item.Notice))
	}
	if len(attribution) > 0 {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Attribution", Value: strings.Join(attribution, "\n"), Inline: ptr(false)})
	}
	return BoundEmbed(embed)
}

func AircraftWithEnrichment(aircraft domain.Aircraft, snapshot *domain.Snapshot, enrichment *domain.Enrichment, route *domain.Route, now time.Time) discord.Embed {
	color := Scope
	alert := "None"
	if aircraft.Emergency != "" && aircraft.Emergency != "none" {
		color = EmergencyColor
		alert = "🔴 " + strings.ToUpper(aircraft.Emergency)
	}
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Aircraft • "+PlainText(identity), color, now)
	sourceLabel := "readsb"
	if aircraft.Provider.Known() {
		sourceLabel = string(aircraft.Provider)
	}
	embed.Description = fmt.Sprintf("`%s` • %s • %s • source %s",
		PlainText(aircraft.ICAO),
		PlainText(valueOr(aircraft.Registration, "registration unknown")),
		PlainText(valueOr(aircraft.AircraftType, "type unknown")),
		PlainText(sourceLabel),
	)
	embed.Fields = []discord.EmbedField{
		{Name: "Position", Value: position(aircraft), Inline: ptr(true)},
		{Name: "Altitude", Value: altitude(aircraft), Inline: ptr(true)},
		{Name: "Ground speed", Value: groundSpeed(aircraft), Inline: ptr(true)},
		{Name: "Track", Value: track(aircraft), Inline: ptr(true)},
		{Name: "Vertical rate", Value: verticalRate(aircraft), Inline: ptr(true)},
		{Name: "Squawk", Value: PlainText(valueOr(aircraft.Squawk, "Unknown")), Inline: ptr(true)},
		{Name: "Alert state", Value: alert, Inline: ptr(false)},
	}
	if snapshot != nil {
		embed.Footer = &discord.EmbedFooter{Text: fmt.Sprintf("Live receiver data • observation age %s", conciseDuration(aircraft.Seen))}
	}
	if route != nil {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Route", Value: routeText(*route)})
		if route.Attribution != "" {
			embed.Footer = &discord.EmbedFooter{Text: PlainText(route.Attribution)}
		}
	}
	if enrichment != nil && enrichment.Found {
		if metadata := enrichment.Aircraft; metadata != nil {
			embed.Fields = append(embed.Fields,
				discord.EmbedField{Name: "Aircraft metadata", Value: strings.Join(nonEmpty(PlainText(metadata.Manufacturer), PlainText(metadata.AircraftType), PlainText(metadata.Registration)), " • ")},
				discord.EmbedField{Name: "Owner / operator", Value: strings.Join(nonEmpty(PlainText(metadata.Owner), PlainText(metadata.OwnerCountry)), " • ")},
			)
			if metadata.PhotoURL != "" {
				embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Photo", Value: PlainText(metadata.PhotoURL)})
			}
			if metadata.ThumbnailURL != "" {
				embed.Thumbnail = &discord.EmbedResource{URL: metadata.ThumbnailURL}
			}
		}
		if route == nil && enrichment.Route != nil {
			embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Route (ADSBDB)", Value: routeText(*enrichment.Route)})
		}
		stale := ""
		if enrichment.Stale {
			stale = " • cached/stale"
		}
		embed.Footer = &discord.EmbedFooter{Text: "Live receiver data • ADSBDB enrichment shown" + stale}
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
		{Name: "/status", Value: "Receiver, active provider, and bot health at a glance."},
		{Name: "/nearby and /traffic", Value: "Paginated nearby or public-airport-area aircraft views."},
		{Name: "/aircraft", Value: "Look up a live aircraft by ICAO, registration, or callsign."},
		{Name: "/route, /airport, /squawk, /top, /emergency", Value: "Routes, airports/METAR, squawks, rankings, and emergency drill-down."},
		{Name: "/privacy", Value: "Review provider sharing, retention, and attribution for this server."},
		{Name: "/watch and /alerts", Value: "Manage watch rules and notification behavior."},
		{Name: "/reports and /feeder", Value: "Build summaries (including peak hour) or inspect source diagnostics."},
		{Name: "Interesting aircraft channel", Value: "Configure `#interesting-aircraft` via `/settings channels` for plane-alert-db first sightings."},
	}
	if manageGuild {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "/settings", Value: "Channels, alert pause/mute, dashboard recreate, and destination tests."})
	}
	return BoundEmbed(embed)
}

func Alert(alert domain.Alert) discord.Embed {
	color := Caution
	view := "Alert"
	if alert.Priority == domain.AlertEmergency {
		color = EmergencyColor
		view = "Emergency"
	}
	description := PlainText(alert.Description)
	if alert.RouteSummary != "" {
		description = description + "\nRoute: " + PlainText(alert.RouteSummary)
	}
	embed := base(view, color, alert.ObservedAt).WithDescription(description)
	embed.Fields = []discord.EmbedField{
		{Name: "Aircraft", Value: valueOr(alert.AircraftICAO, "Unknown"), Inline: ptr(true)},
		{Name: "Callsign", Value: PlainText(valueOr(alert.Callsign, "Unknown")), Inline: ptr(true)},
		{Name: "Rule", Value: string(alert.Type), Inline: ptr(true)},
		{Name: "Priority", Value: map[bool]string{true: "EMERGENCY", false: "NORMAL"}[alert.Priority == domain.AlertEmergency], Inline: ptr(true)},
	}
	return BoundEmbed(embed)
}

func InterestingAlert(alert domain.Alert) discord.Embed {
	description := PlainText(alert.Description)
	if alert.RouteSummary != "" {
		description = description + "\nRoute: " + PlainText(alert.RouteSummary)
	}
	embed := base("Interesting aircraft", Scope, alert.ObservedAt).WithDescription(description)
	if alert.Title != "" {
		embed = embed.WithTitle("SkyFeed • " + PlainText(alert.Title))
	}
	embed.Fields = []discord.EmbedField{
		{Name: "ICAO", Value: valueOr(alert.AircraftICAO, "Unknown"), Inline: ptr(true)},
		{Name: "Callsign", Value: PlainText(valueOr(alert.Callsign, "Unknown")), Inline: ptr(true)},
		{Name: "Group", Value: PlainText(valueOr(alert.InterestingGroup, "Unknown")), Inline: ptr(true)},
	}
	if alert.InterestingOperator != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Operator", Value: PlainText(alert.InterestingOperator), Inline: ptr(false)})
	}
	if alert.InterestingTags != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Tags", Value: PlainText(alert.InterestingTags), Inline: ptr(false)})
	}
	if alert.InterestingLink != "" {
		embed.Fields = append(embed.Fields, discord.EmbedField{Name: "Reference", Value: PlainText(alert.InterestingLink), Inline: ptr(false)})
	}
	if alert.InterestingImage != "" {
		embed.Thumbnail = &discord.EmbedResource{URL: alert.InterestingImage}
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
	if !summary.PeakHour.IsZero() {
		embed.Fields = append(embed.Fields, discord.EmbedField{
			Name: "Busiest hour", Value: fmt.Sprintf("<t:%d:f> • %d peak tracked", summary.PeakHour.Unix(), summary.PeakAircraft), Inline: ptr(false),
		})
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
			return status, EmergencyColor
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
	return PlainText(firstNonEmpty(airport.ICAO, airport.IATA, airport.Name, "Unknown"))
}

func airportSummary(airport domain.Airport) string {
	parts := nonEmpty(airportLabel(airport), PlainText(airport.Municipality), PlainText(airport.CountryCode))
	return strings.Join(parts, " • ")
}

func routeText(route domain.Route) string {
	text := airportLabel(route.Origin) + " → " + airportLabel(route.Destination)
	if route.Midpoint != nil {
		text = airportLabel(route.Origin) + " → " + airportLabel(*route.Midpoint) + " → " + airportLabel(route.Destination)
	}
	return text
}

func squawkMeaning(code string) string {
	switch code {
	case "7500":
		return "7500 — unlawful interference (hijack)"
	case "7600":
		return "7600 — radio failure"
	case "7700":
		return "7700 — general emergency"
	default:
		return PlainText(code) + " — assigned transponder code"
	}
}

func metricLabel(metric string) string {
	switch metric {
	case "altitude":
		return "altitude"
	case "speed":
		return "ground speed"
	case "messages":
		return "messages"
	case "signal":
		return "signal"
	default:
		return "distance"
	}
}

func metricValue(aircraft domain.Aircraft, metric string) string {
	switch metric {
	case "altitude":
		return altitude(aircraft)
	case "speed":
		return groundSpeed(aircraft)
	case "messages":
		return fmt.Sprintf("%d msgs", aircraft.Messages)
	case "signal":
		if !aircraft.HasRSSI {
			return "Unknown"
		}
		return fmt.Sprintf("%.1f dBm", aircraft.RSSI)
	default:
		return position(aircraft)
	}
}

func ptr[T any](value T) *T { return &value }
