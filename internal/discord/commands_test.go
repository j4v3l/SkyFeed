package discord

import (
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
)

func TestDesiredCommandsAreOwnedUniqueAndNative(t *testing.T) {
	commands := DesiredCommands()
	if len(commands) != 17 {
		t.Fatalf("got %d commands", len(commands))
	}
	if err := validateDesiredCommands(commands); err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if command.Type() != disgocord.ApplicationCommandTypeSlash {
			t.Fatalf("%s is not a slash command", command.CommandName())
		}
		slash := command.(disgocord.SlashCommandCreate)
		if len(slash.IntegrationTypes) != 1 || slash.IntegrationTypes[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
			t.Fatalf("%s integration types = %#v", slash.Name, slash.IntegrationTypes)
		}
		if len(slash.Contexts) != 1 || slash.Contexts[0] != disgocord.InteractionContextTypeGuild {
			t.Fatalf("%s contexts = %#v", slash.Name, slash.Contexts)
		}
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
		t.Fatal("direct-message command passed validation")
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

func TestEmergencyAndTrafficCommandsExist(t *testing.T) {
	found := map[string]bool{}
	for _, command := range DesiredCommands() {
		found[command.CommandName()] = true
	}
	for _, name := range []string{"emergency", "traffic"} {
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
