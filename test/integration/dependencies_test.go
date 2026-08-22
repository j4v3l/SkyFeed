package integration_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/gateway"
	_ "modernc.org/sqlite"
)

func TestModerncSQLiteSmoke(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "spike.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})

	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}

	if _, err := db.Exec(`
		CREATE TABLE smoke (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO smoke (value) VALUES (?)", "skyfeed"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	var value string
	if err := db.QueryRow("SELECT value FROM smoke WHERE id = 1").Scan(&value); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if value != "skyfeed" {
		t.Fatalf("value = %q, want skyfeed", value)
	}
}

func TestDisgoRequiredSurfaceCompiles(t *testing.T) {
	command := discord.SlashCommandCreate{
		Name:        "status",
		Description: "Show SkyFeed receiver status",
	}
	button := discord.NewPrimaryButton("Refresh", "v1:opaque-session:refresh")
	selectMenu := discord.NewStringSelectMenu(
		"v1:opaque-session:sort",
		"Sort aircraft",
		discord.NewStringSelectMenuOption("Distance", "distance"),
	)
	modal := discord.NewModalCreate("v1:opaque-session:watch", "Create watch rule").
		AddLabel("ICAO", discord.NewShortTextInput("icao"))
	autocomplete := discord.AutocompleteResult{
		Choices: []discord.AutocompleteChoice{
			discord.AutocompleteChoiceString{Name: "ABC123", Value: "ABC123"},
		},
	}

	if command.CommandName() != "status" || button.CustomID == "" || selectMenu.CustomID == "" || modal.CustomID == "" || len(autocomplete.Choices) != 1 {
		t.Fatal("required disgo command or interaction surface was not constructed")
	}

	client, err := disgo.New(
		// Base64 encodes a synthetic Discord snowflake. It is structurally
		// sufficient for client construction and is never used on the network.
		"MTIzNDU2Nzg5MDEyMzQ1Njc4",
	)
	if err != nil {
		t.Fatalf("construct disgo client: %v", err)
	}
	client.Close(context.Background())

	gatewayClient := gateway.New(
		"synthetic-token",
		func(gateway.Gateway, gateway.EventType, int, gateway.EventData) {},
		gateway.WithIntents(gateway.IntentsNone),
	)
	if gatewayClient.Intents() != gateway.IntentsNone {
		t.Fatal("Gateway did not retain the configured minimal intents")
	}

	// Compile-check the idempotent command-registration and Gateway lifecycle
	// surfaces without making any Discord request.
	_ = client.Rest.GetGlobalCommands
	_ = client.Rest.GetGuildCommands
	_ = client.Rest.CreateGlobalCommand
	_ = client.Rest.CreateGuildCommand
	_ = client.Rest.UpdateGlobalCommand
	_ = client.Rest.UpdateGuildCommand
	_ = client.Rest.DeleteGlobalCommand
	_ = client.Rest.DeleteGuildCommand
	_ = client.OpenGateway
	_ = client.Close
}
