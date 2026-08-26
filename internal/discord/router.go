package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/track"
)

type SnapshotProvider interface {
	Current() *domain.Snapshot
}

type EnrichmentProvider interface {
	Cached(icao, callsign string) (domain.Enrichment, bool, error)
	Enqueue(icao, callsign string) enrichment.AdmissionResult
	Lookup(ctx context.Context, icao, callsign string) (domain.Enrichment, error)
}

type TrackProvider interface {
	Summary(icao string) (track.Summary, error)
	Plot(icao string) ([]byte, track.Summary, error)
}

type AirportActivityProvider interface {
	Activity() domain.AirportActivity
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
	Option     string
	Query      string
	UserID     uint64
	GuildID    uint64
}

// GuildMemberInfo is the guild-scoped membership used to authorize bot DMs.
type GuildMemberInfo struct {
	RoleIDs     []uint64
	Permissions disgocord.Permissions
	Owner       bool
}

type GuildMemberProvider interface {
	GuildMember(ctx context.Context, guildID, userID uint64) (GuildMemberInfo, error)
}

type Router struct {
	snapshots          SnapshotProvider
	sessions           *SessionManager
	configuredGuildID  uint64
	startedAt          time.Time
	now                func() time.Time
	repository         storage.Repository
	members            GuildMemberProvider
	ruleReload         func()
	enrichment         EnrichmentProvider
	routes             RouteProvider
	weather            WeatherProvider
	directory          DirectoryProvider
	privacy            privacy.Disclosure
	testSend           func(context.Context, uint64, string) error
	dashboardReset     func(context.Context) error
	moderation         ModerationExecutor
	domesticCountryISO string
	health             HealthViewer
	enrichmentAudit    EnrichmentAuditor
	routeAudit         RouteAuditor
	tracks             TrackProvider
	activity           AirportActivityProvider
}

func (router *Router) SetRepository(repository storage.Repository) { router.repository = repository }
func (router *Router) SetDomesticCountryISO(countryISO string) {
	router.domesticCountryISO = strings.ToUpper(strings.TrimSpace(countryISO))
}
func (router *Router) SetRuleReload(reload func())               { router.ruleReload = reload }
func (router *Router) SetEnrichment(provider EnrichmentProvider) { router.enrichment = provider }
func (router *Router) SetTestSender(sender func(context.Context, uint64, string) error) {
	router.testSend = sender
}
func (router *Router) SetDashboardReset(reset func(context.Context) error) {
	router.dashboardReset = reset
}
func (router *Router) SetModeration(executor ModerationExecutor) { router.moderation = executor }
func (router *Router) SetTracks(provider TrackProvider)          { router.tracks = provider }
func (router *Router) SetAirportActivity(provider AirportActivityProvider) {
	router.activity = provider
}
func (router *Router) SetGuildMemberProvider(provider GuildMemberProvider) {
	router.members = provider
}

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
	fromDM := request.GuildID == 0
	request.GuildID = router.resolveGuildID(request.GuildID)
	if fromDM {
		if err := router.authorizeDirectMessageAdmin(context.Background(), &request); err != nil {
			return responder.CreateMessage(errorMessage(directMessageAdminOnly))
		}
	}
	snapshot := router.snapshots.Current()
	units := router.effectiveUnits(request.GuildID, request.UserID)
	switch request.Name {
	case "status":
		return responder.CreateMessage(render.SafeMessage(render.StatusWithUnits(snapshot, router.now().Sub(router.startedAt), router.now(), router.enrichment != nil, units), false))
	case "nearby":
		return router.handleNearby(request, responder, snapshot)
	case "aircraft":
		return router.handleAircraft(request, responder, snapshot)
	case "route":
		return router.handleRoute(request, responder, snapshot)
	case "airport":
		return router.handleAirport(request, responder)
	case "squawk":
		return router.handleSquawk(request, responder, snapshot)
	case "emergency":
		return router.handleEmergency(request, responder, snapshot)
	case "traffic":
		return router.handleTraffic(request, responder, snapshot)
	case "top":
		return router.handleTop(request, responder, snapshot)
	case "privacy":
		return router.handlePrivacy(responder)
	case "preferences":
		return router.handlePreferences(request, responder)
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
	case "audit":
		return router.handleAudit(request, responder)
	case "feeder":
		return responder.CreateMessage(render.SafeMessage(render.FeederWithUnits(snapshot, router.now(), units), false))
	case "airline":
		return router.handleAirline(request, responder, snapshot)
	default:
		return responder.CreateMessage(errorMessage("Unknown SkyFeed command."))
	}
}

func (router *Router) effectiveUnits(guildID, userID uint64) domain.UnitSystem {
	if router.repository == nil {
		return domain.UnitsAviation
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if userID != 0 {
		if preference, err := router.repository.UserPreference(ctx, guildID, userID); err == nil {
			return domain.NormalizeUnitSystem(preference.Units)
		}
	}
	if settings, err := router.repository.GuildSettings(ctx, guildID); err == nil {
		return domain.NormalizeUnitSystem(settings.Units)
	}
	return domain.UnitsAviation
}

func (router *Router) handlePreferences(request CommandRequest, responder InteractionResponder) error {
	if router.repository == nil {
		return responder.CreateMessage(errorMessage("Personal preferences are temporarily unavailable."))
	}
	units, ok := domain.ParseUnitSystem(request.Strings["system"])
	if request.Subcommand != "units" || !ok {
		return responder.CreateMessage(errorMessage("Choose aviation or metric units."))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.ensureGuild(ctx, request.GuildID); err != nil {
		return responder.CreateMessage(errorMessage("Personal preferences are temporarily unavailable."))
	}
	if err := router.repository.UpsertUserPreference(ctx, storage.UserPreference{GuildID: request.GuildID, UserID: request.UserID, Units: string(units)}); err != nil {
		return responder.CreateMessage(errorMessage("Your unit preference could not be saved."))
	}
	return responder.CreateMessage(infoMessage("Units updated", fmt.Sprintf("Your SkyFeed views now use %s units. Your preference overrides the server default.", units)))
}

func (router *Router) HandleComponent(request ComponentRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.CreateMessage(errorMessage("SkyFeed is not available in this server."))
	}
	fromDM := request.GuildID == 0
	request.GuildID = router.resolveGuildID(request.GuildID)
	if fromDM {
		commandLike := &CommandRequest{UserID: request.UserID, GuildID: request.GuildID, RoleIDs: request.RoleIDs, Administrator: request.Administrator, Permissions: request.Permissions}
		if err := router.authorizeDirectMessageAdmin(context.Background(), commandLike); err != nil {
			return responder.CreateMessage(errorMessage(directMessageAdminOnly))
		}
		request.RoleIDs = commandLike.RoleIDs
		request.Administrator = commandLike.Administrator
		request.Permissions = commandLike.Permissions
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
		session.Sort = request.Values[0]
		session.Page = 0
	case "route":
		return router.handleAircraftRouteAction(request, responder, session)
	case "details":
		session.Action = "details"
	case "weather-details":
		if session.View != "airport" {
			return responder.CreateMessage(errorMessage("Weather details are not available for this view."))
		}
		session.Action = "weather-details"
	case "airport-activity":
		if session.View != "airport" {
			return responder.CreateMessage(errorMessage("Airport activity is not available for this view."))
		}
		session.Action = "activity"
	case "overview":
		session.Action = ""
	case "track":
		return router.handleAircraftTrackAction(responder, session)
	case "airport":
		if len(request.Values) != 1 {
			return responder.CreateMessage(errorMessage("That airport selection is not valid."))
		}
		return router.handleAirport(CommandRequest{UserID: request.UserID, GuildID: request.GuildID, ChannelID: request.ChannelID, Strings: map[string]string{"code": request.Values[0]}}, responder)
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
	fromDM := request.GuildID == 0
	request.GuildID = router.resolveGuildID(request.GuildID)
	if fromDM {
		commandLike := &CommandRequest{UserID: request.UserID, GuildID: request.GuildID}
		if err := router.authorizeDirectMessageAdmin(context.Background(), commandLike); err != nil {
			return responder.CreateMessage(errorMessage(directMessageAdminOnly))
		}
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
		label = session.Query
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
	rule, err := router.repository.CreateWatchRule(ctx, domain.WatchRule{GuildID: request.GuildID, UserID: request.UserID, Type: domain.RuleICAO, Value: session.Query, Enabled: true, Cooldown: time.Duration(minutes) * time.Minute, MinimumObservations: 2})
	if err != nil {
		return responder.CreateMessage(errorMessage("The watch rule could not be saved."))
	}
	router.requestRuleReload()
	return responder.CreateMessage(infoMessage("Watch rule saved", fmt.Sprintf("%s is saved as personal rule %d for aircraft %s.", label, rule.ID, session.Query)))
}

func (router *Router) HandleAutocomplete(request AutocompleteRequest, responder InteractionResponder) error {
	if !router.acceptsGuild(request.GuildID) {
		return responder.Autocomplete(nil)
	}
	fromDM := request.GuildID == 0
	request.GuildID = router.resolveGuildID(request.GuildID)
	if fromDM {
		commandLike := &CommandRequest{UserID: request.UserID, GuildID: request.GuildID}
		if err := router.authorizeDirectMessageAdmin(context.Background(), commandLike); err != nil {
			return responder.Autocomplete(nil)
		}
	}
	if request.Name == "watch" {
		return router.autocompleteRules(request, responder)
	}
	snapshot := router.snapshots.Current()
	query := strings.ToUpper(strings.TrimSpace(request.Query))
	switch request.Name {
	case "aircraft", "route":
		return router.autocompleteAircraft(request, responder, snapshot, query)
	case "airline":
		return router.autocompleteAirlines(request, responder, snapshot, query)
	case "airport":
		return router.autocompleteAirports(request, responder, query)
	default:
		return responder.Autocomplete(nil)
	}
}

func (router *Router) autocompleteAircraft(request AutocompleteRequest, responder InteractionResponder, snapshot *domain.Snapshot, query string) error {
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

func (router *Router) autocompleteAirlines(_ AutocompleteRequest, responder InteractionResponder, snapshot *domain.Snapshot, query string) error {
	choices := make([]disgocord.AutocompleteChoice, 0, 25)
	seen := make(map[string]struct{})
	add := func(code, label string) {
		if len(choices) >= 25 || code == "" {
			return
		}
		if query != "" && !strings.Contains(code, query) && !strings.Contains(strings.ToUpper(label), query) {
			return
		}
		if _, exists := seen[code]; exists {
			return
		}
		seen[code] = struct{}{}
		choices = append(choices, disgocord.AutocompleteChoiceString{Name: render.Truncate(label+" • "+code, 100), Value: code})
	}
	if snapshot != nil {
		for _, aircraft := range snapshot.Aircraft {
			callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
			if len(callsign) < 2 {
				continue
			}
			prefix := callsign
			if len(prefix) > 3 {
				prefix = prefix[:3]
			}
			if len(prefix) >= 3 && prefix[2] >= '0' && prefix[2] <= '9' {
				prefix = prefix[:2]
			}
			add(prefix, firstNonEmpty(aircraft.Callsign, prefix))
		}
	}
	return responder.Autocomplete(choices)
}

func (router *Router) autocompleteAirports(request AutocompleteRequest, responder InteractionResponder, query string) error {
	choices := make([]disgocord.AutocompleteChoice, 0, 25)
	if router.routes == nil {
		return responder.Autocomplete(choices)
	}
	seen := make(map[string]struct{})
	add := func(code string) {
		if len(choices) >= 25 {
			return
		}
		normalized, ok := enrichment.NormalizeAirportCode(code)
		if !ok {
			return
		}
		if query != "" && !strings.Contains(normalized, query) {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		choices = append(choices, disgocord.AutocompleteChoiceString{Name: normalized, Value: normalized})
	}
	snapshot := router.snapshots.Current()
	if snapshot != nil {
		for _, aircraft := range snapshot.Aircraft {
			if aircraft.Callsign == "" || !aircraft.HasPosition {
				continue
			}
			route, found, _ := router.routes.CachedRoute(strings.ToUpper(strings.TrimSpace(aircraft.Callsign)))
			if !found {
				continue
			}
			add(route.Origin.ICAO)
			add(route.Destination.ICAO)
			if route.Midpoint != nil {
				add(route.Midpoint.ICAO)
			}
		}
	}
	if query != "" {
		add(query)
	}
	return responder.Autocomplete(choices)
}

func (router *Router) handleNearby(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "nearby", normalizedSort(request.Strings["sort"]), "", normalizedSquawk(request.Strings["squawk"]))
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.PageSize = boundedInt(request.Ints["limit"], 1, 25, 10)
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
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
	if query == "" {
		return responder.CreateMessage(errorMessage("That message does not contain a visible aircraft identifier."))
	}
	aircraft, ok := router.resolveAircraft(snapshot, query)
	if !ok {
		if message, found := router.unseenAircraftMessage(query); found {
			return responder.CreateMessage(message)
		}
		return responder.CreateMessage(errorMessage("That aircraft is no longer visible. Run `/aircraft` again to choose from current data."))
	}
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "aircraft", "", aircraft.ICAO, "")
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
	if err := router.sessions.Update(session); err != nil {
		return err
	}
	if err := responder.CreateMessage(router.aircraftMessage(session, aircraft, snapshot)); err != nil {
		return err
	}
	return router.followUpAircraft(session, aircraft, snapshot, responder)
}

func (router *Router) resolveAircraft(snapshot *domain.Snapshot, query string) (domain.Aircraft, bool) {
	if aircraft, ok := findAircraft(snapshot, query); ok {
		return aircraft, true
	}
	if router.directory == nil || !looksLikeNNumber(query) {
		return domain.Aircraft{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hex, err := router.directory.LookupNNumber(ctx, query)
	if err != nil {
		return domain.Aircraft{}, false
	}
	return findAircraft(snapshot, hex)
}

func (router *Router) unseenAircraftMessage(query string) (disgocord.MessageCreate, bool) {
	if router.enrichment == nil || !looksLikeICAO(query) {
		return disgocord.MessageCreate{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := router.enrichment.Lookup(ctx, query, "")
	if err != nil || !value.Found {
		return disgocord.MessageCreate{}, false
	}
	embed := render.AircraftWithEnrichmentAndUnits(domain.Aircraft{ICAO: query}, nil, &value, nil, router.now(), domain.UnitsAviation)
	embed.Description = "Not currently visible to this receiver. Cached ADSBDB metadata is shown."
	return render.SafeMessage(embed, false), true
}

func (router *Router) followUpAircraft(session Session, aircraft domain.Aircraft, snapshot *domain.Snapshot, responder InteractionResponder) error {
	updated := false
	if router.enrichment != nil {
		_, cached, _ := router.enrichment.Cached(aircraft.ICAO, aircraft.Callsign)
		router.enrichment.Enqueue(aircraft.ICAO, aircraft.Callsign)
		if !cached {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := router.enrichment.Lookup(ctx, aircraft.ICAO, aircraft.Callsign); err == nil {
				updated = true
			}
			cancel()
		}
	}
	if router.routes != nil && strings.TrimSpace(aircraft.Callsign) != "" && aircraft.HasPosition {
		callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
		if _, found, _ := router.routes.CachedRoute(callsign); !found {
			routeRequest := enrichment.RouteRequest{Callsign: callsign, Latitude: aircraft.Latitude, Longitude: aircraft.Longitude}
			router.routes.EnqueueRoute(routeRequest)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := router.routes.LookupRoute(ctx, routeRequest); err == nil {
				updated = true
			}
			cancel()
		}
	}
	if !updated {
		return nil
	}
	return responder.UpdateMessage(messageUpdate(router.aircraftMessage(session, aircraft, snapshot)))
}

func (router *Router) handleAircraftRouteAction(request ComponentRequest, responder InteractionResponder, session Session) error {
	snapshot := router.snapshots.Current()
	aircraft, ok := findAircraft(snapshot, session.Query)
	if !ok {
		return responder.CreateMessage(errorMessage("This aircraft is no longer visible."))
	}
	return router.handleRoute(CommandRequest{UserID: request.UserID, GuildID: request.GuildID, ChannelID: request.ChannelID, Strings: map[string]string{"flight": aircraft.ICAO}}, responder, snapshot)
}

func (router *Router) handleAircraftTrackAction(responder InteractionResponder, session Session) error {
	if router.tracks == nil {
		return responder.CreateMessage(errorMessage("Recent track data is unavailable."))
	}
	data, summary, err := router.tracks.Plot(session.Query)
	if err != nil {
		return responder.CreateMessage(errorMessage("There are not enough recent samples to draw this track yet."))
	}
	name := strings.ToLower(summary.ICAO) + "-track.png"
	message := render.SafeMessage(render.TrackSummary(summary, session.Units, router.now()), true).
		WithFiles(disgocord.NewFile(name, "SkyFeed 15-minute local radar track", bytes.NewReader(data)))
	message.Embeds[0].Image = &disgocord.EmbedResource{URL: "attachment://" + name}
	return responder.CreateMessage(message)
}

func (router *Router) aircraftMessage(session Session, aircraft domain.Aircraft, snapshot *domain.Snapshot) disgocord.MessageCreate {
	watchID, _ := CustomID(session.ID, "watch")
	trackID, _ := CustomID(session.ID, "track")
	detailsID, _ := CustomID(session.ID, "details")
	refreshID, _ := CustomID(session.ID, "refresh")
	closeID, _ := CustomID(session.ID, "close")
	embed := render.AircraftSummary(aircraft, snapshot, session.Units, router.now())
	if session.Action == "details" {
		embed = router.aircraftEmbedWithUnits(aircraft, snapshot, session.Units)
	}
	message := render.SafeMessage(embed, false).
		AddActionRow(disgocord.NewPrimaryButton("Details", detailsID).WithDisabled(session.Action == "details"), disgocord.NewSecondaryButton("Track", trackID).WithDisabled(router.tracks == nil), disgocord.NewPrimaryButton("Watch", watchID), disgocord.NewSecondaryButton("Refresh", refreshID), disgocord.NewDangerButton("Close", closeID))
	photoURL := ""
	if router.enrichment != nil {
		if value, ok, _ := router.enrichment.Cached(aircraft.ICAO, aircraft.Callsign); ok && value.Aircraft != nil {
			photoURL = value.Aircraft.PhotoURL
		}
	}
	if links := aircraftLinkButtons(aircraft, photoURL); len(links) > 0 {
		message = message.AddActionRow(links...)
	}
	if router.routes != nil && strings.TrimSpace(aircraft.Callsign) != "" && aircraft.HasPosition {
		routeID, _ := CustomID(session.ID, "route")
		message = message.AddActionRow(disgocord.NewSecondaryButton("Route / Weather", routeID))
		if route, found, _ := router.routes.CachedRoute(strings.ToUpper(strings.TrimSpace(aircraft.Callsign))); found {
			if options := airportSelectOptions(route); len(options) > 0 {
				airportID, _ := CustomID(session.ID, "airport")
				message = message.AddActionRow(disgocord.NewStringSelectMenu(airportID, "Airports", options...))
			}
		}
	}
	return message
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
	case "squawk":
		message, err := router.squawkMessage(session, snapshot)
		if err != nil {
			return err
		}
		return responder.UpdateMessage(messageUpdate(message))
	case "emergency":
		message, err := router.emergencyMessage(session, snapshot)
		if err != nil {
			return err
		}
		return responder.UpdateMessage(messageUpdate(message))
	case "traffic":
		message, err := router.trafficMessage(session, snapshot)
		if err != nil {
			return err
		}
		return responder.UpdateMessage(messageUpdate(message))
	case "aircraft":
		aircraft, ok := findAircraft(snapshot, session.Query)
		if !ok {
			return responder.UpdateMessage(disgocord.NewMessageUpdate().WithContent("This aircraft is no longer visible.").ClearEmbeds().ClearComponents())
		}
		return responder.UpdateMessage(messageUpdate(router.aircraftMessage(session, aircraft, snapshot)))
	case "airport":
		if router.routes == nil {
			return responder.CreateMessage(errorMessage("Airport enrichment is not configured."))
		}
		airport, found, err := router.routes.CachedAirport(session.Query)
		if err != nil || !found {
			return responder.CreateMessage(errorMessage("Airport data is temporarily unavailable. Run `/airport` again."))
		}
		return responder.UpdateMessage(messageUpdate(router.airportMessage(session, airport)))
	default:
		return responder.CreateMessage(errorMessage("This view is no longer supported."))
	}
}

func (router *Router) aircraftEmbedWithUnits(aircraft domain.Aircraft, snapshot *domain.Snapshot, units domain.UnitSystem) disgocord.Embed {
	var enrichmentValue *domain.Enrichment
	if router.enrichment != nil {
		value, ok, _ := router.enrichment.Cached(aircraft.ICAO, aircraft.Callsign)
		if ok && value.Found {
			copyValue := value
			enrichmentValue = &copyValue
		}
	}
	var routeValue *domain.Route
	if router.routes != nil && strings.TrimSpace(aircraft.Callsign) != "" {
		if route, found, _ := router.routes.CachedRoute(strings.ToUpper(strings.TrimSpace(aircraft.Callsign))); found {
			copyRoute := route
			routeValue = &copyRoute
		}
	}
	return router.withRouteWeather(render.AircraftWithEnrichmentAndUnits(aircraft, snapshot, enrichmentValue, routeValue, router.now(), units), routeValue)
}

func (router *Router) nearbyMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := filteredAircraft(snapshot, session)
	sortAircraft(aircraft, session.Sort)
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
	message := render.SafeMessage(render.NearbyWithUnits(aircraft, session.Page, session.PageSize, router.now(), session.Units), false)
	message = message.AddActionRow(
		disgocord.NewSecondaryButton("Previous", previousID).WithDisabled(session.Page == 0),
		disgocord.NewSecondaryButton("Next", nextID).WithDisabled(session.Page >= maxPage),
		disgocord.NewPrimaryButton("Refresh", refreshID),
		disgocord.NewDangerButton("Close", closeID),
	)
	menu := disgocord.NewStringSelectMenu(sortID, "Sort aircraft",
		disgocord.NewStringSelectMenuOption("Distance", "distance").WithDefault(session.Sort == "distance"),
		disgocord.NewStringSelectMenuOption("Altitude", "altitude").WithDefault(session.Sort == "altitude"),
		disgocord.NewStringSelectMenuOption("Callsign", "callsign").WithDefault(session.Sort == "callsign"),
		disgocord.NewStringSelectMenuOption("Ground speed", "speed").WithDefault(session.Sort == "speed"),
		disgocord.NewStringSelectMenuOption("Messages", "messages").WithDefault(session.Sort == "messages"),
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
		if session.Squawk != "" && aircraft.Squawk != session.Squawk {
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
				return aircraft[i].AltitudeFeet > aircraft[j].AltitudeFeet
			}
		case "callsign":
			left, right := firstNonEmpty(aircraft[i].Callsign, aircraft[i].Registration, aircraft[i].ICAO), firstNonEmpty(aircraft[j].Callsign, aircraft[j].Registration, aircraft[j].ICAO)
			if left != right {
				return left < right
			}
		case "speed":
			if aircraft[i].HasGroundSpeed != aircraft[j].HasGroundSpeed {
				return aircraft[i].HasGroundSpeed
			}
			if aircraft[i].GroundSpeedKts != aircraft[j].GroundSpeedKts {
				return aircraft[i].GroundSpeedKts > aircraft[j].GroundSpeedKts
			}
		case "messages":
			if aircraft[i].Messages != aircraft[j].Messages {
				return aircraft[i].Messages > aircraft[j].Messages
			}
		case "signal":
			if aircraft[i].HasRSSI != aircraft[j].HasRSSI {
				return aircraft[i].HasRSSI
			}
			if aircraft[i].RSSI != aircraft[j].RSSI {
				return aircraft[i].RSSI > aircraft[j].RSSI
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
	return value == "distance" || value == "altitude" || value == "callsign" || value == "speed" || value == "messages" || value == "signal"
}

func normalizedSquawk(value string) string {
	value = strings.TrimSpace(value)
	if squawkPattern.MatchString(value) {
		return value
	}
	return ""
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

// airportSelectOptions builds Discord string-select choices for route airports.
// Empty ICAO/IATA codes are skipped — Discord rejects option labels/values outside 1–100 chars.
func airportSelectOptions(route domain.Route) []disgocord.StringSelectMenuOption {
	var options []disgocord.StringSelectMenuOption
	add := func(airport domain.Airport, description string) {
		code := strings.TrimSpace(airport.ICAO)
		if code == "" {
			code = strings.TrimSpace(airport.IATA)
		}
		if code == "" || len(code) > 100 {
			return
		}
		options = append(options, disgocord.NewStringSelectMenuOption(code, code).WithDescription(description))
	}
	add(route.Origin, "Origin")
	add(route.Destination, "Destination")
	if route.Midpoint != nil {
		add(*route.Midpoint, "Midpoint")
	}
	return options
}

func (router *Router) acceptsGuild(guildID uint64) bool {
	if router.configuredGuildID == 0 {
		return false
	}
	// guildID 0 is a bot DM; attribute it to the single configured guild.
	return guildID == 0 || guildID == router.configuredGuildID
}

func (router *Router) resolveGuildID(guildID uint64) uint64 {
	if guildID == 0 {
		return router.configuredGuildID
	}
	return guildID
}

var errDirectMessageAdminOnly = errors.New("direct message requires skyfeed admin")

const directMessageAdminOnly = "Only SkyFeed Admins can use this bot in direct messages. Assign yourself the configured Admin role in the server, or use slash commands in a channel."

func (router *Router) authorizeDirectMessageAdmin(ctx context.Context, request *CommandRequest) error {
	if router.members == nil {
		return errDirectMessageAdminOnly
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	member, err := router.members.GuildMember(ctx, request.GuildID, request.UserID)
	if err != nil {
		return errDirectMessageAdminOnly
	}
	request.RoleIDs = append([]uint64(nil), member.RoleIDs...)
	request.Permissions = member.Permissions
	request.Administrator = member.Owner || member.Permissions.Has(disgocord.PermissionAdministrator)
	request.ManageGuild = request.Administrator || member.Permissions.Has(disgocord.PermissionManageGuild)
	if request.Administrator {
		return nil
	}
	if router.repository == nil || !router.authorizedTier(ctx, request.GuildID, request.RoleIDs, false, "admin") {
		return errDirectMessageAdminOnly
	}
	return nil
}
