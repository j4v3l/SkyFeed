package discord

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

type SnapshotProvider interface {
	Current() *domain.Snapshot
}

type EnrichmentProvider interface {
	Cached(icao, callsign string) (domain.Enrichment, bool, error)
}

type InteractionResponder interface {
	CreateMessage(disgocord.MessageCreate) error
	UpdateMessage(disgocord.MessageUpdate) error
	ShowModal(disgocord.ModalCreate) error
	Autocomplete([]disgocord.AutocompleteChoice) error
}

type CommandRequest struct {
	Name          string
	Group         string
	Subcommand    string
	UserID        uint64
	GuildID       uint64
	ChannelID     uint64
	ManageGuild   bool
	Administrator bool
	Permissions   disgocord.Permissions
	RoleIDs       []uint64
	Strings       map[string]string
	Ints          map[string]int
	Floats        map[string]float64
	Bools         map[string]bool
	IDs           map[string]uint64
}

type ComponentRequest struct {
	CustomID      string
	UserID        uint64
	GuildID       uint64
	ChannelID     uint64
	Values        []string
	Administrator bool
	Permissions   disgocord.Permissions
	RoleIDs       []uint64
}

type ModalRequest struct {
	CustomID  string
	UserID    uint64
	GuildID   uint64
	ChannelID uint64
	Values    map[string]string
}

type AutocompleteRequest struct {
	Name       string
	Subcommand string
	Query      string
	UserID     uint64
	GuildID    uint64
}

type Router struct {
	snapshots         SnapshotProvider
	sessions          *SessionManager
	configuredGuildID uint64
	startedAt         time.Time
	now               func() time.Time
	repository        storage.Repository
	ruleReload        func()
	enrichment        EnrichmentProvider
	testSend          func(context.Context, uint64, string) error
	moderation        ModerationExecutor
}

func (router *Router) SetRepository(repository storage.Repository) { router.repository = repository }
func (router *Router) SetRuleReload(reload func())                 { router.ruleReload = reload }
func (router *Router) SetEnrichment(provider EnrichmentProvider)   { router.enrichment = provider }
func (router *Router) SetTestSender(sender func(context.Context, uint64, string) error) {
	router.testSend = sender
}
func (router *Router) SetModeration(executor ModerationExecutor) { router.moderation = executor }

func (router *Router) requestRuleReload() {
	if router.ruleReload != nil {
		router.ruleReload()
	}
}

func NewRouter(snapshots SnapshotProvider, sessions *SessionManager, configuredGuildID uint64, startedAt time.Time) *Router {
	return &Router{snapshots: snapshots, sessions: sessions, configuredGuildID: configuredGuildID, startedAt: startedAt, now: time.Now}
}

func (router *Router) HandleCommand(request CommandRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.CreateMessage(errorMessage("SkyFeed is not available in this server."))
	}
	snapshot := router.snapshots.Current()
	switch request.Name {
	case "status":
		return responder.CreateMessage(render.SafeMessage(render.Status(snapshot, router.now().Sub(router.startedAt), router.now(), router.enrichment != nil), false))
	case "nearby":
		return router.handleNearby(request, responder, snapshot)
	case "aircraft":
		return router.handleAircraft(request, responder, snapshot)
	case "help":
		return responder.CreateMessage(render.SafeMessage(render.Help(router.now(), request.ManageGuild), false))
	case "settings":
		return router.handleSettings(request, responder)
	case "moderation":
		return router.handleModeration(request, responder)
	case "watch":
		return router.handleWatch(request, responder)
	case "alerts":
		return router.handleAlerts(request, responder)
	case "reports":
		return router.handleReports(request, responder)
	case "feeder":
		return responder.CreateMessage(render.SafeMessage(render.Feeder(snapshot, router.now()), false))
	default:
		return responder.CreateMessage(errorMessage("Unknown SkyFeed command."))
	}
}

func (router *Router) HandleComponent(request ComponentRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.CreateMessage(errorMessage("SkyFeed is not available in this server."))
	}
	sessionID, action, err := ParseCustomID(request.CustomID)
	if err != nil {
		return responder.CreateMessage(errorMessage("This control is invalid or belongs to an older SkyFeed version."))
	}
	session, err := router.sessions.Get(sessionID, request.UserID, request.GuildID, request.ChannelID)
	if err != nil {
		return responder.CreateMessage(errorMessage("This control has expired or is not assigned to you."))
	}
	if session.View == "moderation" {
		return router.handleModerationComponent(request, responder, session, action)
	}

	switch action {
	case "previous":
		session.Page = max(0, session.Page-1)
	case "next":
		session.Page++
	case "refresh":
	case "sort":
		if len(request.Values) != 1 || !validSort(request.Values[0]) {
			return responder.CreateMessage(errorMessage("That sort option is not valid."))
		}
		session.Filter = request.Values[0]
		session.Page = 0
	case "watch":
		modalID, buildErr := CustomID(session.ID, "save-watch")
		if buildErr != nil {
			return buildErr
		}
		modal := disgocord.NewModalCreate(modalID, "Watch this aircraft").
			AddLabel("Rule label", disgocord.NewShortTextInput("label").WithPlaceholder("Optional name").WithMaxLength(64)).
			AddLabel("Cooldown in minutes", disgocord.NewShortTextInput("cooldown").WithPlaceholder("15").WithMaxLength(4))
		return responder.ShowModal(modal)
	case "close":
		router.sessions.Delete(session.ID)
		return responder.UpdateMessage(disgocord.NewMessageUpdate().WithContent("SkyFeed view closed.").ClearEmbeds().ClearComponents())
	default:
		return responder.CreateMessage(errorMessage("This SkyFeed control is no longer supported."))
	}

	if err := router.sessions.Update(session); err != nil {
		return responder.CreateMessage(errorMessage("This control expired while it was being updated."))
	}
	return router.updateSession(session, responder)
}

func (router *Router) HandleModal(request ModalRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.CreateMessage(errorMessage("SkyFeed is not available in this server."))
	}
	sessionID, action, err := ParseCustomID(request.CustomID)
	if err != nil || action != "save-watch" {
		return responder.CreateMessage(errorMessage("This form is invalid or expired."))
	}
	session, err := router.sessions.Get(sessionID, request.UserID, request.GuildID, request.ChannelID)
	if err != nil || session.View != "aircraft" {
		return responder.CreateMessage(errorMessage("The aircraft is no longer attached to this form."))
	}
	cooldown := strings.TrimSpace(request.Values["cooldown"])
	if cooldown != "" {
		minutes, parseErr := strconv.Atoi(cooldown)
		if parseErr != nil || minutes < 0 || minutes > 1440 {
			return responder.CreateMessage(errorMessage("Cooldown must contain only minutes from 0 to 1440."))
		}
	}
	label := render.Truncate(strings.TrimSpace(request.Values["label"]), 64)
	if label == "" {
		label = session.Filter
	}
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Persistent watch storage is unavailable."))
	}
	minutes := 15
	if cooldown != "" {
		minutes, _ = strconv.Atoi(cooldown)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.repository.EnsureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Watch storage is temporarily unavailable."))
	}
	rule, err := router.repository.CreateWatchRule(ctx, domain.WatchRule{GuildID: request.GuildID, UserID: request.UserID, Type: domain.RuleICAO, Value: session.Filter, Enabled: true, Cooldown: time.Duration(minutes) * time.Minute, MinimumObservations: 2})
	if err != nil {
		return responder.CreateMessage(errorMessage("The watch rule could not be saved."))
	}
	router.requestRuleReload()
	return responder.CreateMessage(infoMessage("Watch rule saved", fmt.Sprintf("%s is saved as personal rule %d for aircraft %s.", label, rule.ID, session.Filter)))
}

func (router *Router) HandleAutocomplete(request AutocompleteRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.Autocomplete(nil)
	}
	if request.Name == "watch" {
		return router.autocompleteRules(request, responder)
	}
	snapshot := router.snapshots.Current()
	query := strings.ToUpper(strings.TrimSpace(request.Query))
	choices := make([]disgocord.AutocompleteChoice, 0, 25)
	if snapshot != nil {
		for _, key := range snapshot.Search {
			label := firstNonEmpty(key.Callsign, key.Registration, key.ICAO)
			haystack := key.ICAO + " " + key.Callsign + " " + key.Registration
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			choices = append(choices, disgocord.AutocompleteChoiceString{Name: render.Truncate(label+" • "+key.ICAO, 100), Value: key.ICAO})
			if len(choices) == 25 {
				break
			}
		}
	}
	return responder.Autocomplete(choices)
}

func (router *Router) handleNearby(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "nearby", normalizedSort(request.Strings["sort"]))
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.PageSize = boundedInt(request.Ints["limit"], 1, 25, 10)
	session.RadiusNM = request.Floats["radius-nm"]
	if minFeet, ok := request.Ints["altitude-min"]; ok {
		session.MinFeet, session.HasMin = minFeet, true
	}
	if maxFeet, ok := request.Ints["altitude-max"]; ok {
		session.MaxFeet, session.HasMax = maxFeet, true
	}
	if session.HasMin && session.HasMax && session.MinFeet > session.MaxFeet {
		router.sessions.Delete(session.ID)
		return responder.CreateMessage(errorMessage("Minimum altitude cannot be greater than maximum altitude."))
	}
	if err := router.sessions.Update(session); err != nil {
		return err
	}
	message, err := router.nearbyMessage(session, snapshot)
	if err != nil {
		return err
	}
	return responder.CreateMessage(message)
}

func (router *Router) handleAircraft(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	query := strings.ToUpper(strings.TrimSpace(request.Strings["query"]))
	aircraft, ok := findAircraft(snapshot, query)
	if !ok {
		return responder.CreateMessage(errorMessage("That aircraft is no longer visible. Run `/aircraft` again to choose from current data."))
	}
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "aircraft", aircraft.ICAO)
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	watchID, _ := CustomID(session.ID, "watch")
	refreshID, _ := CustomID(session.ID, "refresh")
	closeID, _ := CustomID(session.ID, "close")
	message := render.SafeMessage(router.aircraftEmbed(aircraft, snapshot), false).
		AddActionRow(disgocord.NewPrimaryButton("Watch", watchID), disgocord.NewSecondaryButton("Refresh", refreshID), disgocord.NewDangerButton("Close", closeID))
	return responder.CreateMessage(message)
}

func (router *Router) updateSession(session Session, responder InteractionResponder) error {
	snapshot := router.snapshots.Current()
	switch session.View {
	case "nearby":
		message, err := router.nearbyMessage(session, snapshot)
		if err != nil {
			return err
		}
		return responder.UpdateMessage(messageUpdate(message))
	case "aircraft":
		aircraft, ok := findAircraft(snapshot, session.Filter)
		if !ok {
			return responder.UpdateMessage(disgocord.NewMessageUpdate().WithContent("This aircraft is no longer visible.").ClearEmbeds().ClearComponents())
		}
		message := render.SafeMessage(router.aircraftEmbed(aircraft, snapshot), false)
		watchID, _ := CustomID(session.ID, "watch")
		refreshID, _ := CustomID(session.ID, "refresh")
		closeID, _ := CustomID(session.ID, "close")
		message = message.AddActionRow(disgocord.NewPrimaryButton("Watch", watchID), disgocord.NewSecondaryButton("Refresh", refreshID), disgocord.NewDangerButton("Close", closeID))
		return responder.UpdateMessage(messageUpdate(message))
	default:
		return responder.CreateMessage(errorMessage("This view is no longer supported."))
	}
}

func (router *Router) aircraftEmbed(aircraft domain.Aircraft, snapshot *domain.Snapshot) disgocord.Embed {
	if router.enrichment != nil {
		value, ok, _ := router.enrichment.Cached(aircraft.ICAO, aircraft.Callsign)
		if ok && value.Found {
			return render.AircraftWithEnrichment(aircraft, snapshot, &value, router.now())
		}
	}
	return render.Aircraft(aircraft, snapshot, router.now())
}

func (router *Router) nearbyMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := filteredAircraft(snapshot, session)
	sortAircraft(aircraft, session.Filter)
	maxPage := max(0, (len(aircraft)-1)/session.PageSize)
	if session.Page > maxPage {
		session.Page = maxPage
		_ = router.sessions.Update(session)
	}
	previousID, err := CustomID(session.ID, "previous")
	if err != nil {
		return disgocord.MessageCreate{}, err
	}
	nextID, _ := CustomID(session.ID, "next")
	refreshID, _ := CustomID(session.ID, "refresh")
	closeID, _ := CustomID(session.ID, "close")
	sortID, _ := CustomID(session.ID, "sort")
	message := render.SafeMessage(render.Nearby(aircraft, session.Page, session.PageSize, router.now()), false)
	message = message.AddActionRow(
		disgocord.NewSecondaryButton("Previous", previousID).WithDisabled(session.Page == 0),
		disgocord.NewSecondaryButton("Next", nextID).WithDisabled(session.Page >= maxPage),
		disgocord.NewPrimaryButton("Refresh", refreshID),
		disgocord.NewDangerButton("Close", closeID),
	)
	menu := disgocord.NewStringSelectMenu(sortID, "Sort aircraft",
		disgocord.NewStringSelectMenuOption("Distance", "distance").WithDefault(session.Filter == "distance"),
		disgocord.NewStringSelectMenuOption("Altitude", "altitude").WithDefault(session.Filter == "altitude"),
		disgocord.NewStringSelectMenuOption("Callsign", "callsign").WithDefault(session.Filter == "callsign"),
	)
	message = message.AddActionRow(menu)
	return message, nil
}

func filteredAircraft(snapshot *domain.Snapshot, session Session) []domain.Aircraft {
	if snapshot == nil {
		return []domain.Aircraft{}
	}
	result := make([]domain.Aircraft, 0, len(snapshot.Aircraft))
	for _, aircraft := range snapshot.Aircraft {
		if session.RadiusNM > 0 && (!aircraft.HasDistance || aircraft.DistanceNM > session.RadiusNM) {
			continue
		}
		if session.HasMin && (!aircraft.HasAltitude || aircraft.AltitudeFeet < session.MinFeet) {
			continue
		}
		if session.HasMax && (!aircraft.HasAltitude || aircraft.AltitudeFeet > session.MaxFeet) {
			continue
		}
		result = append(result, aircraft)
	}
	return result
}

func sortAircraft(aircraft []domain.Aircraft, ordering string) {
	sort.SliceStable(aircraft, func(i, j int) bool {
		switch ordering {
		case "altitude":
			if aircraft[i].HasAltitude != aircraft[j].HasAltitude {
				return aircraft[i].HasAltitude
			}
			if aircraft[i].AltitudeFeet != aircraft[j].AltitudeFeet {
				return aircraft[i].AltitudeFeet < aircraft[j].AltitudeFeet
			}
		case "callsign":
			left, right := firstNonEmpty(aircraft[i].Callsign, aircraft[i].Registration, aircraft[i].ICAO), firstNonEmpty(aircraft[j].Callsign, aircraft[j].Registration, aircraft[j].ICAO)
			if left != right {
				return left < right
			}
		default:
			if aircraft[i].HasDistance != aircraft[j].HasDistance {
				return aircraft[i].HasDistance
			}
			if aircraft[i].DistanceNM != aircraft[j].DistanceNM {
				return aircraft[i].DistanceNM < aircraft[j].DistanceNM
			}
		}
		return aircraft[i].ICAO < aircraft[j].ICAO
	})
}

func findAircraft(snapshot *domain.Snapshot, query string) (domain.Aircraft, bool) {
	if snapshot == nil {
		return domain.Aircraft{}, false
	}
	if aircraft, ok := snapshot.LookupICAO(query); ok {
		return aircraft, true
	}
	for _, aircraft := range snapshot.Aircraft {
		if strings.EqualFold(aircraft.Callsign, query) || strings.EqualFold(aircraft.Registration, query) {
			return aircraft, true
		}
	}
	return domain.Aircraft{}, false
}

func messageUpdate(message disgocord.MessageCreate) disgocord.MessageUpdate {
	update := disgocord.NewMessageUpdate().WithEmbeds(message.Embeds...).WithComponents(message.Components...)
	if message.Content != "" {
		update = update.WithContent(message.Content)
	}
	update.AllowedMentions = message.AllowedMentions
	return update
}

func errorMessage(description string) disgocord.MessageCreate {
	embed := disgocord.NewEmbed().WithTitle("SkyFeed • Error").WithDescription(description).WithColor(render.Caution)
	return render.SafeMessage(embed, true)
}

func infoMessage(title, description string) disgocord.MessageCreate {
	embed := disgocord.NewEmbed().WithTitle("SkyFeed • " + title).WithDescription(description).WithColor(render.Scope)
	return render.SafeMessage(embed, true)
}

func normalizedSort(value string) string {
	if validSort(value) {
		return value
	}
	return "distance"
}

func validSort(value string) bool {
	return value == "distance" || value == "altitude" || value == "callsign"
}

func boundedInt(value, lower, upper, fallback int) int {
	if value == 0 {
		return fallback
	}
	return min(max(value, lower), upper)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Unknown"
}

func (router *Router) acceptsGuild(guildID uint64) bool {
	return router.configuredGuildID != 0 && guildID == router.configuredGuildID
}
