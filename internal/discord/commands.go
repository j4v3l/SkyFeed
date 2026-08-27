package discord

import (
	"fmt"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/omit"
)

const CommandSchemaVersion = 15

const LookupAircraftCommand = "Lookup aircraft"

// ownedCommandNames is permanent command ownership history. When a command is
// removed from DesiredCommands, leave its name here as a deletion tombstone so
// synchronization can remove the stale remote command without touching names
// owned by another application feature.
var ownedCommandNames = map[string]struct{}{
	"status": {}, "nearby": {}, "aircraft": {}, "route": {}, "airport": {}, "squawk": {}, "emergency": {}, "traffic": {}, "top": {}, "privacy": {},
	"watch": {}, "alerts": {}, "reports": {}, "audit": {}, "feeder": {}, "settings": {}, "preferences": {}, "help": {},
	"moderation": {}, "airline": {}, LookupAircraftCommand: {},
	"feeders": {},
}

// Discord command-picker visibility uses native permission bits only (not custom
// SkyFeed roles). Keep these aligned with scripts/setup-discord-governance.py:
//
//	Viewer   — no DefaultMemberPermissions (everyone)
//	Operator — Manage Server (server watch / alert configure / report schedule are runtime-gated)
//	Moderator — Moderate Members (+ Kick/Ban as needed at runtime)
//	Admin    — Manage Server for /settings; role bind/remove additionally require Manage Roles at runtime
//
// Discord Administrators always see every command. Bot DMs ignore these bits;
// DM use is still Admin-only at runtime.
func DesiredCommands() []disgocord.ApplicationCommandCreate {
	minRadius, maxRadius := 1.0, 250.0
	minAltitude, maxAltitude := -2_000, 100_000
	minLimit, maxLimit := 1, 25
	operator := disgocord.Permissions(disgocord.PermissionManageGuild)
	moderator := disgocord.Permissions(disgocord.PermissionModerateMembers)
	commands := []disgocord.ApplicationCommandCreate{
		disgocord.SlashCommandCreate{Name: "status", Description: "Show receiver, source, and SkyFeed health", Options: []disgocord.ApplicationCommandOption{feederOption()}},
		disgocord.SlashCommandCreate{
			Name: "nearby", Description: "Browse aircraft currently visible to this receiver",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionFloat{Name: "radius-nm", Description: "Maximum receiver distance in nautical miles", MinValue: &minRadius, MaxValue: &maxRadius},
				disgocord.ApplicationCommandOptionInt{Name: "altitude-min", Description: "Minimum pressure altitude in feet", MinValue: &minAltitude, MaxValue: &maxAltitude},
				disgocord.ApplicationCommandOptionInt{Name: "altitude-max", Description: "Maximum pressure altitude in feet", MinValue: &minAltitude, MaxValue: &maxAltitude},
				disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of aircraft per page", MinValue: &minLimit, MaxValue: &maxLimit},
				disgocord.ApplicationCommandOptionString{Name: "sort", Description: "Aircraft ordering", Choices: []disgocord.ApplicationCommandOptionChoiceString{
					{Name: "Distance", Value: "distance"}, {Name: "Altitude", Value: "altitude"}, {Name: "Callsign", Value: "callsign"},
					{Name: "Ground speed", Value: "speed"}, {Name: "Messages", Value: "messages"},
				}},
				disgocord.ApplicationCommandOptionString{Name: "squawk", Description: "Filter by transponder code", MinLength: intPtr(4), MaxLength: intPtr(4)},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "aircraft", Description: "Show one currently visible aircraft",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "query", Description: "ICAO, registration, or callsign", Required: true, Autocomplete: true},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "route", Description: "Show the filed route for a visible aircraft",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "flight", Description: "Visible aircraft callsign or ICAO", Required: true, Autocomplete: true},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "airport", Description: "Show airport weather, activity, and details",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "code", Description: "ICAO airport code", Required: true, Autocomplete: true},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "squawk", Description: "List visible aircraft matching a transponder code",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "code", Description: "Four-digit squawk code (0–7)", Required: true, MinLength: intPtr(4), MaxLength: intPtr(4)},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "emergency", Description: "Browse currently visible emergency squawks and flags",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of aircraft per page", MinValue: &minLimit, MaxValue: &maxLimit},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "traffic", Description: "Browse aircraft near the configured public airport center",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionFloat{Name: "radius-nm", Description: "Maximum distance from the public airport area in nautical miles", MinValue: &minRadius, MaxValue: &maxRadius},
				disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of aircraft per page", MinValue: &minLimit, MaxValue: &maxLimit},
				feederOption(),
			},
		},
		disgocord.SlashCommandCreate{
			Name: "top", Description: "Show live aircraft leaders or historical traffic rankings",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionSubCommand{Name: "live", Description: "Rank aircraft visible right now", Options: []disgocord.ApplicationCommandOption{
					disgocord.ApplicationCommandOptionString{Name: "metric", Description: "Live ranking metric", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
						{Name: "Distance", Value: "distance"}, {Name: "Altitude", Value: "altitude"}, {Name: "Ground speed", Value: "speed"}, {Name: "Messages", Value: "messages"}, {Name: "Signal", Value: "signal"},
					}},
					disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of aircraft to show", MinValue: &minLimit, MaxValue: &maxLimit},
					feederOption(),
				}},
				disgocord.ApplicationCommandOptionSubCommand{Name: "traffic", Description: "Rank attributed route sightings over time", Options: []disgocord.ApplicationCommandOption{
					disgocord.ApplicationCommandOptionString{Name: "metric", Description: "Traffic ranking metric", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
						{Name: "Routes", Value: "routes"}, {Name: "Origin countries", Value: "origin-countries"}, {Name: "Destination countries", Value: "destination-countries"}, {Name: "Airlines", Value: "airlines"}, {Name: "Domestic airports", Value: "domestic-airports"}, {Name: "International airports", Value: "international-airports"},
					}},
					disgocord.ApplicationCommandOptionString{Name: "period", Description: "Traffic ranking window", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
						{Name: "Last 24 hours", Value: "24h"}, {Name: "Last 7 days", Value: "7d"}, {Name: "Last 30 days", Value: "30d"}, {Name: "All time", Value: "all"},
					}},
					disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of rows to show", MinValue: &minLimit, MaxValue: &maxLimit},
					feederOption(),
				}},
			},
		},
		disgocord.SlashCommandCreate{
			Name: "airline", Description: "Look up an airline and currently visible flights",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "code", Description: "Airline ICAO or IATA code", Required: true, Autocomplete: true, MinLength: intPtr(2), MaxLength: intPtr(3)},
			},
		},
		disgocord.SlashCommandCreate{Name: "privacy", Description: "Show how SkyFeed shares provider data in this server"},
		disgocord.SlashCommandCreate{Name: "preferences", Description: "Configure your personal SkyFeed display", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionSubCommand{Name: "units", Description: "Choose your preferred units", Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "system", Description: "Unit system", Required: true, Choices: unitChoices()},
			}},
		}},
		disgocord.SlashCommandCreate{Name: "watch", Description: "Manage personal or server aircraft watch rules", Options: watchOptions()},
		disgocord.SlashCommandCreate{
			Name: "alerts", Description: "View or configure alert delivery", DefaultMemberPermissions: omit.NewPtr(operator),
			Options: alertsOptions(),
		},
		disgocord.SlashCommandCreate{
			Name: "reports", Description: "Generate reports or manage schedules", DefaultMemberPermissions: omit.NewPtr(operator),
			Options: reportOptions(),
		},
		disgocord.SlashCommandCreate{
			Name: "audit", Description: "Admin-only full system health and configuration audit", DefaultMemberPermissions: omit.NewPtr(operator),
		},
		disgocord.SlashCommandCreate{Name: "feeder", Description: "Show receiver, statistics, range, and source diagnostics", Options: []disgocord.ApplicationCommandOption{feederOption()}},
		disgocord.SlashCommandCreate{Name: "feeders", Description: "Browse or administer approved community feeders", Options: feederAdminOptions()},
		disgocord.SlashCommandCreate{
			Name: "moderation", Description: "Moderate server members with durable private case records", DefaultMemberPermissions: omit.NewPtr(moderator),
			Options: moderationOptions(),
		},
		disgocord.SlashCommandCreate{
			Name: "settings", Description: "Configure SkyFeed for this server", DefaultMemberPermissions: omit.NewPtr(operator),
			Options: settingsOptions(),
		},
		disgocord.SlashCommandCreate{Name: "help", Description: "Show a permission-aware SkyFeed task guide"},
	}
	for index, command := range commands {
		slash := command.(disgocord.SlashCommandCreate)
		slash.IntegrationTypes = []disgocord.ApplicationIntegrationType{disgocord.ApplicationIntegrationTypeGuildInstall}
		slash.Contexts = guildAndBotDMContextList()
		commands[index] = slash
	}
	commands = append(commands, disgocord.MessageCommandCreate{
		Name:             LookupAircraftCommand,
		IntegrationTypes: []disgocord.ApplicationIntegrationType{disgocord.ApplicationIntegrationTypeGuildInstall},
		Contexts:         guildAndBotDMContextList(),
	})
	return commands
}

func OwnedCommand(name string) bool {
	_, ok := ownedCommandNames[name]
	return ok
}

func validateDesiredCommands(commands []disgocord.ApplicationCommandCreate) error {
	seen := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		name := command.CommandName()
		if !OwnedCommand(name) {
			return fmt.Errorf("command %q is not in the SkyFeed ownership set", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate command %q", name)
		}
		if err := validateCommandInstallScope(command); err != nil {
			return err
		}
		seen[name] = struct{}{}
	}
	return nil
}

func watchOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "add", Description: "Add a watch rule", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "kind", Description: "Rule type", Required: true, Choices: ruleKindChoices()},
			disgocord.ApplicationCommandOptionString{Name: "value", Description: "Normalized rule value", Required: true, MinLength: intPtr(1), MaxLength: intPtr(64)},
			disgocord.ApplicationCommandOptionBool{Name: "server", Description: "Create a server rule instead of a personal rule"},
			feederOption(),
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "remove", Description: "Remove a saved watch rule", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "rule", Description: "Saved rule", Required: true, Autocomplete: true},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "enable", Description: "Enable a saved watch rule", Options: ruleChoiceOption()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "disable", Description: "Disable a saved watch rule", Options: ruleChoiceOption()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "list", Description: "List watch rules visible to you", Options: []disgocord.ApplicationCommandOption{feederOption()}},
	}
}

func alertsOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "view", Description: "View alert categories and cooldowns"},
		disgocord.ApplicationCommandOptionSubCommand{Name: "configure", Description: "Configure an alert category", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "category", Description: "Alert category", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Watch rules", Value: "watch"}, {Name: "Emergencies", Value: "emergency"}, {Name: "Feeder health", Value: "feeder"}, {Name: "Interesting aircraft", Value: "interesting"}, {Name: "Movements", Value: "movements"},
			}},
			disgocord.ApplicationCommandOptionBool{Name: "enabled", Description: "Whether this category is enabled", Required: true},
			disgocord.ApplicationCommandOptionInt{Name: "cooldown-minutes", Description: "Minimum time between duplicate alerts", MinValue: intPtr(0), MaxValue: intPtr(1440)},
			disgocord.ApplicationCommandOptionChannel{Name: "destination", Description: "Destination channel"},
		}},
	}
}

func reportOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "generate", Description: "Generate a bounded period report", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "period", Description: "Report period", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Last hour", Value: "1h"}, {Name: "Last day", Value: "24h"}, {Name: "Last 7 days", Value: "168h"},
			}},
			feederOption(),
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "schedule", Description: "Create or update a report schedule", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "cadence", Description: "Schedule cadence", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Daily", Value: "daily"}, {Name: "Weekly", Value: "weekly"},
			}},
			disgocord.ApplicationCommandOptionChannel{Name: "destination", Description: "Report channel", Required: true},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "list", Description: "List scheduled reports"},
	}
}

func feederOption() disgocord.ApplicationCommandOptionString {
	return disgocord.ApplicationCommandOptionString{
		Name: "feeder", Description: "Approved feeder (defaults to All feeders)", Autocomplete: true,
		MinLength: intPtr(1), MaxLength: intPtr(48),
	}
}

func feederAdminOptions() []disgocord.ApplicationCommandOption {
	latitudeMin, latitudeMax := -90.0, 90.0
	longitudeMin, longitudeMax := -180.0, 180.0
	id := func() disgocord.ApplicationCommandOptionString {
		return disgocord.ApplicationCommandOptionString{Name: "feeder", Description: "Approved feeder", Required: true, Autocomplete: true, MinLength: intPtr(1), MaxLength: intPtr(48)}
	}
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "list", Description: "List approved public feeder summaries"},
		disgocord.ApplicationCommandOptionSubCommand{Name: "show", Description: "Show one approved feeder", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "invite", Description: "Create a private 15-minute agent invitation", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "name", Description: "Public display name", Required: true, MinLength: intPtr(1), MaxLength: intPtr(80)},
			disgocord.ApplicationCommandOptionString{Name: "area", Description: "Approved public airport or area", Required: true, MinLength: intPtr(1), MaxLength: intPtr(80)},
			disgocord.ApplicationCommandOptionString{Name: "airport", Description: "Public four-character ICAO airport code", MinLength: intPtr(4), MaxLength: intPtr(4)},
			disgocord.ApplicationCommandOptionUser{Name: "owner", Description: "Private Discord owner record"},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "rename", Description: "Change a feeder's public name", Options: []disgocord.ApplicationCommandOption{
			id(), disgocord.ApplicationCommandOptionString{Name: "name", Description: "New public display name", Required: true, MinLength: intPtr(1), MaxLength: intPtr(80)},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "set-area", Description: "Set the approved public airport area and coordinates", Options: []disgocord.ApplicationCommandOption{
			id(),
			disgocord.ApplicationCommandOptionString{Name: "area", Description: "Approved public airport or area", Required: true, MinLength: intPtr(1), MaxLength: intPtr(80)},
			disgocord.ApplicationCommandOptionString{Name: "airport", Description: "Public four-character ICAO airport code", Required: true, MinLength: intPtr(4), MaxLength: intPtr(4)},
			disgocord.ApplicationCommandOptionFloat{Name: "latitude", Description: "Approved public airport latitude", Required: true, MinValue: &latitudeMin, MaxValue: &latitudeMax},
			disgocord.ApplicationCommandOptionFloat{Name: "longitude", Description: "Approved public airport longitude", Required: true, MinValue: &longitudeMin, MaxValue: &longitudeMax},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "set-weather-station", Description: "Override the METAR reporting station", Options: []disgocord.ApplicationCommandOption{
			id(), disgocord.ApplicationCommandOptionString{Name: "station", Description: "Four-character reporting station ICAO", Required: true, MinLength: intPtr(4), MaxLength: intPtr(4)},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "enable", Description: "Enable an enrolled feeder", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "disable", Description: "Pause a feeder without revoking its key", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "set-default", Description: "Set the server's default feeder view", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "rotate", Description: "Revoke the old key and create a new invitation", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "revoke", Description: "Revoke an agent and disable its feeder", Options: []disgocord.ApplicationCommandOption{id()}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "test", Description: "Show private enrollment and delivery diagnostics", Options: []disgocord.ApplicationCommandOption{id()}},
	}
}

func settingsOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "units", Description: "Set the server default units", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "system", Description: "Default unit system", Required: true, Choices: unitChoices()},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "channels", Description: "Configure a durable channel binding", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "purpose", Description: "Channel purpose", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Live dashboard", Value: "live"}, {Name: "Alerts", Value: "alerts"}, {Name: "Emergencies", Value: "emergencies"}, {Name: "Interesting aircraft", Value: "interesting"}, {Name: "Reports", Value: "reports"}, {Name: "Administration", Value: "admin"}, {Name: "Moderation log", Value: "moderation"},
			}},
			disgocord.ApplicationCommandOptionChannel{Name: "channel", Description: "Discord channel", Required: true},
		}},
		disgocord.ApplicationCommandOptionSubCommandGroup{Name: "roles", Description: "Bind existing roles to SkyFeed access tiers", Options: []disgocord.ApplicationCommandOptionSubCommand{
			{Name: "bind", Description: "Bind an existing role to an access tier", Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "tier", Description: "SkyFeed access tier", Required: true, Choices: roleTierChoices()},
				disgocord.ApplicationCommandOptionRole{Name: "role", Description: "Existing Discord role", Required: true},
			}},
			{Name: "remove", Description: "Remove an access-tier role binding", Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "tier", Description: "SkyFeed access tier", Required: true, Choices: roleTierChoices()},
			}},
			{Name: "list", Description: "List configured access-tier roles"},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "test", Description: "Test a configured destination", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "purpose", Description: "Destination purpose", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Alerts", Value: "alerts"}, {Name: "Emergencies", Value: "emergencies"}, {Name: "Interesting aircraft", Value: "interesting"}, {Name: "Reports", Value: "reports"}, {Name: "Administration", Value: "admin"},
			}},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "pause-alerts", Description: "Temporarily pause all non-emergency alert delivery"},
		disgocord.ApplicationCommandOptionSubCommand{Name: "resume-alerts", Description: "Resume alert delivery after a pause"},
		disgocord.ApplicationCommandOptionSubCommand{Name: "mute-squawk", Description: "Mute alert delivery for a squawk code", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "code", Description: "Four-digit squawk code (0–7)", Required: true, MinLength: intPtr(4), MaxLength: intPtr(4)},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "unmute-squawk", Description: "Remove a muted squawk code", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "code", Description: "Four-digit squawk code (0–7)", Required: true, MinLength: intPtr(4), MaxLength: intPtr(4)},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "recreate-dashboard", Description: "Clear the live dashboard binding so SkyFeed posts a fresh message"},
	}
}

func unitChoices() []disgocord.ApplicationCommandOptionChoiceString {
	return []disgocord.ApplicationCommandOptionChoiceString{
		{Name: "Aviation (NM, ft, kt)", Value: "aviation"},
		{Name: "Metric (km, m, km/h)", Value: "metric"},
	}
}

func moderationOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "warn", Description: "Record a warning and deliver it by private message", Options: moderationTargetReasonOptions()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "timeout", Description: "Temporarily prevent a member from interacting", Options: append(moderationTargetReasonOptions(),
			disgocord.ApplicationCommandOptionString{Name: "duration", Description: "Timeout duration", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "5 minutes", Value: "5m"}, {Name: "15 minutes", Value: "15m"}, {Name: "1 hour", Value: "1h"}, {Name: "6 hours", Value: "6h"}, {Name: "1 day", Value: "24h"}, {Name: "7 days", Value: "168h"}, {Name: "28 days", Value: "672h"},
			}},
		)},
		disgocord.ApplicationCommandOptionSubCommand{Name: "remove-timeout", Description: "Remove a member timeout", Options: moderationTargetReasonOptions()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "kick", Description: "Kick a member after private confirmation", Options: moderationTargetReasonOptions()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "ban", Description: "Ban a member after private confirmation", Options: append(moderationTargetReasonOptions(),
			disgocord.ApplicationCommandOptionInt{Name: "delete-message-days", Description: "Delete 0–7 days of recent messages", MinValue: intPtr(0), MaxValue: intPtr(7)},
		)},
		disgocord.ApplicationCommandOptionSubCommand{Name: "unban", Description: "Remove a ban by Discord user ID", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "user-id", Description: "Banned user's Discord ID", Required: true, MinLength: intPtr(1), MaxLength: intPtr(20)},
			reasonOption(),
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "case", Description: "View one moderation case", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionInt{Name: "case-id", Description: "Moderation case number", Required: true, MinValue: intPtr(1)},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "history", Description: "View bounded moderation history", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionUser{Name: "user", Description: "Optional member filter"},
			disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Maximum cases", MinValue: intPtr(1), MaxValue: intPtr(25)},
		}},
	}
}

func moderationTargetReasonOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionUser{Name: "user", Description: "Member to moderate", Required: true},
		reasonOption(),
	}
}

func reasonOption() disgocord.ApplicationCommandOptionString {
	return disgocord.ApplicationCommandOptionString{Name: "reason", Description: "Actionable reason recorded in the case", Required: true, MinLength: intPtr(3), MaxLength: intPtr(400)}
}

func roleTierChoices() []disgocord.ApplicationCommandOptionChoiceString {
	return []disgocord.ApplicationCommandOptionChoiceString{
		{Name: "Operator", Value: "operator"}, {Name: "Moderator", Value: "moderator"}, {Name: "Admin", Value: "admin"},
	}
}

func ruleKindChoices() []disgocord.ApplicationCommandOptionChoiceString {
	return []disgocord.ApplicationCommandOptionChoiceString{
		{Name: "ICAO", Value: "icao"}, {Name: "Registration", Value: "registration"}, {Name: "Exact callsign", Value: "callsign"}, {Name: "Callsign prefix", Value: "callsign-prefix"}, {Name: "Squawk", Value: "squawk"},
		{Name: "Operator (best-effort)", Value: "operator"}, {Name: "Owner (best-effort)", Value: "owner"}, {Name: "Aircraft type (best-effort)", Value: "aircraft-type"},
	}
}

func ruleChoiceOption() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionString{Name: "rule", Description: "Saved rule", Required: true, Autocomplete: true},
	}
}

func intPtr(value int) *int { return &value }

func guildAndBotDMContextList() []disgocord.InteractionContextType {
	return []disgocord.InteractionContextType{
		disgocord.InteractionContextTypeGuild,
		disgocord.InteractionContextTypeBotDM,
	}
}

func validateCommandInstallScope(command disgocord.ApplicationCommandCreate) error {
	name := command.CommandName()
	switch typed := command.(type) {
	case disgocord.SlashCommandCreate:
		if len(typed.IntegrationTypes) != 1 || typed.IntegrationTypes[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
			return fmt.Errorf("command %q must allow guild installation only", name)
		}
		if !guildAndBotDMContexts(typed.Contexts) {
			return fmt.Errorf("command %q must allow guild and bot DM interaction contexts only", name)
		}
	case disgocord.MessageCommandCreate:
		if len(typed.IntegrationTypes) != 1 || typed.IntegrationTypes[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
			return fmt.Errorf("command %q must allow guild installation only", name)
		}
		if !guildAndBotDMContexts(typed.Contexts) {
			return fmt.Errorf("command %q must allow guild and bot DM interaction contexts only", name)
		}
	default:
		return fmt.Errorf("command %q has unsupported type %T", name, command)
	}
	return nil
}

func guildAndBotDMContexts(contexts []disgocord.InteractionContextType) bool {
	if len(contexts) != 2 {
		return false
	}
	hasGuild, hasBotDM := false, false
	for _, context := range contexts {
		switch context {
		case disgocord.InteractionContextTypeGuild:
			hasGuild = true
		case disgocord.InteractionContextTypeBotDM:
			hasBotDM = true
		default:
			return false
		}
	}
	return hasGuild && hasBotDM
}
