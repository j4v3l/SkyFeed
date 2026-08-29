package discord

import (
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
)

func TestDesiredCommandsAreOwnedUniqueAndNative(t *testing.T) {
	commands := DesiredCommands()
	if len(commands) != 23 {
		t.Fatalf("got %d commands", len(commands))
	}
	if err := validateDesiredCommands(commands); err != nil {
		t.Fatal(err)
	}
	var sawLookup, sawDelete bool
	for _, command := range commands {
		switch command.Type() {
		case disgocord.ApplicationCommandTypeSlash:
			slash := command.(disgocord.SlashCommandCreate)
			if len(slash.IntegrationTypes) != 1 || slash.IntegrationTypes[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
				t.Fatalf("%s integration types = %#v", slash.Name, slash.IntegrationTypes)
			}
			if !guildAndBotDMContexts(slash.Contexts) {
				t.Fatalf("%s contexts = %#v", slash.Name, slash.Contexts)
			}
		case disgocord.ApplicationCommandTypeMessage:
			switch command.CommandName() {
			case LookupAircraftCommand:
				sawLookup = true
			case DeleteMessageCommand:
				sawDelete = true
			default:
				t.Fatalf("unexpected message command %q", command.CommandName())
			}
		default:
			t.Fatalf("%s has unsupported type %v", command.CommandName(), command.Type())
		}
	}
	if !sawLookup || !sawDelete {
		t.Fatal("required message commands missing")
	}
}

func TestCommandValidationRejectsBroaderInstallOrInteractionScope(t *testing.T) {
	command := DesiredCommands()[0].(disgocord.SlashCommandCreate)
	command.IntegrationTypes = []disgocord.ApplicationIntegrationType{disgocord.ApplicationIntegrationTypeUserInstall}
	if err := validateDesiredCommands([]disgocord.ApplicationCommandCreate{command}); err == nil {
		t.Fatal("user-install command passed validation")
	}
	command = DesiredCommands()[0].(disgocord.SlashCommandCreate)
	command.Contexts = []disgocord.InteractionContextType{disgocord.InteractionContextTypeBotDM}
	if err := validateDesiredCommands([]disgocord.ApplicationCommandCreate{command}); err == nil {
		t.Fatal("bot-DM-only command passed validation")
	}
	command = DesiredCommands()[0].(disgocord.SlashCommandCreate)
	command.Contexts = []disgocord.InteractionContextType{disgocord.InteractionContextTypeGuild}
	if err := validateDesiredCommands([]disgocord.ApplicationCommandCreate{command}); err == nil {
		t.Fatal("guild-only command passed validation")
	}
}

func TestModerationCommandUsesTypedTargetsAndBoundedReasons(t *testing.T) {
	for _, command := range DesiredCommands() {
		if command.CommandName() != "moderation" {
			continue
		}
		slash := command.(disgocord.SlashCommandCreate)
		warn := slash.Options[0].(disgocord.ApplicationCommandOptionSubCommand)
		if _, ok := warn.Options[0].(disgocord.ApplicationCommandOptionUser); !ok {
			t.Fatalf("warn target is %T", warn.Options[0])
		}
		reason := warn.Options[1].(disgocord.ApplicationCommandOptionString)
		if !reason.Required || reason.MinLength == nil || *reason.MinLength != 3 || reason.MaxLength == nil || *reason.MaxLength != 400 {
			t.Fatalf("reason option = %+v", reason)
		}
		return
	}
	t.Fatal("moderation command not found")
}

func TestEverySubcommandPlacesRequiredOptionsFirst(t *testing.T) {
	for _, command := range DesiredCommands() {
		slash, ok := command.(disgocord.SlashCommandCreate)
		if !ok {
			continue
		}
		for _, option := range slash.Options {
			subcommand, ok := option.(disgocord.ApplicationCommandOptionSubCommand)
			if !ok {
				continue
			}
			optionalSeen := false
			for _, child := range subcommand.Options {
				required := commandOptionRequired(child)
				if !required {
					optionalSeen = true
				} else if optionalSeen {
					t.Fatalf("%s %s has required option %s after an optional option", slash.Name, subcommand.Name, child.OptionName())
				}
			}
		}
	}
}

func commandOptionRequired(option disgocord.ApplicationCommandOption) bool {
	switch value := option.(type) {
	case disgocord.ApplicationCommandOptionString:
		return value.Required
	case disgocord.ApplicationCommandOptionInt:
		return value.Required
	case disgocord.ApplicationCommandOptionBool:
		return value.Required
	case disgocord.ApplicationCommandOptionUser:
		return value.Required
	case disgocord.ApplicationCommandOptionChannel:
		return value.Required
	case disgocord.ApplicationCommandOptionRole:
		return value.Required
	case disgocord.ApplicationCommandOptionMentionable:
		return value.Required
	case disgocord.ApplicationCommandOptionFloat:
		return value.Required
	case disgocord.ApplicationCommandOptionAttachment:
		return value.Required
	default:
		return false
	}
}

func TestAircraftUsesAutocomplete(t *testing.T) {
	for _, command := range DesiredCommands() {
		if command.CommandName() != "aircraft" {
			continue
		}
		slash := command.(disgocord.SlashCommandCreate)
		query := slash.Options[0].(disgocord.ApplicationCommandOptionString)
		if !query.Required || !query.Autocomplete {
			t.Fatalf("query option = %+v", query)
		}
		return
	}
	t.Fatal("aircraft command not found")
}

func TestSettingsDefaultsToManageGuild(t *testing.T) {
	for _, command := range DesiredCommands() {
		if command.CommandName() == "settings" {
			settings := command.(disgocord.SlashCommandCreate)
			if !settings.DefaultMemberPermissions.OK || settings.DefaultMemberPermissions.Value == nil || *settings.DefaultMemberPermissions.Value != disgocord.PermissionManageGuild {
				t.Fatalf("settings permissions = %+v", settings.DefaultMemberPermissions)
			}
			return
		}
	}
	t.Fatal("settings command not found")
}

func TestCommandPickerPermissionsMatchAccessTiers(t *testing.T) {
	want := map[string]disgocord.Permissions{
		"alerts":     disgocord.PermissionManageGuild,
		"reports":    disgocord.PermissionManageGuild,
		"audit":      disgocord.PermissionManageGuild,
		"moderation": disgocord.PermissionModerateMembers,
		"settings":   disgocord.PermissionManageGuild,
	}
	for _, command := range DesiredCommands() {
		slash, ok := command.(disgocord.SlashCommandCreate)
		if !ok {
			continue
		}
		expected, privileged := want[slash.Name]
		if !privileged {
			if slash.DefaultMemberPermissions.OK && slash.DefaultMemberPermissions.Value != nil && *slash.DefaultMemberPermissions.Value != 0 {
				t.Fatalf("%s should be visible to viewers, got %+v", slash.Name, slash.DefaultMemberPermissions)
			}
			continue
		}
		if !slash.DefaultMemberPermissions.OK || slash.DefaultMemberPermissions.Value == nil || *slash.DefaultMemberPermissions.Value != expected {
			t.Fatalf("%s permissions = %+v want %d", slash.Name, slash.DefaultMemberPermissions, expected)
		}
	}
}

func TestEmergencyAndTrafficCommandsExist(t *testing.T) {
	found := map[string]bool{}
	for _, command := range DesiredCommands() {
		found[command.CommandName()] = true
	}
	for _, name := range []string{"emergency", "traffic", "airline", LookupAircraftCommand} {
		if !found[name] {
			t.Fatalf("%s command missing", name)
		}
	}
}

func TestSettingsIncludesOpsControls(t *testing.T) {
	for _, command := range DesiredCommands() {
		if command.CommandName() != "settings" {
			continue
		}
		slash := command.(disgocord.SlashCommandCreate)
		names := map[string]bool{}
		for _, option := range slash.Options {
			names[option.OptionName()] = true
		}
		for _, name := range []string{"pause-alerts", "resume-alerts", "mute-squawk", "unmute-squawk", "recreate-dashboard"} {
			if !names[name] {
				t.Fatalf("settings missing %s", name)
			}
		}
		return
	}
	t.Fatal("settings command not found")
}
