package discord

import (
	"bytes"
	"testing"

	disgocord "github.com/disgoorg/disgo/discord"
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
	if CommandSchemaVersion < 1 || len(first) < 100 {
		t.Fatalf("schema version=%d bytes=%d", CommandSchemaVersion, len(first))
	}
}

func TestSlashUpdateIncludesCompleteOwnedSchema(t *testing.T) {
	desired := DesiredCommands()[1].(disgocord.SlashCommandCreate)
	update := slashUpdate(desired)
	if update.Name == nil || *update.Name != desired.Name || update.Description == nil || *update.Description != desired.Description || update.Options == nil || len(*update.Options) != len(desired.Options) {
		t.Fatalf("incomplete update: %+v", update)
	}
}
