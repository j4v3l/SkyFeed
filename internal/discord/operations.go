package discord

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/discord/render"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/weather/aviationweather"
)

var squawkPattern = regexp.MustCompile(`^[0-7]{4}$`)

type RouteProvider interface {
	CachedRoute(callsign string) (domain.Route, bool, error)
	CachedAirport(code string) (domain.Airport, bool, error)
	LookupRoute(ctx context.Context, request enrichment.RouteRequest) (domain.Route, error)
	LookupAirport(ctx context.Context, code string) (domain.Airport, error)
	EnqueueRoute(request enrichment.RouteRequest) bool
	EnqueueAirport(code string) bool
}

type WeatherProvider interface {
	Lookup(ctx context.Context, icao string) (aviationweather.Observation, error)
}

func (router *Router) SetRoutes(provider RouteProvider) { router.routes = provider }
func (router *Router) SetWeather(provider WeatherProvider) {
	router.weather = provider
}
func (router *Router) SetPrivacyDisclosure(disclosure privacy.Disclosure) {
	router.privacy = disclosure.Clone()
}

func (router *Router) handleRoute(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	if router.routes == nil {
		return responder.CreateMessage(errorMessage("Route enrichment is not configured."))
	}
	flight := strings.ToUpper(strings.TrimSpace(request.Strings["flight"]))
	aircraft, ok := findAircraft(snapshot, flight)
	if !ok {
		return responder.CreateMessage(errorMessage("That aircraft is not currently visible. Choose a live aircraft from autocomplete."))
	}
	callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
	if callsign == "" {
		return responder.CreateMessage(errorMessage("This aircraft has no callsign, so a route cannot be resolved."))
	}
	if !aircraft.HasPosition {
		return responder.CreateMessage(errorMessage("This aircraft has no public position, so a route cannot be resolved."))
	}
	routeRequest := enrichment.RouteRequest{Callsign: callsign, Latitude: aircraft.Latitude, Longitude: aircraft.Longitude}
	route, found, err := router.routes.CachedRoute(callsign)
	if err != nil {
		return responder.CreateMessage(errorMessage("Route data is temporarily unavailable. Try again shortly."))
	}
	if found {
		return responder.CreateMessage(render.SafeMessage(render.Route(route, aircraft, router.now()), false))
	}
	_ = responder.CreateMessage(render.SafeMessage(render.LookingUp("Route", callsign, router.now()), false))
	router.routes.EnqueueRoute(routeRequest)
	lookupContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	route, err = router.routes.LookupRoute(lookupContext, routeRequest)
	if errors.Is(err, enrichment.ErrNotFound) {
		return responder.UpdateMessage(messageUpdate(errorMessage("No route is available for this callsign right now.")))
	}
	if err != nil {
		return responder.UpdateMessage(messageUpdate(errorMessage("Route lookup is temporarily unavailable. Try again shortly.")))
	}
	return responder.UpdateMessage(messageUpdate(render.SafeMessage(render.Route(route, aircraft, router.now()), false)))
}

func (router *Router) handleAirport(request CommandRequest, responder InteractionResponder) error {
	if router.routes == nil {
		return responder.CreateMessage(errorMessage("Airport enrichment is not configured."))
	}
	code := strings.ToUpper(strings.TrimSpace(request.Strings["code"]))
	if _, ok := enrichment.NormalizeAirportCode(code); !ok {
		return responder.CreateMessage(errorMessage("Enter a valid four-character ICAO airport code."))
	}
	airport, found, err := router.routes.CachedAirport(code)
	if err != nil {
		return responder.CreateMessage(errorMessage("Airport data is temporarily unavailable. Try again shortly."))
	}
	if !found {
		_ = responder.CreateMessage(render.SafeMessage(render.LookingUp("Airport", code, router.now()), false))
		router.routes.EnqueueAirport(code)
		lookupContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		airport, err = router.routes.LookupAirport(lookupContext, code)
		if errors.Is(err, enrichment.ErrNotFound) {
			return responder.UpdateMessage(messageUpdate(errorMessage("No airport data is available for that code right now.")))
		}
		if err != nil {
			return responder.UpdateMessage(messageUpdate(errorMessage("Airport lookup is temporarily unavailable. Try again shortly.")))
		}
		metar, taf := router.lookupWeather(code)
		return responder.UpdateMessage(messageUpdate(render.SafeMessage(render.AirportWithWeather(airport, metar, taf, router.now()), false)))
	}
	metar, taf := router.lookupWeather(code)
	return responder.CreateMessage(render.SafeMessage(render.AirportWithWeather(airport, metar, taf, router.now()), false))
}

func (router *Router) lookupWeather(code string) (metar, taf string) {
	if router.weather == nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observation, err := router.weather.Lookup(ctx, code)
	if err != nil {
		return "", ""
	}
	return observation.METAR, observation.TAF
}

func (router *Router) handleSquawk(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	code := strings.TrimSpace(request.Strings["code"])
	if !squawkPattern.MatchString(code) {
		return responder.CreateMessage(errorMessage("Squawk codes must be exactly four octal digits (0–7)."))
	}
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "squawk", "", code, "")
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.PageSize = 10
	if err := router.sessions.Update(session); err != nil {
		return err
	}
	message, err := router.squawkMessage(session, snapshot)
	if err != nil {
		return err
	}
	return responder.CreateMessage(message)
}

func (router *Router) handleEmergency(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "emergency", "distance", "", "")
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.PageSize = boundedInt(request.Ints["limit"], 1, 25, 10)
	if err := router.sessions.Update(session); err != nil {
		return err
	}
	message, err := router.emergencyMessage(session, snapshot)
	if err != nil {
		return err
	}
	return responder.CreateMessage(message)
}

func (router *Router) handleTraffic(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	airport := strings.ToUpper(strings.TrimSpace(router.privacy.PublicAirportCode))
	if airport == "" {
		return responder.CreateMessage(errorMessage("No public airport center is configured for traffic views."))
	}
	radius := request.Floats["radius-nm"]
	if radius <= 0 {
		if router.privacy.RadiusNM > 0 {
			radius = float64(router.privacy.RadiusNM)
		} else {
			radius = 50
		}
	}
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "traffic", "distance", airport, "")
	if err != nil {
		return responder.CreateMessage(errorMessage("Too many active views. Close an older SkyFeed view and try again."))
	}
	session.PageSize = boundedInt(request.Ints["limit"], 1, 25, 10)
	session.RadiusNM = radius
	if err := router.sessions.Update(session); err != nil {
		return err
	}
	message, err := router.trafficMessage(session, snapshot)
	if err != nil {
		return err
	}
	return responder.CreateMessage(message)
}

func (router *Router) handleTop(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	metric := normalizedTopMetric(request.Strings["metric"])
	limit := boundedInt(request.Ints["limit"], 1, 25, 10)
	aircraft := topAircraft(snapshot, metric, limit)
	return responder.CreateMessage(render.SafeMessage(render.Top(aircraft, metric, limit, router.now()), false))
}

func (router *Router) handlePrivacy(responder InteractionResponder) error {
	return responder.CreateMessage(render.SafeMessage(render.Privacy(router.privacy), true))
}

func (router *Router) squawkMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := squawkAircraft(snapshot, session.Query)
	sortAircraft(aircraft, "distance")
	return router.pagedAircraftMessage(session, render.Squawk(aircraft, session.Query, session.Page, session.PageSize, router.now()), len(aircraft))
}

func (router *Router) emergencyMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := emergencyAircraft(snapshot)
	sortAircraft(aircraft, session.Sort)
	return router.pagedAircraftMessage(session, render.Emergency(aircraft, session.Page, session.PageSize, router.now()), len(aircraft))
}

func (router *Router) trafficMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := trafficAircraft(snapshot, session.RadiusNM)
	sortAircraft(aircraft, "distance")
	return router.pagedAircraftMessage(session, render.Traffic(aircraft, session.Query, session.RadiusNM, session.Page, session.PageSize, router.now()), len(aircraft))
}

func (router *Router) pagedAircraftMessage(session Session, embed disgocord.Embed, total int) (disgocord.MessageCreate, error) {
	maxPage := max(0, (total-1)/session.PageSize)
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
	message := render.SafeMessage(embed, false)
	message = message.AddActionRow(
		disgocord.NewSecondaryButton("Previous", previousID).WithDisabled(session.Page == 0),
		disgocord.NewSecondaryButton("Next", nextID).WithDisabled(session.Page >= maxPage),
		disgocord.NewPrimaryButton("Refresh", refreshID),
		disgocord.NewDangerButton("Close", closeID),
	)
	return message, nil
}

func squawkAircraft(snapshot *domain.Snapshot, code string) []domain.Aircraft {
	if snapshot == nil || code == "" {
		return []domain.Aircraft{}
	}
	result := make([]domain.Aircraft, 0)
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.Squawk == code {
			result = append(result, aircraft)
		}
	}
	return result
}

func emergencyAircraft(snapshot *domain.Snapshot) []domain.Aircraft {
	if snapshot == nil {
		return []domain.Aircraft{}
	}
	result := make([]domain.Aircraft, 0)
	for _, aircraft := range snapshot.Aircraft {
		emergency := strings.ToLower(strings.TrimSpace(aircraft.Emergency))
		if aircraft.Squawk == "7500" || aircraft.Squawk == "7600" || aircraft.Squawk == "7700" || (emergency != "" && emergency != "none") {
			result = append(result, aircraft)
		}
	}
	return result
}

func trafficAircraft(snapshot *domain.Snapshot, radiusNM float64) []domain.Aircraft {
	if snapshot == nil {
		return []domain.Aircraft{}
	}
	if radiusNM <= 0 {
		radiusNM = 50
	}
	result := make([]domain.Aircraft, 0)
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.HasDistance && aircraft.DistanceNM <= radiusNM {
			result = append(result, aircraft)
		}
	}
	return result
}

func topAircraft(snapshot *domain.Snapshot, metric string, limit int) []domain.Aircraft {
	if snapshot == nil || limit < 1 {
		return []domain.Aircraft{}
	}
	aircraft := append([]domain.Aircraft(nil), snapshot.Aircraft...)
	sortAircraft(aircraft, metric)
	if len(aircraft) > limit {
		aircraft = aircraft[:limit]
	}
	return aircraft
}

func normalizedTopMetric(value string) string {
	switch value {
	case "altitude", "speed", "messages", "signal":
		return value
	default:
		return "distance"
	}
}

func routeSummary(route domain.Route) string {
	origin := firstNonEmpty(route.Origin.IATA, route.Origin.ICAO)
	destination := firstNonEmpty(route.Destination.IATA, route.Destination.ICAO)
	if origin == "" && destination == "" {
		return ""
	}
	if origin == "" {
		origin = "?"
	}
	if destination == "" {
		destination = "?"
	}
	return fmt.Sprintf("%s→%s", origin, destination)
}
