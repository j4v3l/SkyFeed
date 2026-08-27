package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
	trackdata "github.com/j4v3l/SkyFeed/internal/track"
)

const (
	Radar          = 0x35D07F
	Scope          = 0x37B5FF
	Caution        = 0xF3B63A
	EmergencyColor = 0xF05252
	Muted          = 0x6B7280

	footer = "SkyFeed • live aviation data"
)

func SafeMessage(embed discord.Embed, ephemeral bool) discord.MessageCreate {
	mentions := discord.AllowedMentions{}
	return discord.NewMessageCreate().WithEmbeds(BoundEmbed(embed)).WithEphemeral(ephemeral).WithAllowedMentions(&mentions)
}

func InterestingAlertMessage(alert domain.Alert, ephemeral bool) discord.MessageCreate {
	message := SafeMessage(InterestingAlert(alert), ephemeral)
	if link, ok := SafeHTTPSURL(alert.InterestingLink); ok {
		message = message.AddActionRow(discord.NewLinkButton(referenceLinkLabel(link), link))
	}
	return message
}

func Status(snapshot *domain.Snapshot, uptime time.Duration, now time.Time, enrichmentEnabled bool) discord.Embed {
	return StatusWithUnits(snapshot, uptime, now, enrichmentEnabled, domain.UnitsAviation)
}

func StatusWithUnits(snapshot *domain.Snapshot, uptime time.Duration, now time.Time, enrichmentEnabled bool, units domain.UnitSystem) discord.Embed {
	if snapshot == nil {
		return base("Status", Muted, now).WithDescription("⚪ **UNKNOWN** — waiting for the first receiver payload.")
	}
	status, color := overallHealth(snapshot.Health)
	description := fmt.Sprintf("%s **%s**\nWaiting for the first aircraft update.", badge(status), strings.ToUpper(string(status)))
	if !snapshot.FetchedAt.IsZero() {
		description = fmt.Sprintf("%s **%s**\nLive state updated %s.", badge(status), strings.ToUpper(string(status)), RelativeTime(snapshot.FetchedAt, "recently"))
	}
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
		maximumRange = distance(snapshot.Statistics.MaxRangeNM, units)
	}
	activeProvider := "Unknown"
	if snapshot.FeederID == domain.FeederAll && len(snapshot.Feeders) > 0 {
		activeProvider = fmt.Sprintf("Community aggregate · %d feeders", len(snapshot.Feeders))
	} else if snapshot.ActiveProvider.Known() {
		activeProvider = string(snapshot.ActiveProvider)
	}
	providerAge := "Never"
	if snapshot.FeederID == domain.FeederAll && len(snapshot.Feeders) > 0 {
		providerAge = "latest observations"
	} else if !snapshot.ProviderChangedAt.IsZero() {
		age := now.Sub(snapshot.ProviderChangedAt)
		if age < 0 {
			age = 0
		}
		providerAge = conciseDuration(age) + " ago"
	}
	// Full-width section fields stay labeled on mobile; avoid many tiny inline columns.
	embed := base("Status", color, snapshot.PublishedAt).WithDescription(description)
	embed.Fields = []discord.EmbedField{
		section("📡 Live traffic", Facts(tracked, messageRate, "Max range "+maximumRange)),
		section("🛰️ Data source", Facts("`"+PlainText(activeProvider)+"`", providerAge)),
		section("🩺 Source health", fmt.Sprintf("**Aircraft** %s\n**Receiver** %s\n**Statistics** %s", sourceLabel(snapshot.Health.Aircraft), sourceLabel(snapshot.Health.Receiver), sourceLabel(snapshot.Health.Stats))),
		section("🤖 SkyFeed", Facts("Up "+conciseDuration(uptime), "Enrichment "+enrichmentStatus)),
	}
	if overview := feederOverview(snapshot.Feeders); overview != "" {
		embed.Fields = append(embed.Fields, section("Community coverage", overview))
	}
	embed.Footer = &discord.EmbedFooter{Text: providerFooter(snapshot, nil, nil, now)}
	return BoundEmbed(embed)
}

func feederOverview(feeders []domain.FeederSummary) string {
	if len(feeders) == 0 {
		return ""
	}
	healthy, attention, offline, disabled := 0, 0, 0, 0
	areas := make(map[string]int)
	for _, feeder := range feeders {
		if !feeder.Enabled {
			disabled++
			continue
		}
		switch feeder.Health {
		case domain.HealthHealthy:
			healthy++
		case domain.HealthOffline:
			offline++
		default:
			attention++
		}
		if area := strings.TrimSpace(feeder.PublicArea); area != "" {
			areas[PlainText(area)]++
		}
	}
	areaNames := make([]string, 0, len(areas))
	for area, count := range areas {
		if count > 1 {
			area += fmt.Sprintf(" ×%d", count)
		}
		areaNames = append(areaNames, area)
	}
	sort.Strings(areaNames)
	coverage := "No public areas configured"
	if len(areaNames) > 0 {
		coverage = strings.Join(areaNames, " · ")
	}
	return Truncate(fmt.Sprintf("🟢 %d healthy · 🟡 %d attention · 🔴 %d offline · ⚪ %d paused\n%s", healthy, attention, offline, disabled, coverage), 1000)
}

func Feeder(snapshot *domain.Snapshot, now time.Time) discord.Embed {
	return FeederWithUnits(snapshot, now, domain.UnitsAviation)
}

func FeederWithUnits(snapshot *domain.Snapshot, now time.Time, units domain.UnitSystem) discord.Embed {
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
		maximumRange = distance(snapshot.Statistics.MaxRangeNM, units)
	}
	providerField := "Unknown"
	if snapshot.ActiveProvider.Known() {
		providerField = string(snapshot.ActiveProvider)
	}
	embed.Fields = []discord.EmbedField{
		section("📻 Receiver", Facts(PlainText(receiverVersion), "Refresh "+refresh, "Position "+positionState)),
		section("📊 Statistics", Facts(window, messages+" messages", tracks+" tracks", "Max range "+maximumRange)),
		section("🛰️ Provider", "`"+PlainText(providerField)+"`"),
		section("🩺 JSON sources", fmt.Sprintf("**Aircraft** %s\n**Receiver** %s\n**Statistics** %s", sourceLabel(snapshot.Health.Aircraft), sourceLabel(snapshot.Health.Receiver), sourceLabel(snapshot.Health.Stats))),
	}
	embed.Footer = &discord.EmbedFooter{Text: providerFooter(snapshot, nil, nil, now)}
	return BoundEmbed(embed)
}

func WithAirportUpdate(embed discord.Embed, airportCode string, weather WeatherView, activity domain.AirportActivity, units domain.UnitSystem, now time.Time) discord.Embed {
	code := PlainText(valueOr(airportCode, "Local airport"))
	embed.Fields = append(embed.Fields,
		section("🌤️ "+code+" weather", weatherSummary(weather, now, units)),
		section("✈️ "+code+" movements", activitySummary(activity, units, now)),
	)
	return BoundEmbed(embed)
}

func WithAirportWeather(embed discord.Embed, airportCode string, weather WeatherView, units domain.UnitSystem, now time.Time) discord.Embed {
	code := PlainText(valueOr(airportCode, "Local airport"))
	embed.Fields = append(embed.Fields, section("🌤️ "+code+" weather", weatherSummary(weather, now, units)))
	return BoundEmbed(embed)
}

func WithCommunityActivity(embed discord.Embed, views []CommunityActivityView, now time.Time) discord.Embed {
	lines := make([]string, 0, min(len(views), 8))
	for _, view := range views {
		if !view.Activity.Configured {
			continue
		}
		landings, departures, approaches := 0, 0, 0
		for _, movement := range view.Activity.Movements {
			switch movement.Phase {
			case domain.MovementLanded:
				landings++
			case domain.MovementDeparture:
				departures++
			case domain.MovementApproach:
				approaches++
			}
		}
		label := PlainText(firstNonEmpty(view.Area, view.Airport, "Approved area"))
		if airport := strings.ToUpper(strings.TrimSpace(view.Airport)); airport != "" && !strings.EqualFold(label, airport) {
			label += " (`" + PlainText(airport) + "`)"
		}
		trend := "quiet right now"
		if landings+departures+approaches > 0 {
			trend = fmt.Sprintf("%d likely landing · %d likely departure · %d approach trend", landings, departures, approaches)
		}
		lines = append(lines, "**"+label+"** — "+trend)
		if len(lines) == 8 {
			break
		}
	}
	if len(lines) == 0 {
		return embed
	}
	embed.Fields = append(embed.Fields, section("✈️ Activity by area", strings.Join(lines, "\n")))
	return BoundEmbed(embed)
}

func ModerationCase(value storage.ModerationCase) discord.Embed {
	color := Radar
	if value.Status == "failed" {
		color = EmergencyColor
	} else if value.Status == "pending" {
		color = Caution
	}
	embed := base(fmt.Sprintf("Moderation case %d", value.ID), color, value.CreatedAt).
		WithDescription(fmt.Sprintf("%s **%s**\n%s", moderationStatusIcon(value.Status), strings.ToUpper(value.Status), PlainText(strings.ToUpper(value.Action))))
	embed.Fields = []discord.EmbedField{
		section("👤 People", Facts(fmt.Sprintf("**Target** `%d`", value.TargetUserID), fmt.Sprintf("**Moderator** `%d`", value.ModeratorID))),
		section("🕒 Timing", Facts(labeledMarkup("Created", RelativeTime(value.CreatedAt, "Unknown")), labeledMarkup("Completed", RelativeTime(value.CompletedAt, "Pending")))),
		section("📝 Reason", Truncate(PlainText(value.Reason), 400)),
		section("📨 Delivery", Labeled("Direct message", value.DMStatus)),
	}
	if value.Duration > 0 {
		embed.Fields = append(embed.Fields, section("⏱️ Duration", conciseDuration(value.Duration)))
	}
	if value.DeleteMessageDuration > 0 {
		embed.Fields = append(embed.Fields, section("🧹 Message deletion", conciseDuration(value.DeleteMessageDuration)))
	}
	if value.ErrorCode != "" {
		embed.Fields = append(embed.Fields, section("⚠️ Failure class", PlainText(value.ErrorCode)))
	}
	if !value.CompletedAt.IsZero() {
		embed.Timestamp = &value.CompletedAt
	}
	return BoundEmbed(embed)
}

func Aircraft(aircraft domain.Aircraft, snapshot *domain.Snapshot, now time.Time) discord.Embed {
	return AircraftWithEnrichment(aircraft, snapshot, nil, nil, now)
}

func AircraftSummary(aircraft domain.Aircraft, snapshot *domain.Snapshot, units domain.UnitSystem, now time.Time) discord.Embed {
	color := Scope
	alert := "No active emergency"
	if domain.EmergencyActive(aircraft) {
		color = EmergencyColor
		alert = strings.ToUpper(firstNonEmpty(aircraft.Emergency, domain.SquawkMeaning(aircraft.Squawk)))
	}
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Aircraft • "+PlainText(identity), color, now)
	embed.Description = fmt.Sprintf("✈️ **%s**\n`%s` · %s · %s",
		PlainText(identity), PlainText(aircraft.ICAO), PlainText(valueOr(aircraft.Registration, "Registration unknown")), PlainText(valueOr(aircraft.AircraftType, "Type unknown")))
	observation := now.Add(-max(aircraft.Seen, 0))
	embed.Fields = []discord.EmbedField{
		section("📍 Position", Facts(positionWithUnits(aircraft, units), altitudeWithUnits(aircraft, units), groundSpeedWithUnits(aircraft, units))),
		section("🧭 Movement", Facts("Track "+trackWithCompass(aircraft), verticalRateWithUnits(aircraft, units))),
		section("📡 Transponder", Facts("Squawk `"+PlainText(valueOr(aircraft.Squawk, "????"))+"`", PlainText(alert))),
		section("🕒 Freshness", Facts("Observed "+RelativeTime(observation, "recently"), strings.ToUpper(freshnessLabel(aircraft.Seen)))),
	}
	embed.Footer = &discord.EmbedFooter{Text: aircraftProviderFooter(aircraft, snapshot, nil, nil, now)}
	return BoundEmbed(embed)
}

func Route(route domain.Route, aircraft domain.Aircraft, originMETAR, destinationMETAR string, now time.Time) discord.Embed {
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Route • "+PlainText(identity), Scope, now)
	embed.Description = "**" + routeText(route) + "**"
	fields := []discord.EmbedField{
		section("Flight", fmt.Sprintf("`%s` · %s", PlainText(valueOr(route.Callsign, aircraft.Callsign)), PlainText(valueOr(aircraft.ICAO, "icao unknown")))),
	}
	if route.Midpoint != nil {
		fields = append(fields, section("Midpoint", airportSummary(*route.Midpoint)))
	}
	if route.PlausibilityKnown {
		plausibility := "Questionable"
		if route.Plausible {
			plausibility = "Plausible"
		}
		fields = append(fields, section("Plausibility", plausibility))
	}
	if route.AirlineName != "" || route.AirlineICAO != "" {
		fields = append(fields, section("Airline", PlainText(strings.TrimSpace(route.AirlineName+" "+route.AirlineICAO))))
	}
	if originMETAR != "" {
		fields = append(fields, section("Origin METAR", Truncate(PlainText(originMETAR), 900)))
	}
	if destinationMETAR != "" {
		fields = append(fields, section("Destination METAR", Truncate(PlainText(destinationMETAR), 900)))
	}
	embed.Fields = fields
	footerParts := []string{aircraftProviderFooter(aircraft, nil, nil, &route, now)}
	if originMETAR != "" || destinationMETAR != "" {
		footerParts = append(footerParts, "weather aviationweather.gov")
	}
	embed.Footer = &discord.EmbedFooter{Text: Truncate(strings.Join(uniqueStrings(footerParts), " • "), 2048)}
	return BoundEmbed(embed)
}

func Airport(airport domain.Airport, now time.Time) discord.Embed {
	return AirportWithWeather(airport, "", "", now)
}

func AirportWithWeather(airport domain.Airport, metar, taf string, now time.Time) discord.Embed {
	view := WeatherView{METAR: metar, TAF: taf, METARStatus: "not-found", TAFStatus: "not-found"}
	if metar != "" {
		view.METARStatus = "available"
	}
	if taf != "" {
		view.TAFStatus = "available"
	}
	return AirportWithWeatherView(airport, view, true, now)
}

type WeatherView struct {
	RequestedICAO        string
	ReportingICAO        string
	StationStatus        string
	StationDistanceNM    float64
	HasStationDistance   bool
	METAR                string
	TAF                  string
	FlightCategory       string
	METARStatus          string
	TAFStatus            string
	ObservedAt           time.Time
	FetchedAt            time.Time
	Stale                bool
	UpstreamFailed       bool
	Attribution          string
	WindDirectionDegrees int
	WindVariable         bool
	WindSpeedKts         int
	WindGustKts          int
	HasWind              bool
	VisibilitySM         float64
	VisibilityAtLeast    bool
	VisibilityLessThan   bool
	HasVisibility        bool
	TemperatureC         int
	DewpointC            int
	HasTemperature       bool
	HasDewpoint          bool
	AltimeterInHg        float64
	HasAltimeter         bool
	Clouds               []WeatherCloudView
	Conditions           []string
}

type WeatherCloudView struct {
	Cover    string
	BaseFeet int
	HasBase  bool
}

type CommunityActivityView struct {
	Area     string
	Airport  string
	Activity domain.AirportActivity
}

func AirportWithWeatherView(airport domain.Airport, weather WeatherView, rawDetails bool, now time.Time) discord.Embed {
	return AirportWithWeatherViewAndUnits(airport, weather, rawDetails, now, domain.UnitsAviation)
}

func AirportWithWeatherViewAndUnits(airport domain.Airport, weather WeatherView, rawDetails bool, now time.Time, units domain.UnitSystem) discord.Embed {
	mode := ""
	if rawDetails {
		mode = "weather-details"
	}
	return AirportDashboard(airport, weather, domain.AirportActivity{}, mode, now, units)
}

func AirportDashboard(airport domain.Airport, weather WeatherView, activity domain.AirportActivity, mode string, now time.Time, units domain.UnitSystem) discord.Embed {
	title := PlainText(firstNonEmpty(airport.ICAO, airport.IATA, "Airport"))
	color := weatherColor(weather)
	embed := base("Airport • "+title, color, now)
	location := strings.Join(nonEmpty(PlainText(airport.Municipality), PlainText(airport.CountryCode)), ", ")
	if location == "" {
		location = "Location unavailable"
	}
	elevation := "Unavailable"
	if airport.HasElevation {
		if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
			elevation = fmt.Sprintf("%.0f m", airport.ElevationFeet*0.3048)
		} else {
			elevation = fmt.Sprintf("%d ft", int(airport.ElevationFeet))
		}
	}
	embed.Description = "📍 **" + PlainText(valueOr(airport.Name, "Name unavailable")) + "**\n" + location
	identity := fmt.Sprintf("`%s` / `%s` · elevation %s", PlainText(valueOr(airport.ICAO, "????")), PlainText(valueOr(airport.IATA, "—")), elevation)
	switch mode {
	case "activity":
		embed.Description += "\nLikely arrivals and departures inferred from your local ADS-B feed."
		embed.Fields = []discord.EmbedField{section("Airport", identity)}
		embed.Fields = append(embed.Fields, activityFields(activity, units, now)...)
	case "weather-details":
		embed.Description += "\nCurrent flying weather in plain language, with the original reports below."
		embed.Fields = []discord.EmbedField{
			section("Airport", identity),
			section("Current conditions", weatherSummary(weather, now, units)),
		}
		if weather.METAR != "" {
			embed.Fields = append(embed.Fields, section("Raw METAR", Truncate(PlainText(weather.METAR), 900)))
		}
		if weather.TAF != "" {
			embed.Fields = append(embed.Fields, section("Raw TAF", Truncate(PlainText(weather.TAF), 900)))
		}
	default:
		embed.Description += "\nA quick look at flying weather and nearby airport activity."
		embed.Fields = []discord.EmbedField{
			section("At a glance", identity),
			section("🌤️ Flying weather", weatherSummary(weather, now, units)),
			section("✈️ Arrivals & departures", activitySummary(activity, units, now)),
		}
	}
	attribution := uniqueStrings([]string{airport.Attribution, weather.Attribution})
	if len(attribution) > 0 {
		embed.Footer = &discord.EmbedFooter{Text: PlainText(strings.Join(attribution, " • "))}
	}
	return BoundEmbed(embed)
}

func weatherSummary(weather WeatherView, now time.Time, units domain.UnitSystem) string {
	if weather.UpstreamFailed {
		return "⚪ Weather is temporarily unavailable. SkyFeed will try again on refresh."
	}
	status := "⚪ Conditions unavailable"
	if weather.METARStatus == "available" {
		category := strings.ToUpper(strings.TrimSpace(weather.FlightCategory))
		switch category {
		case "VFR":
			status = "🟢 **VFR** — generally good visual flying conditions"
		case "MVFR":
			status = "🟡 **MVFR** — visibility or clouds may need extra attention"
		case "IFR":
			status = "🟠 **IFR** — instrument conditions are being reported"
		case "LIFR":
			status = "🔴 **LIFR** — very low visibility or cloud ceiling"
		default:
			status = "🔵 Current airport weather is available"
		}
	} else if weather.METARStatus == "not-found" {
		status = "⚪ No current METAR is being reported"
	}
	lines := []string{status}
	if weather.ReportingICAO != "" && weather.RequestedICAO != "" && !strings.EqualFold(weather.ReportingICAO, weather.RequestedICAO) {
		station := fmt.Sprintf("📡 Observed at **%s** for %s", PlainText(weather.ReportingICAO), PlainText(weather.RequestedICAO))
		if weather.HasStationDistance {
			if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
				station += fmt.Sprintf(" · %.1f km away", weather.StationDistanceNM*1.852)
			} else {
				station += fmt.Sprintf(" · %.1f NM away", weather.StationDistanceNM)
			}
		}
		if weather.StationStatus == "nearby" {
			station += " · nearest reporting station"
		} else {
			station += " · renamed/replacement station"
		}
		lines = append(lines, station)
	}
	if weather.HasWind {
		wind := "variable wind"
		if weather.WindSpeedKts == 0 && weather.WindGustKts == 0 {
			wind = "calm wind"
		} else if !weather.WindVariable {
			wind = strings.ToLower(compassLong(float64(weather.WindDirectionDegrees))) + fmt.Sprintf(" wind from %03d°", weather.WindDirectionDegrees)
		}
		speed := fmt.Sprintf("%d kt", weather.WindSpeedKts)
		if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
			speed = fmt.Sprintf("%.0f km/h", float64(weather.WindSpeedKts)*1.852)
		}
		if weather.WindGustKts > 0 {
			if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
				speed += fmt.Sprintf(", gusting %.0f km/h", float64(weather.WindGustKts)*1.852)
			} else {
				speed += fmt.Sprintf(", gusting %d kt", weather.WindGustKts)
			}
		}
		lines = append(lines, "💨 "+wind+" at "+speed)
	}
	if weather.HasVisibility {
		prefix := ""
		if weather.VisibilityAtLeast {
			prefix = "at least "
		} else if weather.VisibilityLessThan {
			prefix = "less than "
		}
		visibility := fmt.Sprintf("%s%.1f statute miles", prefix, weather.VisibilitySM)
		if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
			visibility = fmt.Sprintf("%s%.1f km", prefix, weather.VisibilitySM*1.609344)
		}
		lines = append(lines, "👁️ Visibility "+visibility)
	}
	if clouds := cloudSummary(weather.Clouds, units); clouds != "" {
		lines = append(lines, "☁️ "+clouds)
	}
	if len(weather.Conditions) > 0 {
		lines = append(lines, "🌧️ "+PlainText(strings.Join(weather.Conditions, ", ")))
	}
	measurements := make([]string, 0, 3)
	if weather.HasTemperature {
		measurements = append(measurements, fmt.Sprintf("temperature %d°C", weather.TemperatureC))
	}
	if weather.HasDewpoint {
		measurements = append(measurements, fmt.Sprintf("dew point %d°C", weather.DewpointC))
	}
	if weather.HasAltimeter {
		pressure := fmt.Sprintf("%.2f inHg", weather.AltimeterInHg)
		if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
			pressure = fmt.Sprintf("%.0f hPa", weather.AltimeterInHg*33.8639)
		}
		measurements = append(measurements, "pressure "+pressure)
	}
	if len(measurements) > 0 {
		lines = append(lines, "🌡️ "+strings.Join(measurements, " · "))
	}
	taf := "forecast available"
	switch weather.TAFStatus {
	case "not-found":
		taf = "no airport forecast is being reported"
	case "unavailable":
		taf = "airport forecast is temporarily unavailable"
	case "":
		taf = "airport forecast not requested"
	}
	ageText := ""
	weatherTime := weather.ObservedAt
	if weatherTime.IsZero() {
		weatherTime = weather.FetchedAt
	}
	if !weatherTime.IsZero() {
		age := now.Sub(weatherTime)
		if age < 0 {
			age = 0
		}
		ageText = " · observed " + conciseDuration(age) + " ago"
	}
	if weather.Stale {
		ageText += " · showing the last cached report"
	}
	lines = append(lines, "🕒 "+taf+ageText)
	return strings.Join(lines, "\n")
}

func weatherColor(weather WeatherView) int {
	switch strings.ToUpper(strings.TrimSpace(weather.FlightCategory)) {
	case "VFR":
		return Radar
	case "MVFR":
		return Caution
	case "IFR", "LIFR":
		return EmergencyColor
	default:
		return Scope
	}
}

func cloudSummary(clouds []WeatherCloudView, units domain.UnitSystem) string {
	if len(clouds) == 0 {
		return "No significant cloud layer reported"
	}
	parts := make([]string, 0, min(3, len(clouds)))
	for _, cloud := range clouds {
		if len(parts) >= 3 {
			break
		}
		cover := map[string]string{"FEW": "few clouds", "SCT": "scattered clouds", "BKN": "broken ceiling", "OVC": "overcast ceiling", "VV": "vertical visibility"}[cloud.Cover]
		if cover == "" {
			cover = strings.ToLower(cloud.Cover)
		}
		if cloud.HasBase {
			if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
				cover += fmt.Sprintf(" at %.0f m", float64(cloud.BaseFeet)*0.3048)
			} else {
				cover += fmt.Sprintf(" at %s ft", commaInt(cloud.BaseFeet))
			}
		}
		parts = append(parts, cover)
	}
	return strings.Join(parts, " · ")
}

func activitySummary(activity domain.AirportActivity, units domain.UnitSystem, now time.Time) string {
	if !activity.Configured {
		return "⚪ Airport activity needs a configured public airport center."
	}
	if len(activity.Movements) == 0 {
		return "🟢 No likely arrivals or departures detected right now.\nSkyFeed waits for three compatible local ADS-B updates before showing a trend."
	}
	lines := make([]string, 0, min(4, len(activity.Movements))+1)
	for _, movement := range activity.Movements[:min(4, len(activity.Movements))] {
		lines = append(lines, movementOneLine(movement, units))
	}
	if len(activity.Movements) > 4 {
		lines = append(lines, fmt.Sprintf("…and %d more. Open **Arrivals & departures** for the full view.", len(activity.Movements)-4))
	}
	if !activity.UpdatedAt.IsZero() {
		age := now.Sub(activity.UpdatedAt)
		if age < 0 {
			age = 0
		}
		lines = append(lines, "Updated "+conciseDuration(age)+" ago")
	}
	return strings.Join(lines, "\n")
}

func activityFields(activity domain.AirportActivity, units domain.UnitSystem, now time.Time) []discord.EmbedField {
	if !activity.Configured || len(activity.Movements) == 0 {
		return []discord.EmbedField{section("No likely movements right now", activitySummary(activity, units, now))}
	}
	limit := min(10, len(activity.Movements))
	fields := make([]discord.EmbedField, 0, limit+1)
	for _, movement := range activity.Movements[:limit] {
		identity := PlainText(firstNonEmpty(movement.Callsign, movement.ICAO))
		facts := movementFacts(movement, units)
		value := facts + "\nWhy: " + PlainText(movement.Evidence) + fmt.Sprintf(" · confidence %d%%", movement.Confidence)
		if !movement.ObservedAt.IsZero() {
			value += fmt.Sprintf("\nLast seen <t:%d:R>", movement.ObservedAt.Unix())
		}
		fields = append(fields, section(movementIcon(movement.Phase)+" "+movementLabel(movement.Phase)+" • "+identity, value))
	}
	fields = append(fields, section("How SkyFeed decides", "SkyFeed checks airport distance, heading, climb/descent, radial motion, altitude, speed, ground state, and three consecutive samples. These are likely movements—not official runway or flight-status data."))
	return fields
}

func movementOneLine(movement domain.AirportMovement, units domain.UnitSystem) string {
	identity := PlainText(firstNonEmpty(movement.Callsign, movement.ICAO))
	return fmt.Sprintf("%s **%s** — %s · %s", movementIcon(movement.Phase), identity, movementLabel(movement.Phase), movementFacts(movement, units))
}

func movementFacts(movement domain.AirportMovement, units domain.UnitSystem) string {
	parts := make([]string, 0, 4)
	if movement.HasDistance {
		parts = append(parts, strings.ToLower(compassLong(movement.BearingDegrees))+" of airport at "+distance(movement.DistanceNM, units))
	}
	if movement.HasAltitude {
		aircraft := domain.Aircraft{HasAltitude: true, AltitudeFeet: movement.AltitudeFeet}
		parts = append(parts, altitudeWithUnits(aircraft, units))
	}
	if movement.HasVerticalRate {
		aircraft := domain.Aircraft{HasVerticalRate: true, VerticalRateFPM: movement.VerticalRateFPM}
		parts = append(parts, verticalRateWithUnits(aircraft, units))
	}
	if movement.HasGroundSpeed {
		aircraft := domain.Aircraft{HasGroundSpeed: true, GroundSpeedKts: movement.GroundSpeedKts}
		parts = append(parts, groundSpeedWithUnits(aircraft, units))
	}
	if len(parts) == 0 {
		return "live position details unavailable"
	}
	return strings.Join(parts, " · ")
}

func movementIcon(phase domain.MovementPhase) string {
	switch phase {
	case domain.MovementApproach:
		return "🛬"
	case domain.MovementDeparture:
		return "🛫"
	default:
		return "✅"
	}
}

func movementLabel(phase domain.MovementPhase) string {
	switch phase {
	case domain.MovementApproach:
		return "likely approaching"
	case domain.MovementDeparture:
		return "likely departing"
	default:
		return "likely landed"
	}
}

func compassLong(degrees float64) string {
	directions := [...]string{"North", "Northeast", "East", "Southeast", "South", "Southwest", "West", "Northwest"}
	index := int((degrees+22.5)/45) % len(directions)
	if index < 0 {
		index += len(directions)
	}
	return directions[index]
}

func commaInt(value int) string {
	text := strconv.Itoa(value)
	for index := len(text) - 3; index > 0; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	return text
}

func Airline(airline domain.Airline, flights []domain.Aircraft, now time.Time) discord.Embed {
	return AirlineWithUnits(airline, flights, now, domain.UnitsAviation)
}

func AirlineWithUnits(airline domain.Airline, flights []domain.Aircraft, now time.Time, units domain.UnitSystem) discord.Embed {
	code := firstNonEmpty(airline.ICAO, airline.IATA, "Airline")
	embed := base("Airline • "+PlainText(code), Scope, now)
	identity := strings.Join(nonEmpty(PlainText(airline.Name), PlainText(airline.ICAO), PlainText(airline.IATA)), " • ")
	if identity == "" {
		identity = "Live callsign prefix match • airline directory unavailable"
	}
	meta := []string{identity, fmt.Sprintf("%d visible flights", len(flights))}
	if airline.Country != "" || airline.CountryISO != "" {
		meta = append(meta, "Country "+PlainText(firstNonEmpty(airline.Country, airline.CountryISO)))
	}
	if airline.RadioCallsign != "" {
		meta = append(meta, "Radio "+PlainText(airline.RadioCallsign))
	}
	embed.Description = "✈️ **AIRLINE ACTIVITY**\n" + strings.Join(meta, "\n")
	visible := flights
	if len(visible) > 20 {
		visible = visible[:20]
	}
	embed.Fields = aircraftRowFieldsWithUnits(visible, 0, units)
	footerParts := uniqueStrings([]string{aircraftListFooter(flights), airline.Attribution})
	if len(footerParts) > 0 {
		embed.Footer = &discord.EmbedFooter{Text: Truncate(PlainText(strings.Join(footerParts, " • ")), 2048)}
	}
	return BoundEmbed(embed)
}

func LookingUp(kind, identity string, now time.Time) discord.Embed {
	return BoundEmbed(base(kind, Muted, now).WithDescription(fmt.Sprintf("Looking up **%s**…", PlainText(identity))))
}

func TrackSummary(summary trackdata.Summary, units domain.UnitSystem, now time.Time) discord.Embed {
	embed := base("Track • "+PlainText(summary.ICAO), Scope, now).
		WithDescription("🛰️ **RECENT TRACK**\nMemory-only samples from the last 15 minutes.")
	window := fmt.Sprintf("%d points", summary.Points)
	if !summary.From.IsZero() && !summary.To.IsZero() {
		window = fmt.Sprintf("%d points · <t:%d:T>–<t:%d:T>", summary.Points, summary.From.Unix(), summary.To.Unix())
	}
	closest := "Unavailable"
	if summary.HasClosestApproach {
		closest = distance(summary.ClosestApproachNM, units)
	}
	altitudeChange := "Unavailable"
	if summary.HasAltitudeChange {
		if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
			altitudeChange = fmt.Sprintf("%+.0f m", float64(summary.AltitudeChangeFeet)*0.3048)
		} else {
			altitudeChange = fmt.Sprintf("%+d ft", summary.AltitudeChangeFeet)
		}
	}
	embed.Fields = []discord.EmbedField{
		section("📈 Track summary", Facts(window, "Closest "+closest, "Altitude "+altitudeChange)),
		section("🧭 Direction", PlainText(summary.Direction)),
	}
	embed.Footer = &discord.EmbedFooter{Text: "Memory only • retained up to 15 minutes • not authoritative navigation data"}
	return BoundEmbed(embed)
}

func Emergency(aircraft []domain.Aircraft, page, pageSize int, now time.Time) discord.Embed {
	return EmergencyWithUnits(aircraft, page, pageSize, now, domain.UnitsAviation)
}

func EmergencyWithUnits(aircraft []domain.Aircraft, page, pageSize int, now time.Time, units domain.UnitSystem) discord.Embed {
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	copyAircraft := append([]domain.Aircraft(nil), aircraft...)
	start, end, page, maxPage := PageBounds(len(copyAircraft), page, pageSize)
	embed := base("Emergency", EmergencyColor, now)
	if len(copyAircraft) == 0 {
		embed.Description = "No emergency squawks or emergency flags are currently visible."
		embed.Color = Radar
		return BoundEmbed(embed)
	}
	embed.Description = "🔴 **ACTIVE EMERGENCIES**\n7500, 7600, 7700, or an emergency flag\n" + PageDescription("aircraft", len(copyAircraft), start, end, page, maxPage)
	embed.Fields = emergencyRowFieldsWithUnits(copyAircraft[start:end], start, units)
	embed.Footer = &discord.EmbedFooter{Text: aircraftListFooter(copyAircraft)}
	return BoundEmbed(embed)
}

func Traffic(aircraft []domain.Aircraft, airportCode string, radiusNM float64, page, pageSize int, now time.Time) discord.Embed {
	return TrafficWithUnits(aircraft, airportCode, radiusNM, page, pageSize, now, domain.UnitsAviation)
}

func TrafficWithUnits(aircraft []domain.Aircraft, airportCode string, radiusNM float64, page, pageSize int, now time.Time, units domain.UnitSystem) discord.Embed {
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	copyAircraft := append([]domain.Aircraft(nil), aircraft...)
	start, end, page, maxPage := PageBounds(len(copyAircraft), page, pageSize)
	label := PlainText(valueOr(airportCode, "public airport"))
	embed := base("Traffic • "+label, Scope, now)
	if len(copyAircraft) == 0 {
		embed.Description = fmt.Sprintf("No visible aircraft are currently within %s of %s.", distance(radiusNM, units), label)
		return BoundEmbed(embed)
	}
	embed.Description = fmt.Sprintf("📍 **NEAR %s**\nWithin %s\n%s", label, distance(radiusNM, units), PageDescription("aircraft", len(copyAircraft), start, end, page, maxPage))
	embed.Fields = aircraftRowFieldsWithUnits(copyAircraft[start:end], start, units)
	embed.Footer = &discord.EmbedFooter{Text: aircraftListFooter(copyAircraft)}
	return BoundEmbed(embed)
}

func Squawk(aircraft []domain.Aircraft, code string, page, pageSize int, now time.Time) discord.Embed {
	return SquawkWithUnits(aircraft, code, page, pageSize, now, domain.UnitsAviation)
}

func SquawkWithUnits(aircraft []domain.Aircraft, code string, page, pageSize int, now time.Time, units domain.UnitSystem) discord.Embed {
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	copyAircraft := append([]domain.Aircraft(nil), aircraft...)
	start, end, page, maxPage := PageBounds(len(copyAircraft), page, pageSize)
	embed := base("Squawk • "+PlainText(code), Scope, now)
	if len(copyAircraft) == 0 {
		embed.Description = squawkMeaning(code) + "\nNo current aircraft match this squawk."
		return BoundEmbed(embed)
	}
	embed.Description = "📡 **TRANSPONDER MATCH**\n" + squawkMeaning(code) + "\n" + PageDescription("aircraft", len(copyAircraft), start, end, page, maxPage)
	embed.Fields = aircraftRowFieldsWithUnits(copyAircraft[start:end], start, units)
	embed.Footer = &discord.EmbedFooter{Text: aircraftListFooter(copyAircraft)}
	return BoundEmbed(embed)
}

func Top(aircraft []domain.Aircraft, metric string, limit int, now time.Time) discord.Embed {
	return TopWithUnits(aircraft, metric, limit, now, domain.UnitsAviation)
}

func TopWithUnits(aircraft []domain.Aircraft, metric string, limit int, now time.Time, units domain.UnitSystem) discord.Embed {
	embed := base("Top aircraft • "+metricLabel(metric), Scope, now)
	if len(aircraft) == 0 {
		embed.Description = "No current aircraft are available for this ranking."
		return BoundEmbed(embed)
	}
	embed.Description = fmt.Sprintf("Top %d by %s", min(limit, len(aircraft)), metricLabel(metric))
	fields := make([]discord.EmbedField, 0, len(aircraft))
	for index, item := range aircraft {
		name := fmt.Sprintf("%d. %s", index+1, PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO)))
		value := Facts(
			"`"+PlainText(item.ICAO)+"`",
			Labeled(metricLabel(metric), metricValueWithUnits(item, metric, units)),
			Labeled("Altitude", altitudeWithUnits(item, units)),
			Labeled("Speed", groundSpeedWithUnits(item, units)),
		)
		fields = append(fields, section(name, value))
	}
	embed.Fields = fields
	embed.Footer = &discord.EmbedFooter{Text: aircraftListFooter(aircraft)}
	return BoundEmbed(embed)
}

func TopRouteRankings(metric, period string, rows []storage.RouteRankingRow, limit int, now time.Time) discord.Embed {
	title := "Top " + routeRankingTitle(metric)
	embed := base(title, Scope, now)
	embed.Description = fmt.Sprintf("%s · %s", routeRankingTitle(metric), routeRankingPeriodLabel(period))
	if len(rows) == 0 {
		embed.Description = embed.Description + "\nNo ranked route traffic is recorded for this window yet."
		embed.Footer = &discord.EmbedFooter{Text: "Durable rankings use attributed adsb.lol route sightings"}
		return BoundEmbed(embed)
	}
	fields := make([]discord.EmbedField, 0, min(limit, len(rows)))
	for index, row := range rows {
		if index >= limit {
			break
		}
		name := fmt.Sprintf("%d. %s", index+1, PlainText(row.Label))
		value := fmt.Sprintf("%d route sightings", row.Count)
		if detail := strings.TrimSpace(row.Detail); detail != "" {
			value = PlainText(detail) + "\n" + value
		}
		fields = append(fields, section(name, value))
	}
	embed.Fields = fields
	embed.Footer = &discord.EmbedFooter{Text: "Durable rankings use attributed adsb.lol route sightings"}
	return BoundEmbed(embed)
}

func routeRankingTitle(metric string) string {
	switch metric {
	case "routes":
		return "routes"
	case "origin-countries":
		return "origin countries"
	case "destination-countries":
		return "destination countries"
	case "airlines":
		return "airlines"
	case "domestic-airports":
		return "domestic airports"
	case "international-airports":
		return "international airports"
	default:
		return metric
	}
}

func routeRankingPeriodLabel(period string) string {
	switch period {
	case "7d":
		return "last 7 days"
	case "30d":
		return "last 30 days"
	case "all":
		return "all time"
	default:
		return "last 24 hours"
	}
}

func Privacy(disclosure privacy.Disclosure) discord.Embed {
	embed := base("Privacy", Scope, time.Now())
	providers := "readsb only"
	if len(disclosure.Providers) > 0 {
		providers = strings.Join(disclosure.Providers, ", ")
	}
	center := "No external point-query source is configured."
	if disclosure.PublicAirportCode != "" {
		externalFallback := false
		for _, provider := range disclosure.Providers {
			if strings.EqualFold(provider, "airplanes.live") {
				externalFallback = true
				break
			}
		}
		if externalFallback {
			center = fmt.Sprintf("Airport activity and external fallback use public airport %s within %d NM (airplanes.live: 1 req/s, max 250 NM).", PlainText(disclosure.PublicAirportCode), disclosure.RadiusNM)
		} else {
			center = fmt.Sprintf("Airport activity uses published airport %s as its reference point. No external aircraft fallback is configured.", PlainText(disclosure.PublicAirportCode))
		}
	}
	embed.Description = "SkyFeed shares only the data described below. Receiver coordinates, fallback center coordinates, and private site values are not shown or durably stored; fallback coordinates are used only for configured provider requests."
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
	return AircraftWithEnrichmentAndUnits(aircraft, snapshot, enrichment, route, now, domain.UnitsAviation)
}

func AircraftWithEnrichmentAndUnits(aircraft domain.Aircraft, snapshot *domain.Snapshot, enrichment *domain.Enrichment, route *domain.Route, now time.Time, units domain.UnitSystem) discord.Embed {
	color := Scope
	alert := "None"
	if domain.EmergencyActive(aircraft) {
		color = EmergencyColor
		alert = "🔴 " + strings.ToUpper(firstNonEmpty(aircraft.Emergency, domain.SquawkMeaning(aircraft.Squawk)))
	}
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	embed := base("Aircraft • "+PlainText(identity), color, now)
	sourceLabel := "unknown"
	if aircraft.Provider.Known() {
		sourceLabel = string(aircraft.Provider)
	}
	embed.Description = fmt.Sprintf("✈️ **%s**\n`%s` · %s · %s\nSource `%s`",
		PlainText(identity),
		PlainText(aircraft.ICAO),
		PlainText(valueOr(aircraft.Registration, "Registration unknown")),
		PlainText(valueOr(aircraft.AircraftType, "Type unknown")),
		PlainText(sourceLabel),
	)
	embed.Fields = []discord.EmbedField{
		section("📍 Live position", Facts(positionWithUnits(aircraft, units), altitudeWithUnits(aircraft, units), groundSpeedWithUnits(aircraft, units))),
		section("🧭 Movement", Facts("Track "+trackWithCompass(aircraft), verticalRateWithUnits(aircraft, units))),
		section("📡 Transponder", Facts("Squawk `"+PlainText(valueOr(aircraft.Squawk, "????"))+"`", alert)),
	}
	if route != nil {
		embed.Fields = append(embed.Fields, section("🗺️ Route", routeText(*route)))
	}
	if enrichment != nil && enrichment.Found {
		if metadata := enrichment.Aircraft; metadata != nil {
			if meta := strings.Join(nonEmpty(PlainText(metadata.Manufacturer), PlainText(metadata.AircraftType), PlainText(metadata.Registration)), " · "); meta != "Unavailable" {
				embed.Fields = append(embed.Fields, section("🛩️ Aircraft", meta))
			}
			if owner := strings.Join(nonEmpty(PlainText(metadata.Owner), PlainText(metadata.OwnerCountry)), " · "); owner != "Unavailable" {
				embed.Fields = append(embed.Fields, section("🏷️ Operator", owner))
			}
			if metadata.ThumbnailURL != "" {
				embed.Thumbnail = &discord.EmbedResource{URL: metadata.ThumbnailURL}
			} else if metadata.PhotoURL != "" {
				embed.Thumbnail = &discord.EmbedResource{URL: metadata.PhotoURL}
			}
		}
		if route == nil && enrichment.Route != nil {
			embed.Fields = append(embed.Fields, section("🗺️ Route", routeText(*enrichment.Route)))
		}
	}
	embed.Footer = &discord.EmbedFooter{Text: aircraftProviderFooter(aircraft, snapshot, enrichment, route, now)}
	return BoundEmbed(embed)
}

func Nearby(aircraft []domain.Aircraft, page, pageSize int, now time.Time) discord.Embed {
	return NearbyWithUnits(aircraft, page, pageSize, now, domain.UnitsAviation)
}

func NearbyWithUnits(aircraft []domain.Aircraft, page, pageSize int, now time.Time, units domain.UnitSystem) discord.Embed {
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	copyAircraft := append([]domain.Aircraft(nil), aircraft...)
	start, end, page, maxPage := PageBounds(len(copyAircraft), page, pageSize)
	embed := base("Nearby", Scope, now)
	if len(copyAircraft) == 0 {
		embed.Description = "No current aircraft match this view."
		return BoundEmbed(embed)
	}
	embed.Description = "📡 **LIVE AIRCRAFT**\n" + PageDescription("aircraft", len(copyAircraft), start, end, page, maxPage)
	embed.Fields = aircraftRowFieldsWithUnits(copyAircraft[start:end], start, units)
	embed.Footer = &discord.EmbedFooter{Text: aircraftListFooter(copyAircraft)}
	return BoundEmbed(embed)
}

func Help(now time.Time, manageGuild bool) discord.Embed {
	embed := base("Help", Scope, now).WithDescription("👋 **WHAT WOULD YOU LIKE TO DO?**\nStart with a task below. SkyFeed shows a quick answer first and keeps deeper details behind controls.")
	embed.Fields = []discord.EmbedField{
		section("✈️ Explore aircraft", "`/nearby` `/traffic` `/aircraft` `/route` `/airport`\n`/airline` `/squawk` `/emergency` `/top live` `/top traffic`"),
		section("🔔 Alerts", "Use `/watch` for personal rules. Operators can configure server delivery with `/alerts`."),
		section("📊 Reports & health", "`/status` `/feeder` `/reports` `/privacy`"),
		section("🛡️ Moderation", "Authorized moderators can warn, timeout, kick, ban, and review case history with `/moderation`."),
		section("⚙️ Preferences", "Use `/preferences units` to choose aviation or metric values for your own views."),
	}
	if manageGuild {
		embed.Fields = append(embed.Fields, section("🔧 Administration", "Use `/settings`, `/feeders`, and `/audit` for server configuration and diagnostics. Role changes also require Manage Roles."))
	}
	return BoundEmbed(embed)
}

func freshnessLabel(age time.Duration) string {
	if age > 15*time.Second {
		return "stale"
	}
	return "live"
}

func Alert(alert domain.Alert) discord.Embed {
	color := Caution
	view := "Alert"
	priority := "NORMAL"
	if alert.Priority == domain.AlertEmergency {
		color = EmergencyColor
		view = "Emergency"
		priority = "EMERGENCY"
	}
	if alert.Title != "" {
		view = PlainText(alert.Title)
	}
	if alert.Type == domain.RuleTakeoff {
		view = "🛫 Likely departure"
		color = Radar
	} else if alert.Type == domain.RuleLanding {
		view = "✅ Likely landing"
		color = Scope
	} else if alert.Type == domain.RuleApproach {
		view = "🛬 Likely approach"
		color = Caution
	}
	description := PlainText(alert.Description)
	if alert.RouteSummary != "" {
		description = description + "\n**Route** " + PlainText(alert.RouteSummary)
	}
	embed := base(view, color, alert.ObservedAt).WithDescription(description)
	embed.Fields = []discord.EmbedField{section("Aircraft", fmt.Sprintf("`%s` · %s", PlainText(valueOr(alert.AircraftICAO, "Unknown")), PlainText(valueOr(alert.Callsign, "Unknown"))))}
	if alert.Type == domain.RuleTakeoff || alert.Type == domain.RuleLanding || alert.Type == domain.RuleApproach {
		embed.Fields = append(embed.Fields,
			section("How confident is this?", "Three consecutive local ADS-B samples matched the airport-relative movement pattern."),
			section("Friendly reminder", "This is a likely movement, not confirmation from air traffic control or the airport."),
		)
	} else {
		embed.Fields = append(embed.Fields, section("Rule", fmt.Sprintf("%s · %s", string(alert.Type), priority)))
	}
	return BoundEmbed(embed)
}

func InterestingAlert(alert domain.Alert) discord.Embed {
	description := PlainText(alert.Description)
	if alert.RouteSummary != "" {
		description = description + "\n**Route** " + PlainText(alert.RouteSummary)
	}
	embed := base("Interesting aircraft", Scope, alert.ObservedAt).WithDescription(description)
	if alert.Title != "" {
		embed = embed.WithTitle("SkyFeed • " + PlainText(alert.Title))
	}
	embed.Fields = []discord.EmbedField{
		section("Aircraft", fmt.Sprintf("`%s` · %s", PlainText(valueOr(alert.AircraftICAO, "Unknown")), PlainText(valueOr(alert.Callsign, "Unknown")))),
		section("Group", PlainText(valueOr(alert.InterestingGroup, "Unknown"))),
	}
	if alert.InterestingOperator != "" {
		embed.Fields = append(embed.Fields, section("Operator", PlainText(alert.InterestingOperator)))
	}
	if alert.InterestingTags != "" {
		embed.Fields = append(embed.Fields, section("Tags", PlainText(alert.InterestingTags)))
	}
	if alert.InterestingLink != "" {
		if _, ok := SafeHTTPSURL(alert.InterestingLink); !ok {
			embed.Fields = append(embed.Fields, section("Reference", PlainText(alert.InterestingLink)))
		}
	}
	if alert.InterestingImage != "" {
		if imageURL, ok := SafePlaneAlertImageURL(alert.InterestingImage); ok {
			embed.Thumbnail = &discord.EmbedResource{URL: imageURL}
		}
	}
	return BoundEmbed(embed)
}

func Report(summary storage.ReportSummary) discord.Embed {
	return ReportWithUnits(summary, domain.UnitsAviation)
}

func ReportWithUnits(summary storage.ReportSummary, units domain.UnitSystem) discord.Embed {
	embed := base("Report", Scope, summary.To).
		WithDescription(fmt.Sprintf("📊 **TRAFFIC SUMMARY**\n<t:%d:f> to <t:%d:f>", summary.From.Unix(), summary.To.Unix()))
	embed.Fields = []discord.EmbedField{
		section("✈️ Traffic", Facts(fmt.Sprintf("%d observations", summary.AircraftObservations), fmt.Sprintf("%d peak tracked", summary.PeakTracked), fmt.Sprintf("%d messages", summary.Messages))),
		section("📡 Range & alerts", Facts("Max "+distance(summary.MaximumRangeNM, units), fmt.Sprintf("%d emergency events", summary.EmergencyEvents))),
	}
	if !summary.PeakHour.IsZero() {
		embed.Fields = append(embed.Fields, section("🕒 Busiest hour", Facts(fmt.Sprintf("<t:%d:f>", summary.PeakHour.Unix()), fmt.Sprintf("%d peak tracked", summary.PeakAircraft))))
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

func positionWithUnits(aircraft domain.Aircraft, units domain.UnitSystem) string {
	if !aircraft.HasDistance {
		return "Position unavailable"
	}
	return fmt.Sprintf("%s %s • %03.0f°", compass(aircraft.BearingDegrees), distance(aircraft.DistanceNM, units), aircraft.BearingDegrees)
}

func altitudeWithUnits(aircraft domain.Aircraft, units domain.UnitSystem) string {
	if aircraft.OnGround {
		return "Ground"
	}
	if !aircraft.HasAltitude {
		return "Unknown"
	}
	if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
		return fmt.Sprintf("%d m", int(float64(aircraft.AltitudeFeet)*0.3048))
	}
	return fmt.Sprintf("%d ft", aircraft.AltitudeFeet)
}

func groundSpeedWithUnits(aircraft domain.Aircraft, units domain.UnitSystem) string {
	if !aircraft.HasGroundSpeed {
		return "Unknown"
	}
	if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
		return fmt.Sprintf("%.0f km/h", aircraft.GroundSpeedKts*1.852)
	}
	return fmt.Sprintf("%.0f kt", aircraft.GroundSpeedKts)
}

func trackWithCompass(aircraft domain.Aircraft) string {
	if !aircraft.HasTrack {
		return "Unknown"
	}
	return fmt.Sprintf("%s %03.0f°", compass(aircraft.TrackDegrees), aircraft.TrackDegrees)
}

func verticalRateWithUnits(aircraft domain.Aircraft, units domain.UnitSystem) string {
	if !aircraft.HasVerticalRate {
		return "Unknown"
	}
	arrow := "→"
	if aircraft.VerticalRateFPM > 64 {
		arrow = "↑"
	} else if aircraft.VerticalRateFPM < -64 {
		arrow = "↓"
	}
	if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
		return fmt.Sprintf("%s %+.1f m/s", arrow, float64(aircraft.VerticalRateFPM)*0.00508)
	}
	return fmt.Sprintf("%s %+d ft/min", arrow, aircraft.VerticalRateFPM)
}

func distance(valueNM float64, units domain.UnitSystem) string {
	if domain.NormalizeUnitSystem(string(units)) == domain.UnitsMetric {
		return fmt.Sprintf("%.1f km", valueNM*1.852)
	}
	return fmt.Sprintf("%.1f NM", valueNM)
}

func compass(degrees float64) string {
	directions := [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	index := int((degrees+22.5)/45) % len(directions)
	if index < 0 {
		index += len(directions)
	}
	return directions[index]
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
	return PlainText(code) + " — " + domain.SquawkMeaning(code)
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

func metricValueWithUnits(aircraft domain.Aircraft, metric string, units domain.UnitSystem) string {
	switch metric {
	case "altitude":
		return altitudeWithUnits(aircraft, units)
	case "speed":
		return groundSpeedWithUnits(aircraft, units)
	case "messages":
		return fmt.Sprintf("%d msgs", aircraft.Messages)
	case "signal":
		if !aircraft.HasRSSI {
			return "Unknown"
		}
		return fmt.Sprintf("%.1f dBm", aircraft.RSSI)
	default:
		return positionWithUnits(aircraft, units)
	}
}

func section(name, value string) discord.EmbedField {
	return discord.EmbedField{Name: name, Value: value, Inline: ptr(false)}
}

func aircraftRowFieldsWithUnits(aircraft []domain.Aircraft, startIndex int, units domain.UnitSystem) []discord.EmbedField {
	fields := make([]discord.EmbedField, 0, len(aircraft))
	for index, item := range aircraft {
		name := fmt.Sprintf("%d. %s", startIndex+index+1, PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO)))
		freshness := "live"
		if item.Seen > 15*time.Second {
			freshness = "stale"
		}
		value := Facts("`"+PlainText(item.ICAO)+"`", positionWithUnits(item, units)) + "\n" +
			Facts(altitudeWithUnits(item, units), groundSpeedWithUnits(item, units), verticalRateWithUnits(item, units)) + "\n" +
			Facts("Age "+conciseDuration(item.Seen), strings.ToUpper(freshness))
		fields = append(fields, section(name, value))
	}
	return fields
}

func moderationStatusIcon(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "complete", "completed":
		return "🟢"
	case "failed":
		return "🔴"
	case "pending":
		return "🟡"
	default:
		return "⚪"
	}
}

func emergencyRowFieldsWithUnits(aircraft []domain.Aircraft, startIndex int, units domain.UnitSystem) []discord.EmbedField {
	fields := make([]discord.EmbedField, 0, len(aircraft))
	for index, item := range aircraft {
		name := fmt.Sprintf("%d. %s", startIndex+index+1, PlainText(firstNonEmpty(item.Callsign, item.Registration, item.ICAO)))
		detail := squawkMeaning(item.Squawk)
		if item.Emergency != "" && item.Emergency != "none" {
			detail = strings.ToUpper(item.Emergency)
		}
		value := fmt.Sprintf("`%s` · squawk `%s` · %s\n%s · %s · age %s", PlainText(item.ICAO), PlainText(valueOr(item.Squawk, "????")), detail, positionWithUnits(item, units), altitudeWithUnits(item, units), conciseDuration(item.Seen))
		fields = append(fields, section(name, value))
	}
	return fields
}

func providerFooter(snapshot *domain.Snapshot, enrichment *domain.Enrichment, route *domain.Route, now time.Time) string {
	parts := make([]string, 0, 5)
	if snapshot != nil && snapshot.ActiveProvider.Known() {
		parts = append(parts, "live "+string(snapshot.ActiveProvider))
	} else {
		parts = append(parts, "live provider unknown")
	}
	if snapshot != nil && !snapshot.FetchedAt.IsZero() {
		age := now.Sub(snapshot.FetchedAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, "snapshot age "+conciseDuration(age))
	}
	if enrichment != nil && enrichment.Found {
		label := "ADSBDB enrichment"
		if enrichment.Stale {
			label += " stale"
		}
		if !enrichment.FetchedAt.IsZero() {
			age := now.Sub(enrichment.FetchedAt)
			if age < 0 {
				age = 0
			}
			label += " age " + conciseDuration(age)
		}
		parts = append(parts, label)
	}
	selectedRoute := route
	if selectedRoute == nil && enrichment != nil {
		selectedRoute = enrichment.Route
	}
	if selectedRoute != nil {
		if selectedRoute.Source.Known() {
			parts = append(parts, "route "+string(selectedRoute.Source))
		}
		if selectedRoute.Attribution != "" {
			parts = append(parts, PlainText(selectedRoute.Attribution))
		}
	}
	return Truncate(strings.Join(uniqueStrings(parts), " • "), 2048)
}

func aircraftProviderFooter(aircraft domain.Aircraft, snapshot *domain.Snapshot, enrichment *domain.Enrichment, route *domain.Route, now time.Time) string {
	if snapshot != nil && snapshot.ActiveProvider.Known() {
		return providerFooter(snapshot, enrichment, route, now)
	}
	fallback := domain.Snapshot{}
	if snapshot != nil {
		fallback = *snapshot
	}
	if aircraft.Provider.Known() {
		fallback.ActiveProvider = aircraft.Provider
	}
	if fallback.FetchedAt.IsZero() && aircraft.Seen >= 0 {
		fallback.FetchedAt = now.Add(-aircraft.Seen)
	}
	return providerFooter(&fallback, enrichment, route, now)
}

func aircraftListFooter(aircraft []domain.Aircraft) string {
	providers := make([]string, 0, 2)
	maxAge := time.Duration(0)
	for _, item := range aircraft {
		if item.Provider.Known() {
			providers = append(providers, string(item.Provider))
		}
		if item.Seen > maxAge {
			maxAge = item.Seen
		}
	}
	providers = uniqueStrings(providers)
	providerText := "live provider unknown"
	if len(providers) > 0 {
		providerText = "live " + strings.Join(providers, ", ")
	}
	return providerText + " • observations up to " + conciseDuration(maxAge) + " old"
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func ptr[T any](value T) *T { return &value }
