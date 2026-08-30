package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/report"
)

func FlightLeaders(snapshot *domain.Snapshot, leaders report.LiveLeaders, units domain.UnitSystem, now time.Time) discord.Embed {
	if snapshot == nil {
		return BoundEmbed(base("Live flight leaders", Muted, now).WithDescription("⚪ **WAITING**\nNo aircraft snapshot is available yet."))
	}
	status := snapshot.Health.Aircraft.Status
	color := Radar
	if status == domain.HealthOffline {
		color = EmergencyColor
	} else if status == domain.HealthStale || status == domain.HealthDegraded {
		color = Caution
	} else if status == domain.HealthUnknown || status == domain.HealthDisabled {
		color = Muted
	}
	description := fmt.Sprintf("%s **%s**\nFresh airborne leaders across all approved feeders.", badge(status), strings.ToUpper(string(status)))
	if leaders.Eligible == 0 {
		description += "\nNo aircraft currently meet the 15-second freshness window."
	}
	embed := base("Live flight leaders", color, snapshot.PublishedAt).WithDescription(description)
	embed.Fields = []discord.EmbedField{
		leaderSection("🚀 Fastest aircraft", leaders.Fastest, "speed", units),
		leaderSection("🪶 Slowest aircraft", leaders.Slowest, "speed", units),
		leaderSection("⬆️ Highest aircraft", leaders.Highest, "altitude", units),
		leaderSection("⬇️ Lowest aircraft", leaders.Lowest, "altitude", units),
	}
	embed.Footer = &discord.EmbedFooter{Text: flightLeaderFooter(snapshot, leaders)}
	return BoundEmbed(embed)
}

func flightLeaderFooter(snapshot *domain.Snapshot, leaders report.LiveLeaders) string {
	providers := make([]string, 0, 4)
	maximumAge := time.Duration(0)
	for _, leader := range []report.AircraftLeader{leaders.Fastest, leaders.Slowest, leaders.Highest, leaders.Lowest} {
		if !leader.Found {
			continue
		}
		if leader.Aircraft.Provider.Known() {
			providers = append(providers, string(leader.Aircraft.Provider))
		}
		if leader.Age > maximumAge {
			maximumAge = leader.Age
		}
	}
	providers = uniqueStrings(providers)
	providerText := "live provider unknown"
	if len(providers) > 0 {
		providerText = "live " + strings.Join(providers, ", ")
	} else if snapshot != nil && snapshot.ActiveProvider.Known() {
		providerText = "live " + string(snapshot.ActiveProvider)
	}
	parts := []string{providerText}
	if snapshot != nil && snapshot.FeederID == domain.FeederAll {
		parts = append(parts, "community aggregate")
	}
	if leaders.Eligible > 0 {
		parts = append(parts, "observations up to "+conciseDuration(maximumAge)+" old")
	}
	return strings.Join(parts, " • ")
}

func leaderSection(title string, leader report.AircraftLeader, metric string, units domain.UnitSystem) discord.EmbedField {
	if !leader.Found {
		return section(title, "No fresh airborne aircraft with this measurement.")
	}
	aircraft := leader.Aircraft
	identity := firstNonEmpty(aircraft.Callsign, aircraft.Registration, aircraft.ICAO)
	value := groundSpeedWithUnits(aircraft, units)
	context := "Altitude " + altitudeWithUnits(aircraft, units)
	if metric == "altitude" {
		value = altitudeWithUnits(aircraft, units)
		context = "Speed " + groundSpeedWithUnits(aircraft, units)
	}
	seenBy := "One feeder"
	if len(aircraft.SeenBy) > 1 {
		seenBy = fmt.Sprintf("%d feeders", len(aircraft.SeenBy))
	}
	metricTitle := "Speed"
	if metric == "altitude" {
		metricTitle = "Altitude"
	}
	return section(title, Facts(
		"**"+PlainText(identity)+"** · `"+PlainText(aircraft.ICAO)+"`",
		Labeled(metricTitle, value),
		context,
		seenBy,
		fmt.Sprintf("Observed %.1fs ago", leader.Age.Seconds()),
	))
}
