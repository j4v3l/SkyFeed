package discord

import (
	"fmt"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/omit"
)

const CommandSchemaVersion = 4

// ownedCommandNames is permanent command ownership history. When a command is
// removed from DesiredCommands, leave its name here as a deletion tombstone so
// synchronization can remove the stale remote command without touching names
// owned by another application feature.
var ownedCommandNames = map[string]struct{}{
	"status": {}, "nearby": {}, "aircraft": {}, "watch": {}, "alerts": {},
	"reports": {}, "feeder": {}, "settings": {}, "help": {},
	"moderation": {},
}

func DesiredCommands() []disgocord.ApplicationCommandCreate {
	minRadius, maxRadius := 1.0, 250.0
	minAltitude, maxAltitude := -2_000, 100_000
	minLimit, maxLimit := 1, 25
	admin := disgocord.Permissions(disgocord.PermissionManageGuild)
	commands := []disgocord.ApplicationCommandCreate{
		disgocord.SlashCommandCreate{Name: "status", Description: "Show receiver, source, and SkyFeed health"},
		disgocord.SlashCommandCreate{
			Name: "nearby", Description: "Browse aircraft currently visible to this receiver",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionFloat{Name: "radius-nm", Description: "Maximum receiver distance in nautical miles", MinValue: &minRadius, MaxValue: &maxRadius},
				disgocord.ApplicationCommandOptionInt{Name: "altitude-min", Description: "Minimum pressure altitude in feet", MinValue: &minAltitude, MaxValue: &maxAltitude},
				disgocord.ApplicationCommandOptionInt{Name: "altitude-max", Description: "Maximum pressure altitude in feet", MinValue: &minAltitude, MaxValue: &maxAltitude},
				disgocord.ApplicationCommandOptionInt{Name: "limit", Description: "Number of aircraft per page", MinValue: &minLimit, MaxValue: &maxLimit},
				disgocord.ApplicationCommandOptionString{Name: "sort", Description: "Aircraft ordering", Choices: []disgocord.ApplicationCommandOptionChoiceString{
					{Name: "Distance", Value: "distance"}, {Name: "Altitude", Value: "altitude"}, {Name: "Callsign", Value: "callsign"},
				}},
			},
		},
		disgocord.SlashCommandCreate{
			Name: "aircraft", Description: "Show one currently visible aircraft",
			Options: []disgocord.ApplicationCommandOption{
				disgocord.ApplicationCommandOptionString{Name: "query", Description: "ICAO, registration, or callsign", Required: true, Autocomplete: true},
			},
		},
		disgocord.SlashCommandCreate{Name: "watch", Description: "Manage personal or server aircraft watch rules", Options: watchOptions()},
		disgocord.SlashCommandCreate{Name: "alerts", Description: "View or configure alert delivery", Options: alertsOptions()},
		disgocord.SlashCommandCreate{Name: "reports", Description: "Generate reports or manage schedules", Options: reportOptions()},
		disgocord.SlashCommandCreate{Name: "feeder", Description: "Show receiver, statistics, range, and source diagnostics"},
		disgocord.SlashCommandCreate{Name: "moderation", Description: "Moderate server members with durable private case records", Options: moderationOptions()},
		disgocord.SlashCommandCreate{
			Name: "settings", Description: "Configure SkyFeed for this server", DefaultMemberPermissions: omit.NewPtr(admin),
			Options: settingsOptions(),
		},
		disgocord.SlashCommandCreate{Name: "help", Description: "Show a permission-aware SkyFeed task guide"},
	}
	for index, command := range commands {
		slash := command.(disgocord.SlashCommandCreate)
		slash.IntegrationTypes = []disgocord.ApplicationIntegrationType{disgocord.ApplicationIntegrationTypeGuildInstall}
		slash.Contexts = []disgocord.InteractionContextType{disgocord.InteractionContextTypeGuild}
		commands[index] = slash
	}
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
		slash, ok := command.(disgocord.SlashCommandCreate)
		if !ok {
			return fmt.Errorf("command %q is not a slash command", name)
		}
		if len(slash.IntegrationTypes) != 1 || slash.IntegrationTypes[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
			return fmt.Errorf("command %q must allow guild installation only", name)
		}
		if len(slash.Contexts) != 1 || slash.Contexts[0] != disgocord.InteractionContextTypeGuild {
			return fmt.Errorf("command %q must allow guild interaction context only", name)
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
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "remove", Description: "Remove a saved watch rule", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "rule", Description: "Saved rule", Required: true, Autocomplete: true},
		}},
		disgocord.ApplicationCommandOptionSubCommand{Name: "enable", Description: "Enable a saved watch rule", Options: ruleChoiceOption()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "disable", Description: "Disable a saved watch rule", Options: ruleChoiceOption()},
		disgocord.ApplicationCommandOptionSubCommand{Name: "list", Description: "List watch rules visible to you"},
	}
}

func alertsOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "view", Description: "View alert categories and cooldowns"},
		disgocord.ApplicationCommandOptionSubCommand{Name: "configure", Description: "Configure an alert category", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "category", Description: "Alert category", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Watch rules", Value: "watch"}, {Name: "Emergencies", Value: "emergency"}, {Name: "Feeder health", Value: "feeder"},
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

func settingsOptions() []disgocord.ApplicationCommandOption {
	return []disgocord.ApplicationCommandOption{
		disgocord.ApplicationCommandOptionSubCommand{Name: "channels", Description: "Configure a durable channel binding", Options: []disgocord.ApplicationCommandOption{
			disgocord.ApplicationCommandOptionString{Name: "purpose", Description: "Channel purpose", Required: true, Choices: []disgocord.ApplicationCommandOptionChoiceString{
				{Name: "Live dashboard", Value: "live"}, {Name: "Alerts", Value: "alerts"}, {Name: "Emergencies", Value: "emergencies"}, {Name: "Reports", Value: "reports"}, {Name: "Administration", Value: "admin"}, {Name: "Moderation log", Value: "moderation"},
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
				{Name: "Alerts", Value: "alerts"}, {Name: "Emergencies", Value: "emergencies"}, {Name: "Reports", Value: "reports"}, {Name: "Administration", Value: "admin"},
			}},
		}},
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
