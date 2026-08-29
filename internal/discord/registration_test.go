package discord

import (
	"bytes"
	"encoding/json"
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

func TestCommandSchemaIsStableAndVersioned(t *testing.T) {
	first, err := MarshalCommandSchema()
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalCommandSchema()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("command schema is not deterministic")
	}
	if CommandSchemaVersion != 16 || len(first) < 100 {
		t.Fatalf("schema version=%d bytes=%d", CommandSchemaVersion, len(first))
	}
}

func TestSlashUpdateIncludesCompleteOwnedSchema(t *testing.T) {
	desired := DesiredCommands()[1].(disgocord.SlashCommandCreate)
	update := slashUpdate(desired)
	if update.Name == nil || *update.Name != desired.Name || update.Description == nil || *update.Description != desired.Description || update.Options == nil || len(*update.Options) != len(desired.Options) {
		t.Fatalf("incomplete update: %+v", update)
	}
	if update.IntegrationTypes == nil || len(*update.IntegrationTypes) != 1 || (*update.IntegrationTypes)[0] != disgocord.ApplicationIntegrationTypeGuildInstall {
		t.Fatalf("integration types = %#v", update.IntegrationTypes)
	}
	if update.Contexts == nil || !guildAndBotDMContexts(*update.Contexts) {
		t.Fatalf("contexts = %#v", update.Contexts)
	}
}

func TestCommandEquivalentComparesPermissionsInstallAndContext(t *testing.T) {
	status := DesiredCommands()[0].(disgocord.SlashCommandCreate)
	remoteStatus := remoteSlashCommand(t, status, "1")
	if !commandEquivalent(remoteStatus, status) {
		t.Fatal("identical command was not equivalent")
	}

	userInstall := status
	userInstall.IntegrationTypes = []disgocord.ApplicationIntegrationType{disgocord.ApplicationIntegrationTypeUserInstall}
	if commandEquivalent(remoteStatus, userInstall) {
		t.Fatal("integration type drift was ignored")
	}
	directMessage := status
	directMessage.Contexts = []disgocord.InteractionContextType{disgocord.InteractionContextTypeGuild}
	if commandEquivalent(remoteStatus, directMessage) {
		t.Fatal("context drift was ignored")
	}

	settings := findCommand(t, "settings").(disgocord.SlashCommandCreate)
	remoteSettings := remoteSlashCommand(t, settings, "2")
	withoutPermissions := settings
	withoutPermissions.DefaultMemberPermissions = status.DefaultMemberPermissions
	if commandEquivalent(remoteSettings, withoutPermissions) {
		t.Fatal("removing default permissions did not trigger an update")
	}
}

func TestSyncDeletesOwnedTombstonesAndIgnoresForeignCommands(t *testing.T) {
	owned := remoteSlashCommand(t, DesiredCommands()[0].(disgocord.SlashCommandCreate), "1")
	foreign := remoteSlashCommand(t, disgocord.SlashCommandCreate{Name: "foreign", Description: "Not owned by SkyFeed"}, "2")
	var deleted []string
	stats, err := syncCommandSet(commandAPI{
		scope: "test",
		list: func() ([]disgocord.ApplicationCommand, error) {
			return []disgocord.ApplicationCommand{owned, foreign}, nil
		},
		delete: func(id snowflake.ID) error {
			deleted = append(deleted, id.String())
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Deleted != 1 || stats.Ignored != 1 || len(deleted) != 1 || deleted[0] != "1" {
		t.Fatalf("stats=%+v deleted=%v", stats, deleted)
	}
}

func findCommand(t *testing.T, name string) disgocord.ApplicationCommandCreate {
	t.Helper()
	for _, command := range DesiredCommands() {
		if command.CommandName() == name {
			return command
		}
	}
	t.Fatalf("command %q not found", name)
	return nil
}

func remoteSlashCommand(t *testing.T, command disgocord.SlashCommandCreate, id string) disgocord.SlashCommand {
	t.Helper()
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	object["id"] = id
	object["application_id"] = "100"
	object["version"] = "1"
	object["type"] = disgocord.ApplicationCommandTypeSlash
	encoded, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	var remote disgocord.SlashCommand
	if err := json.Unmarshal(encoded, &remote); err != nil {
		t.Fatal(err)
	}
	return remote
}
