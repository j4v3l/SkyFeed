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
	EnqueueRoute(request enrichment.RouteRequest) enrichment.AdmissionResult
	EnqueueAirport(code string) enrichment.AdmissionResult
}

type WeatherProvider interface {
	Lookup(ctx context.Context, icao string) (aviationweather.Observation, error)
}

type DirectoryProvider interface {
	LookupAirline(ctx context.Context, code string) (domain.Airline, error)
	LookupCallsign(ctx context.Context, callsign string) (domain.Enrichment, error)
	LookupModeS(ctx context.Context, hex string) (string, error)
	LookupNNumber(ctx context.Context, registration string) (string, error)
}

func (router *Router) SetRoutes(provider RouteProvider) { router.routes = provider }
func (router *Router) SetWeather(provider WeatherProvider) {
	router.weather = provider
}
func (router *Router) SetDirectory(provider DirectoryProvider) { router.directory = provider }
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
		return responder.CreateMessage(render.SafeMessage(router.routeEmbed(route, aircraft), false))
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
	return responder.UpdateMessage(messageUpdate(render.SafeMessage(router.routeEmbed(route, aircraft), false)))
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
		message, messageErr := router.newAirportMessage(request, airport)
		if messageErr != nil {
			return responder.UpdateMessage(messageUpdate(errorMessage(messageErr.Error())))
		}
		return responder.UpdateMessage(messageUpdate(message))
	}
	message, err := router.newAirportMessage(request, airport)
	if err != nil {
		return responder.CreateMessage(errorMessage(err.Error()))
	}
	return responder.CreateMessage(message)
}

func (router *Router) newAirportMessage(request CommandRequest, airport domain.Airport) (disgocord.MessageCreate, error) {
	code := strings.ToUpper(strings.TrimSpace(firstNonEmpty(airport.ICAO, request.Strings["code"])))
	session, err := router.sessions.Create(request.UserID, request.GuildID, request.ChannelID, "airport", "", code, "")
	if err != nil {
		return disgocord.MessageCreate{}, errors.New("too many active views; close an older SkyFeed view and try again")
	}
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
	if err := router.sessions.Update(session); err != nil {
		return disgocord.MessageCreate{}, err
	}
	return router.airportMessage(session, airport), nil
}

func (router *Router) airportMessage(session Session, airport domain.Airport) disgocord.MessageCreate {
	detailsID, _ := CustomID(session.ID, "weather-details")
	activityID, _ := CustomID(session.ID, "airport-activity")
	overviewID, _ := CustomID(session.ID, "overview")
	refreshID, _ := CustomID(session.ID, "refresh")
	closeID, _ := CustomID(session.ID, "close")
	weather := router.lookupWeatherView(session.Query)
	activity := domain.AirportActivity{}
	if router.activity != nil {
		candidate := router.activity.Activity()
		if strings.EqualFold(candidate.AirportCode, session.Query) {
			activity = candidate
		}
	}
	return render.SafeMessage(render.AirportDashboard(airport, weather, activity, session.Action, router.now(), session.Units), false).
		AddActionRow(
			disgocord.NewPrimaryButton("Arrivals & departures", activityID).WithDisabled(session.Action == "activity" || !activity.Configured),
			disgocord.NewSecondaryButton("Weather report", detailsID).WithDisabled(session.Action == "weather-details"),
			disgocord.NewSecondaryButton("Overview", overviewID).WithDisabled(session.Action == ""),
			disgocord.NewSecondaryButton("Refresh", refreshID),
			disgocord.NewDangerButton("Close", closeID),
		)
}

func (router *Router) lookupWeatherView(code string) render.WeatherView {
	if router.weather == nil {
		return render.WeatherView{METARStatus: "unavailable", TAFStatus: "unavailable", UpstreamFailed: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observation, err := router.weather.Lookup(ctx, code)
	if err != nil {
		return render.WeatherView{METARStatus: "unavailable", TAFStatus: "unavailable", UpstreamFailed: true}
	}
	clouds := make([]render.WeatherCloudView, 0, len(observation.Clouds))
	for _, cloud := range observation.Clouds {
		clouds = append(clouds, render.WeatherCloudView{Cover: cloud.Cover, BaseFeet: cloud.BaseFeet, HasBase: cloud.HasBase})
	}
	return render.WeatherView{
		METAR: observation.METAR, TAF: observation.TAF, FlightCategory: observation.FlightCategory,
		METARStatus: observation.METARStatus, TAFStatus: observation.TAFStatus, FetchedAt: observation.FetchedAt,
		Stale: observation.Stale, Attribution: observation.Attribution,
		WindDirectionDegrees: observation.WindDirectionDegrees, WindVariable: observation.WindVariable,
		WindSpeedKts: observation.WindSpeedKts, WindGustKts: observation.WindGustKts, HasWind: observation.HasWind,
		VisibilitySM: observation.VisibilitySM, VisibilityAtLeast: observation.VisibilityAtLeast, HasVisibility: observation.HasVisibility,
		TemperatureC: observation.TemperatureC, DewpointC: observation.DewpointC,
		HasTemperature: observation.HasTemperature, HasDewpoint: observation.HasDewpoint,
		AltimeterInHg: observation.AltimeterInHg, HasAltimeter: observation.HasAltimeter,
		Clouds: clouds, Conditions: append([]string(nil), observation.Conditions...),
	}
}

func (router *Router) lookupWeather(code string) (metar, taf string) {
	weather := router.lookupWeatherView(code)
	return weather.METAR, weather.TAF
}

func (router *Router) routeEmbed(route domain.Route, aircraft domain.Aircraft) disgocord.Embed {
	originMETAR, _ := router.lookupWeather(firstNonEmpty(route.Origin.ICAO, route.Origin.IATA))
	destMETAR, _ := router.lookupWeather(firstNonEmpty(route.Destination.ICAO, route.Destination.IATA))
	return render.Route(route, aircraft, originMETAR, destMETAR, router.now())
}

func (router *Router) withRouteWeather(embed disgocord.Embed, route *domain.Route) disgocord.Embed {
	if route == nil {
		return embed
	}
	if origin := firstNonEmpty(route.Origin.ICAO, route.Origin.IATA); origin != "" {
		if metar, _ := router.lookupWeather(origin); metar != "" {
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: "Origin METAR", Value: render.Truncate(render.PlainText(metar), 900)})
		}
	}
	if dest := firstNonEmpty(route.Destination.ICAO, route.Destination.IATA); dest != "" {
		if metar, _ := router.lookupWeather(dest); metar != "" {
			embed.Fields = append(embed.Fields, disgocord.EmbedField{Name: "Destination METAR", Value: render.Truncate(render.PlainText(metar), 900)})
		}
	}
	return render.BoundEmbed(embed)
}

func (router *Router) handleAirline(request CommandRequest, responder InteractionResponder, snapshot *domain.Snapshot) error {
	code := strings.ToUpper(strings.TrimSpace(request.Strings["code"]))
	if len(code) < 2 || len(code) > 3 {
		return responder.CreateMessage(errorMessage("Enter a 2-character IATA or 3-character ICAO airline code."))
	}
	airline := domain.Airline{ICAO: code, IATA: code}
	if router.directory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if value, err := router.directory.LookupAirline(ctx, code); err == nil {
			airline = value
		}
		cancel()
	}
	visible := visibleAirlineFlights(snapshot, airline, code)
	return responder.CreateMessage(render.SafeMessage(render.AirlineWithUnits(airline, visible, router.now(), router.effectiveUnits(request.GuildID, request.UserID)), false))
}

func visibleAirlineFlights(snapshot *domain.Snapshot, airline domain.Airline, code string) []domain.Aircraft {
	if snapshot == nil {
		return nil
	}
	prefixes := make([]string, 0, 2)
	for _, prefix := range []string{strings.ToUpper(strings.TrimSpace(airline.ICAO)), strings.ToUpper(strings.TrimSpace(airline.IATA)), strings.ToUpper(strings.TrimSpace(code))} {
		if prefix == "" {
			continue
		}
		exists := false
		for _, seen := range prefixes {
			if seen == prefix {
				exists = true
				break
			}
		}
		if !exists {
			prefixes = append(prefixes, prefix)
		}
	}
	matches := make([]domain.Aircraft, 0)
	for _, aircraft := range snapshot.Aircraft {
		callsign := strings.ToUpper(strings.TrimSpace(aircraft.Callsign))
		if callsign == "" {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(callsign, prefix) {
				matches = append(matches, aircraft)
				break
			}
		}
	}
	return matches
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
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
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
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
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
	session.Units = router.effectiveUnits(request.GuildID, request.UserID)
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
	if request.Subcommand == "traffic" {
		if !isRouteRankingMetric(metric) {
			return router.respondError(responder, "Choose a historical traffic metric for `/top traffic`.")
		}
		return router.handleRouteTop(request, responder, metric, limit)
	}
	if request.Subcommand != "live" || isRouteRankingMetric(metric) {
		return router.respondError(responder, "Choose a live metric for `/top live`.")
	}
	aircraft := topAircraft(snapshot, metric, limit)
	return responder.CreateMessage(render.SafeMessage(render.TopWithUnits(aircraft, metric, limit, router.now(), router.effectiveUnits(request.GuildID, request.UserID)), false))
}

func (router *Router) handleRouteTop(request CommandRequest, responder InteractionResponder, metric string, limit int) error {
	if router.repository == nil {
		return router.respondError(responder, "Route rankings require SQLite persistence to be enabled.")
	}
	if router.routes == nil {
		return router.respondError(responder, "Route rankings require adsb.lol route enrichment to be enabled.")
	}
	period := normalizedRouteRankingPeriod(request.Strings["period"])
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := router.repository.TopRouteRankings(ctx, request.GuildID, metric, period, limit, router.domesticCountryISO)
	if err != nil {
		return router.respondError(responder, err.Error())
	}
	return responder.CreateMessage(render.SafeMessage(render.TopRouteRankings(metric, period, rows, limit, router.now()), false))
}

func (router *Router) respondError(responder InteractionResponder, description string) error {
	return responder.CreateMessage(render.SafeMessage(disgocord.NewEmbed().WithTitle("SkyFeed • Error").WithDescription(render.PlainText(description)).WithColor(render.Caution), false))
}

func (router *Router) handlePrivacy(responder InteractionResponder) error {
	return responder.CreateMessage(render.SafeMessage(render.Privacy(router.privacy), true))
}

func (router *Router) squawkMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := squawkAircraft(snapshot, session.Query)
	sortAircraft(aircraft, "distance")
	return router.pagedAircraftMessage(session, render.SquawkWithUnits(aircraft, session.Query, session.Page, session.PageSize, router.now(), session.Units), len(aircraft))
}

func (router *Router) emergencyMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := emergencyAircraft(snapshot)
	sortAircraft(aircraft, session.Sort)
	return router.pagedAircraftMessage(session, render.EmergencyWithUnits(aircraft, session.Page, session.PageSize, router.now(), session.Units), len(aircraft))
}

func (router *Router) trafficMessage(session Session, snapshot *domain.Snapshot) (disgocord.MessageCreate, error) {
	aircraft := trafficAircraft(snapshot, session.RadiusNM)
	sortAircraft(aircraft, "distance")
	return router.pagedAircraftMessage(session, render.TrafficWithUnits(aircraft, session.Query, session.RadiusNM, session.Page, session.PageSize, router.now(), session.Units), len(aircraft))
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
		if domain.EmergencyActive(aircraft) {
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
	case "altitude", "speed", "messages", "signal", "routes", "origin-countries", "destination-countries", "airlines", "domestic-airports", "international-airports":
		return value
	default:
		return "distance"
	}
}

func isRouteRankingMetric(metric string) bool {
	switch metric {
	case "routes", "origin-countries", "destination-countries", "airlines", "domestic-airports", "international-airports":
		return true
	default:
		return false
	}
}

func normalizedRouteRankingPeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "7d", "week":
		return "7d"
	case "30d", "month":
		return "30d"
	case "all", "all-time":
		return "all"
	default:
		return "24h"
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
