package discord

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

type flightLeadersRESTStub struct {
	nextID   snowflake.ID
	messages map[snowflake.ID]*disgocord.Message
	creates  int
	updates  int
}

func (stub *flightLeadersRESTStub) CreateMessage(channelID snowflake.ID, _ disgocord.MessageCreate, _ ...rest.RequestOpt) (*disgocord.Message, error) {
	stub.creates++
	stub.nextID++
	message := &disgocord.Message{ID: stub.nextID, ChannelID: channelID}
	stub.messages[message.ID] = message
	return message, nil
}

func (stub *flightLeadersRESTStub) UpdateMessage(channelID, messageID snowflake.ID, _ disgocord.MessageUpdate, _ ...rest.RequestOpt) (*disgocord.Message, error) {
	message := stub.messages[messageID]
	if message == nil || message.ChannelID != channelID {
		return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownMessage}
	}
	stub.updates++
	return message, nil
}

func TestFlightLeadersCreateEditAndRecreate(t *testing.T) {
	ctx := context.Background()
	repository, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := repository.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: 42, Purpose: "reports", ChannelID: 99, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshot := &domain.Snapshot{
		PublishedAt: now,
		Health:      domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}},
		Aircraft:    []domain.Aircraft{{ICAO: "ABC123", HasGroundSpeed: true, GroundSpeedKts: 300, HasAltitude: true, AltitudeFeet: 20_000}},
	}
	router := NewRouter(snapshotStub{snapshot}, NewSessionManager(100, 10, time.Minute), 42, now)
	service := NewGatewayService(config.Discord{GuildID: 42}, router, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	service.SetRepository(repository)
	discordREST := &flightLeadersRESTStub{nextID: 500, messages: make(map[snowflake.ID]*disgocord.Message)}

	if err := service.updateFlightLeadersWithREST(ctx, discordREST); err != nil {
		t.Fatal(err)
	}
	binding, found, err := repository.MessageBinding(ctx, 42, "flight-leaders")
	if err != nil || !found || binding.MessageID != 501 || discordREST.creates != 1 {
		t.Fatalf("initial binding=%+v found=%t creates=%d err=%v", binding, found, discordREST.creates, err)
	}
	if err := service.updateFlightLeadersWithREST(ctx, discordREST); err != nil {
		t.Fatal(err)
	}
	if discordREST.creates != 1 || discordREST.updates != 1 {
		t.Fatalf("creates=%d updates=%d", discordREST.creates, discordREST.updates)
	}
	delete(discordREST.messages, snowflake.ID(binding.MessageID))
	if err := service.updateFlightLeadersWithREST(ctx, discordREST); err != nil {
		t.Fatal(err)
	}
	binding, found, err = repository.MessageBinding(ctx, 42, "flight-leaders")
	if err != nil || !found || binding.MessageID != 502 || discordREST.creates != 2 {
		t.Fatalf("recreated binding=%+v found=%t creates=%d err=%v", binding, found, discordREST.creates, err)
	}
}
