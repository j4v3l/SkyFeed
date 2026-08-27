package discord

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const feederEnrollmentTTL = 15 * time.Minute

type FeederAdminConfig struct {
	Enabled    bool
	PublicURL  string
	MaxFeeders int
}

func (router *Router) SetFeederAdminConfig(config FeederAdminConfig) {
	if config.MaxFeeders <= 0 {
		config.MaxFeeders = 100
	}
	config.PublicURL = strings.TrimRight(strings.TrimSpace(config.PublicURL), "/")
	router.feederAdmin = config
}

func (router *Router) handleFeeders(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Feeder administration requires persistent storage."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Feeder records are temporarily unavailable."))
	}
	if request.Subcommand == "list" {
		return router.listFeeders(ctx, request, responder)
	}
	if request.Subcommand == "show" {
		return router.showFeeder(ctx, request, responder, false)
	}
	if !request.ManageGuild || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, request.Administrator, "admin") {
		return responder.CreateMessage(errorMessage("A configured Admin role plus Manage Server permission is required to change feeders."))
	}
	switch request.Subcommand {
	case "invite":
		return router.inviteFeeder(ctx, request, responder)
	case "rename", "set-area", "set-weather-station", "enable", "disable", "set-default", "rotate", "revoke", "test":
		return router.mutateFeeder(ctx, request, responder)
	default:
		return responder.CreateMessage(errorMessage("Choose a feeder subcommand."))
	}
}

func (router *Router) listFeeders(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	feeders, err := router.repository.Feeders(ctx, request.GuildID, min(router.feederAdmin.MaxFeeders+1, 250))
	if err != nil {
		return responder.CreateMessage(errorMessage("Approved feeders could not be loaded."))
	}
	health := router.feederSummaries()
	embed := disgocord.NewEmbed().WithTitle("SkyFeed • Community feeders").WithColor(render.Scope)
	embed.Description = "Friendly community coverage using approved public airport and area details. Private installation and account details are never shown here."
	for _, feeder := range feeders {
		summary := health[feeder.Descriptor.ID]
		state := "offline"
		if !feeder.Descriptor.Enabled {
			state = "disabled"
		} else if !summary.LastPublished.IsZero() {
			state = string(summary.Health)
		}
		area := firstNonEmpty(feeder.Descriptor.PublicArea, feeder.Descriptor.AirportICAO, "Area not set")
		embed.Fields = append(embed.Fields, disgocord.EmbedField{
			Name:  render.Truncate(feeder.Descriptor.DisplayName+" • "+state, 256),
			Value: render.Truncate(fmt.Sprintf("%s • %d aircraft", area, summary.Aircraft), 1024),
		})
	}
	if len(feeders) == 0 {
		embed.Description += "\n\nNo feeders are registered yet."
	}
	return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), false))
}

func (router *Router) showFeeder(ctx context.Context, request CommandRequest, responder InteractionResponder, private bool) error {
	id, err := domain.NormalizeFeederID(request.Strings["feeder"])
	if err != nil || id == domain.FeederAll {
		return responder.CreateMessage(errorMessage("Choose a valid approved feeder."))
	}
	feeder, err := router.repository.Feeder(ctx, id)
	if err != nil || feeder.GuildID != request.GuildID {
		return responder.CreateMessage(errorMessage("That feeder is not approved in this server."))
	}
	summary := router.feederSummaries()[id]
	state := string(summary.Health)
	if state == "" {
		state = "offline"
	}
	if !feeder.Descriptor.Enabled {
		state = "disabled"
	}
	embed := disgocord.NewEmbed().WithTitle("SkyFeed • " + feeder.Descriptor.DisplayName).WithColor(render.Scope)
	embed.Description = firstNonEmpty(feeder.Descriptor.PublicArea, "Public area not configured")
	embed.Fields = append(embed.Fields,
		disgocord.EmbedField{Name: "Status", Value: fmt.Sprintf("%s • %d visible aircraft", state, summary.Aircraft), Inline: feederBoolPtr(true)},
		disgocord.EmbedField{Name: "Airport", Value: firstNonEmpty(feeder.Descriptor.AirportICAO, "Not configured"), Inline: feederBoolPtr(true)},
		disgocord.EmbedField{Name: "Source", Value: string(feeder.Descriptor.SourceKind), Inline: feederBoolPtr(true)},
	)
	if private {
		lastSeen := "Never"
		if !feeder.LastSeenAt.IsZero() {
			lastSeen = fmt.Sprintf("<t:%d:R>", feeder.LastSeenAt.Unix())
		}
		embed.Fields = append(embed.Fields,
			disgocord.EmbedField{Name: "Agent identity", Value: fmt.Sprintf("key enrolled: %t • last sequence: %d", len(feeder.PublicKey) == 32, feeder.LastSequence)},
			disgocord.EmbedField{Name: "Last accepted delivery", Value: lastSeen},
		)
	}
	return responder.CreateMessage(render.SafeMessage(render.BoundEmbed(embed), private))
}

func (router *Router) feederSummaries() map[domain.FeederID]domain.FeederSummary {
	result := make(map[domain.FeederID]domain.FeederSummary)
	if provider, ok := router.snapshots.(FeederSnapshotProvider); ok {
		for _, summary := range provider.ListFeeders() {
			result[summary.ID] = summary
		}
	}
	return result
}

func (router *Router) inviteFeeder(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	if !router.feederAdmin.Enabled || router.feederAdmin.PublicURL == "" {
		return responder.CreateMessage(errorMessage("Community feeder ingress is disabled. Configure a private HTTPS ingress before creating invitations."))
	}
	feeders, err := router.repository.Feeders(ctx, request.GuildID, router.feederAdmin.MaxFeeders+1)
	if err != nil {
		return responder.CreateMessage(errorMessage("The feeder limit could not be checked."))
	}
	remoteCount := 0
	for _, feeder := range feeders {
		if feeder.Descriptor.SourceKind == domain.FeederSourceAgent {
			remoteCount++
		}
	}
	if remoteCount >= router.feederAdmin.MaxFeeders {
		return responder.CreateMessage(errorMessage("This server has reached its configured community feeder limit."))
	}
	name := strings.TrimSpace(request.Strings["name"])
	area := strings.TrimSpace(request.Strings["area"])
	if name == "" || len(name) > 80 || area == "" || len(area) > 80 {
		return responder.CreateMessage(errorMessage("Provide a public name and area of 1–80 characters."))
	}
	airport := ""
	if raw := strings.TrimSpace(request.Strings["airport"]); raw != "" {
		var ok bool
		airport, ok = enrichment.NormalizeAirportCode(raw)
		if !ok {
			return responder.CreateMessage(errorMessage("The public airport must be a four-character ICAO code."))
		}
	}
	id, err := newFeederID()
	if err != nil {
		return responder.CreateMessage(errorMessage("A secure feeder identity could not be generated."))
	}
	feeder := storage.Feeder{GuildID: request.GuildID, OwnerUserID: request.IDs["owner"], Descriptor: domain.FeederDescriptor{
		ID: id, DisplayName: name, PublicArea: area, AirportICAO: airport, SourceKind: domain.FeederSourceAgent, Enabled: true,
	}}
	if err := router.repository.UpsertFeeder(ctx, feeder); err != nil {
		return responder.CreateMessage(errorMessage("The feeder record could not be created."))
	}
	if registry, ok := router.snapshots.(FeederRegistry); ok {
		_ = registry.Register(feeder.Descriptor)
	}
	router.notifyFeederChanged(feeder.Descriptor)
	return router.createInvitation(ctx, feeder, responder)
}

func (router *Router) mutateFeeder(ctx context.Context, request CommandRequest, responder InteractionResponder) error {
	id, err := domain.NormalizeFeederID(request.Strings["feeder"])
	if err != nil || id == domain.FeederAll {
		return responder.CreateMessage(errorMessage("Choose a valid approved feeder."))
	}
	feeder, err := router.repository.Feeder(ctx, id)
	if err != nil || feeder.GuildID != request.GuildID {
		return responder.CreateMessage(errorMessage("That feeder is not approved in this server."))
	}
	now := router.now().UTC()
	switch request.Subcommand {
	case "test":
		return router.showFeeder(ctx, request, responder, true)
	case "rename":
		name := strings.TrimSpace(request.Strings["name"])
		if name == "" || len(name) > 80 {
			return responder.CreateMessage(errorMessage("The public name must contain 1–80 characters."))
		}
		feeder.Descriptor.DisplayName = name
	case "set-area":
		area := strings.TrimSpace(request.Strings["area"])
		airport, ok := enrichment.NormalizeAirportCode(request.Strings["airport"])
		latitude, latitudeOK := request.Floats["latitude"]
		longitude, longitudeOK := request.Floats["longitude"]
		if area == "" || len(area) > 80 || !ok || !latitudeOK || !longitudeOK || math.IsNaN(latitude) || math.IsNaN(longitude) || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			return responder.CreateMessage(errorMessage("Provide a valid public area, ICAO airport, latitude, and longitude."))
		}
		feeder.Descriptor.PublicArea, feeder.Descriptor.AirportICAO = area, airport
		feeder.Descriptor.Latitude, feeder.Descriptor.Longitude, feeder.Descriptor.HasCenter = latitude, longitude, true
	case "set-weather-station":
		station, ok := enrichment.NormalizeAirportCode(request.Strings["station"])
		if !ok {
			return responder.CreateMessage(errorMessage("The weather station must be a four-character ICAO code."))
		}
		feeder.Descriptor.WeatherStationICAO = station
	case "enable":
		if feeder.Descriptor.SourceKind == domain.FeederSourceAgent && len(feeder.PublicKey) != 32 {
			return responder.CreateMessage(errorMessage("This feeder has no active key. Use `/feeders rotate` to create a new invitation."))
		}
		feeder.Descriptor.Enabled = true
	case "disable":
		feeder.Descriptor.Enabled = false
	case "set-default":
		settings, settingsErr := router.repository.GuildSettings(ctx, request.GuildID)
		if settingsErr != nil {
			return responder.CreateMessage(errorMessage("Server settings could not be loaded."))
		}
		settings.DefaultFeederID = id
		if err := router.repository.UpsertGuildSettings(ctx, settings); err != nil {
			return responder.CreateMessage(errorMessage("The default feeder could not be saved."))
		}
		return responder.CreateMessage(infoMessage("Default feeder updated", feeder.Descriptor.DisplayName+" is now the default view. Use All feeders explicitly for the community view."))
	case "rotate":
		if feeder.Descriptor.SourceKind != domain.FeederSourceAgent {
			return responder.CreateMessage(errorMessage("The local feeder does not use an agent key."))
		}
		if !router.feederAdmin.Enabled || router.feederAdmin.PublicURL == "" {
			return responder.CreateMessage(errorMessage("Community feeder ingress is disabled."))
		}
		if err := router.repository.RevokeFeeder(ctx, id, now); err != nil {
			return responder.CreateMessage(errorMessage("The old agent key could not be revoked."))
		}
		feeder.PublicKey, feeder.LastPayloadHash, feeder.LastSequence = nil, nil, 0
		feeder.Descriptor.Enabled = true
		feeder.UpdatedAt = now
		if err := router.repository.UpsertFeeder(ctx, feeder); err != nil {
			return responder.CreateMessage(errorMessage("The feeder could not be prepared for reenrollment."))
		}
		if registry, ok := router.snapshots.(FeederRegistry); ok {
			_ = registry.Register(feeder.Descriptor)
		}
		router.notifyFeederChanged(feeder.Descriptor)
		return router.createInvitation(ctx, feeder, responder)
	case "revoke":
		if feeder.Descriptor.SourceKind != domain.FeederSourceAgent {
			return responder.CreateMessage(errorMessage("The local feeder cannot be revoked."))
		}
		if err := router.repository.RevokeFeeder(ctx, id, now); err != nil {
			return responder.CreateMessage(errorMessage("The feeder key could not be revoked."))
		}
		feeder.Descriptor.Enabled = false
		if registry, ok := router.snapshots.(FeederRegistry); ok {
			_ = registry.Register(feeder.Descriptor)
		}
		router.notifyFeederChanged(feeder.Descriptor)
		return responder.CreateMessage(infoMessage("Feeder revoked", feeder.Descriptor.DisplayName+" is disabled and its agent key can no longer submit snapshots."))
	default:
		return responder.CreateMessage(errorMessage("Choose a feeder action."))
	}
	feeder.UpdatedAt = now
	if err := router.repository.UpsertFeeder(ctx, feeder); err != nil {
		return responder.CreateMessage(errorMessage("The feeder change could not be saved."))
	}
	if registry, ok := router.snapshots.(FeederRegistry); ok {
		_ = registry.Register(feeder.Descriptor)
	}
	router.notifyFeederChanged(feeder.Descriptor)
	return responder.CreateMessage(infoMessage("Feeder updated", feeder.Descriptor.DisplayName+" was updated. Public views contain only approved metadata."))
}

func (router *Router) notifyFeederChanged(descriptor domain.FeederDescriptor) {
	if router.feederChanged != nil {
		router.feederChanged(descriptor)
	}
}

func (router *Router) createInvitation(ctx context.Context, feeder storage.Feeder, responder InteractionResponder) error {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return responder.CreateMessage(errorMessage("A secure enrollment code could not be generated."))
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	now := router.now().UTC()
	if err := router.repository.CreateFeederEnrollment(ctx, storage.FeederEnrollment{TokenHash: hash[:], FeederID: feeder.Descriptor.ID, CreatedAt: now, ExpiresAt: now.Add(feederEnrollmentTTL)}); err != nil {
		return responder.CreateMessage(errorMessage("The private invitation could not be stored."))
	}
	description := fmt.Sprintf("Feeder: **%s** (`%s`)\nEnrollment URL: `%s/v1/agent/enroll`\nOne-time code: `%s`\nExpires <t:%d:R>.\n\nSend this privately to the feeder owner. SkyFeed stores only its SHA-256 hash; this plaintext code cannot be recovered.",
		feeder.Descriptor.DisplayName, feeder.Descriptor.ID, router.feederAdmin.PublicURL, token, now.Add(feederEnrollmentTTL).Unix())
	return responder.CreateMessage(infoMessage("Private feeder invitation", description))
}

func newFeederID() (domain.FeederID, error) {
	random := make([]byte, 10)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random))
	return domain.FeederID("sf-" + encoded), nil
}

func feederBoolPtr(value bool) *bool { return &value }
