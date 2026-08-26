package rules

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	movementCooldown      = 10 * time.Minute
	activityRetention     = 10 * time.Minute
	airportGroundRadiusNM = 3.0
	departureRadiusNM     = 8.0
	activityRadiusNM      = 12.0
	takeoffVerticalFPM    = 200
	takeoffMaxAltitudeFt  = 5000
	approachRadiusNM      = 10.0
	approachExitRadiusNM  = 12.0
	approachMinAltitudeFt = 100
	approachMaxAltitudeFt = 5000
	approachVerticalFPM   = -200
	approachHeadingDeg    = 45.0
	minimumRadialSpeedKts = 15.0
	movementStaleAfter    = 30 * time.Minute
)

type MovementConfig struct {
	AirportCode string
	Latitude    float64
	Longitude   float64
	HasCenter   bool
}

type movementState struct {
	onGround        bool
	known           bool
	approaching     bool
	lastTakeoff     time.Time
	lastLanding     time.Time
	lastApproach    time.Time
	lastSeen        time.Time
	lastSample      time.Time
	lastDistanceNM  float64
	hasDistance     bool
	lastAltitudeFt  int
	hasAltitude     bool
	groundNear      bool
	airborneNear    bool
	takeoffMatches  int
	landingMatches  int
	approachMatches int
	phase           domain.MovementPhase
	phaseAt         time.Time
	latest          domain.AirportMovement
}

type MovementMonitor struct {
	config MovementConfig
	mu     sync.Mutex
	tracks map[string]*movementState
	lastAt time.Time
}

func NewMovementMonitor(config MovementConfig) *MovementMonitor {
	return &MovementMonitor{config: config, tracks: make(map[string]*movementState)}
}

func (monitor *MovementMonitor) Evaluate(guildID uint64, snapshot *domain.Snapshot) []domain.Alert {
	if snapshot == nil || guildID == 0 || !monitor.config.HasCenter {
		return nil
	}
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = time.Now()
	}
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	monitor.lastAt = now
	seen := make(map[string]struct{}, len(snapshot.Aircraft))
	alerts := make([]domain.Alert, 0)
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.Provider != domain.ProviderReadsb {
			continue
		}
		icao := strings.ToUpper(strings.TrimSpace(aircraft.ICAO))
		if icao == "" {
			continue
		}
		seen[icao] = struct{}{}
		state := monitor.tracks[icao]
		if state == nil {
			state = &movementState{}
			monitor.tracks[icao] = state
		}
		state.lastSeen = now
		if !aircraft.HasPosition {
			state.approachMatches = 0
			state.takeoffMatches = 0
			state.landingMatches = 0
			state.known = true
			state.onGround = aircraft.OnGround
			continue
		}
		distance, fromAirport := distanceBearing(monitor.config.Latitude, monitor.config.Longitude, aircraft.Latitude, aircraft.Longitude)
		toAirport := math.Mod(fromAirport+180, 360)
		if distance > activityRadiusNM && state.phase == domain.MovementDeparture {
			state.phase = ""
		}
		radialSpeed := 0.0
		hasRadialSpeed := false
		verticalRate := aircraft.VerticalRateFPM
		hasVerticalRate := aircraft.HasVerticalRate
		if state.known && state.hasDistance && !state.lastSample.IsZero() {
			elapsed := now.Sub(state.lastSample)
			if elapsed > 0 && elapsed <= 30*time.Second {
				radialSpeed = (state.lastDistanceNM - distance) / elapsed.Hours()
				hasRadialSpeed = true
				if !hasVerticalRate && aircraft.HasAltitude && state.hasAltitude {
					verticalRate = int(math.Round(float64(aircraft.AltitudeFeet-state.lastAltitudeFt) / elapsed.Minutes()))
					hasVerticalRate = true
				}
			}
		}
		if aircraft.OnGround && distance <= airportGroundRadiusNM {
			state.groundNear = true
		}
		if !aircraft.OnGround && distance <= 5 && (!aircraft.HasAltitude || aircraft.AltitudeFeet <= 2000) {
			state.airborneNear = true
		}
		if alert, ok := monitor.evaluateDeparture(guildID, aircraft, state, distance, fromAirport, radialSpeed, hasRadialSpeed, verticalRate, hasVerticalRate, now); ok {
			alerts = append(alerts, alert)
		}
		if alert, ok := monitor.evaluateLanding(guildID, aircraft, state, distance, verticalRate, hasVerticalRate, now); ok {
			alerts = append(alerts, alert)
		}
		if alert, ok := monitor.evaluateApproach(guildID, aircraft, state, distance, toAirport, radialSpeed, hasRadialSpeed, verticalRate, hasVerticalRate, now); ok {
			alerts = append(alerts, alert)
		}
		monitor.updateActivity(aircraft, state, distance, fromAirport, verticalRate, hasVerticalRate, now)
		state.onGround = aircraft.OnGround
		state.known = true
		state.lastSample = now
		state.lastDistanceNM = distance
		state.hasDistance = true
		state.lastAltitudeFt = aircraft.AltitudeFeet
		state.hasAltitude = aircraft.HasAltitude
	}
	for icao, state := range monitor.tracks {
		if _, ok := seen[icao]; ok {
			continue
		}
		if now.Sub(state.lastSeen) > movementStaleAfter {
			delete(monitor.tracks, icao)
		}
	}
	return alerts
}

func (monitor *MovementMonitor) evaluateApproach(guildID uint64, aircraft domain.Aircraft, state *movementState, distance, toAirport, radialSpeed float64, hasRadialSpeed bool, verticalRate int, hasVerticalRate bool, now time.Time) (domain.Alert, bool) {
	if aircraft.OnGround {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if distance > approachExitRadiusNM {
		state.approaching = false
		if state.phase == domain.MovementApproach {
			state.phase = ""
		}
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if state.approaching || now.Sub(state.lastApproach) < movementCooldown {
		return domain.Alert{}, false
	}
	if distance > approachRadiusNM {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if !aircraft.HasAltitude || aircraft.AltitudeFeet < approachMinAltitudeFt || aircraft.AltitudeFeet > approachMaxAltitudeFt {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if !hasVerticalRate || verticalRate > approachVerticalFPM {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	headingCompatible := aircraft.HasTrack && headingDelta(aircraft.TrackDegrees, toAirport) <= approachHeadingDeg
	closingCompatible := hasRadialSpeed && radialSpeed >= minimumRadialSpeedKts
	if !headingCompatible && !closingCompatible {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if hasRadialSpeed && radialSpeed < -minimumRadialSpeedKts {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	if aircraft.HasGroundSpeed && (aircraft.GroundSpeedKts < 45 || aircraft.GroundSpeedKts > 280) {
		state.approachMatches = 0
		return domain.Alert{}, false
	}
	state.approachMatches++
	if state.approachMatches < 3 {
		return domain.Alert{}, false
	}
	state.approachMatches = 0
	state.approaching = true
	state.lastApproach = now
	state.phase = domain.MovementApproach
	state.phaseAt = now
	description := movementSentence("appears to be approaching", monitor.config.AirportCode, aircraft, distance, verticalRate, hasVerticalRate,
		"It has been descending toward the airport for three consecutive updates.")
	return movementAlert(guildID, aircraft, domain.RuleApproach, "Likely approach", description, now), true
}

func (monitor *MovementMonitor) evaluateDeparture(guildID uint64, aircraft domain.Aircraft, state *movementState, distance, fromAirport, radialSpeed float64, hasRadialSpeed bool, verticalRate int, hasVerticalRate bool, now time.Time) (domain.Alert, bool) {
	if aircraft.OnGround {
		state.takeoffMatches = 0
		return domain.Alert{}, false
	}
	if distance > departureRadiusNM || (!state.groundNear && state.takeoffMatches == 0) {
		state.takeoffMatches = 0
		return domain.Alert{}, false
	}
	climbing := hasVerticalRate && verticalRate >= takeoffVerticalFPM
	if !climbing && aircraft.HasAltitude && state.hasAltitude {
		climbing = aircraft.AltitudeFeet >= state.lastAltitudeFt+75
	}
	movingAway := hasRadialSpeed && radialSpeed <= -minimumRadialSpeedKts
	headingAway := aircraft.HasTrack && headingDelta(aircraft.TrackDegrees, fromAirport) <= approachHeadingDeg
	lowEnough := !aircraft.HasAltitude || aircraft.AltitudeFeet <= takeoffMaxAltitudeFt
	if !climbing || (!movingAway && !headingAway) || !lowEnough {
		state.takeoffMatches = 0
		return domain.Alert{}, false
	}
	state.takeoffMatches++
	if state.takeoffMatches < 3 || now.Sub(state.lastTakeoff) < movementCooldown {
		return domain.Alert{}, false
	}
	state.takeoffMatches = 0
	state.lastTakeoff = now
	state.groundNear = false
	state.phase = domain.MovementDeparture
	state.phaseAt = now
	description := movementSentence("appears to be departing", monitor.config.AirportCode, aircraft, distance, verticalRate, hasVerticalRate,
		"It has been climbing and moving away from the airport for three consecutive updates.")
	return movementAlert(guildID, aircraft, domain.RuleTakeoff, "Likely departure", description, now), true
}

func (monitor *MovementMonitor) evaluateLanding(guildID uint64, aircraft domain.Aircraft, state *movementState, distance float64, verticalRate int, hasVerticalRate bool, now time.Time) (domain.Alert, bool) {
	if !aircraft.OnGround {
		state.landingMatches = 0
		return domain.Alert{}, false
	}
	if distance > airportGroundRadiusNM || (!state.approaching && !state.airborneNear && state.landingMatches == 0) {
		state.landingMatches = 0
		return domain.Alert{}, false
	}
	state.landingMatches++
	if state.landingMatches < 3 || now.Sub(state.lastLanding) < movementCooldown {
		return domain.Alert{}, false
	}
	state.landingMatches = 0
	state.lastLanding = now
	state.approaching = false
	state.airborneNear = false
	state.phase = domain.MovementLanded
	state.phaseAt = now
	description := movementSentence("appears to have landed near", monitor.config.AirportCode, aircraft, distance, verticalRate, hasVerticalRate,
		"It changed from a low airborne track to three consecutive on-ground updates near the airport.")
	return movementAlert(guildID, aircraft, domain.RuleLanding, "Likely landing", description, now), true
}

func movementAlert(guildID uint64, aircraft domain.Aircraft, rule domain.RuleType, title, description string, now time.Time) domain.Alert {
	icao := strings.ToUpper(strings.TrimSpace(aircraft.ICAO))
	return domain.Alert{
		ID:                   fmt.Sprintf("%s:%s:%d", rule, icao, now.UnixNano()),
		GuildID:              guildID,
		AircraftICAO:         icao,
		Callsign:             aircraft.Callsign,
		Type:                 rule,
		Priority:             domain.AlertNormal,
		Title:                title,
		Description:          description,
		ConditionFingerprint: string(rule) + ":" + icao,
		ObservedAt:           now,
	}
}

func movementSentence(action, airport string, aircraft domain.Aircraft, distance float64, verticalRate int, hasVerticalRate bool, evidence string) string {
	identity := strings.TrimSpace(aircraft.Callsign)
	if identity == "" {
		identity = strings.TrimSpace(aircraft.ICAO)
	}
	if airport == "" {
		airport = "the configured airport"
	}
	parts := []string{fmt.Sprintf("%s %s %s.", identity, action, airport)}
	details := make([]string, 0, 4)
	if aircraft.HasAltitude {
		details = append(details, fmt.Sprintf("%d ft", aircraft.AltitudeFeet))
	}
	if hasVerticalRate {
		details = append(details, fmt.Sprintf("%+d ft/min", verticalRate))
	}
	details = append(details, fmt.Sprintf("%.1f NM from the airport", distance))
	if aircraft.HasGroundSpeed {
		details = append(details, fmt.Sprintf("%.0f kt", aircraft.GroundSpeedKts))
	}
	if len(details) > 0 {
		parts = append(parts, strings.Join(details, " · ")+".")
	}
	parts = append(parts, evidence, "This is an ADS-B trend, not an official runway status.")
	return strings.Join(parts, "\n")
}

func (monitor *MovementMonitor) updateActivity(aircraft domain.Aircraft, state *movementState, distance, bearing float64, verticalRate int, hasVerticalRate bool, now time.Time) {
	if state.phase == "" {
		return
	}
	confidence := 90
	evidence := "Three compatible ADS-B updates"
	switch state.phase {
	case domain.MovementApproach:
		confidence = 85
		evidence = "descending and converging on the airport"
	case domain.MovementDeparture:
		evidence = "climbing away after being on the ground nearby"
	case domain.MovementLanded:
		confidence = 95
		evidence = "airborne nearby, then three on-ground updates"
	}
	state.latest = domain.AirportMovement{
		Phase: state.phase, ICAO: aircraft.ICAO, Callsign: aircraft.Callsign, Confidence: confidence,
		DistanceNM: distance, HasDistance: true, BearingDegrees: bearing,
		AltitudeFeet: aircraft.AltitudeFeet, HasAltitude: aircraft.HasAltitude,
		VerticalRateFPM: verticalRate, HasVerticalRate: hasVerticalRate,
		GroundSpeedKts: aircraft.GroundSpeedKts, HasGroundSpeed: aircraft.HasGroundSpeed,
		ObservedAt: now, Evidence: evidence,
	}
}

func (monitor *MovementMonitor) Activity() domain.AirportActivity {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	result := domain.AirportActivity{AirportCode: monitor.config.AirportCode, UpdatedAt: monitor.lastAt, Configured: monitor.config.HasCenter}
	if !monitor.config.HasCenter {
		return result
	}
	for _, state := range monitor.tracks {
		if state.phase == "" || state.latest.ICAO == "" || monitor.lastAt.Sub(state.phaseAt) > activityRetention {
			continue
		}
		result.Movements = append(result.Movements, state.latest)
	}
	sort.Slice(result.Movements, func(left, right int) bool {
		leftPriority := movementPriority(result.Movements[left].Phase)
		rightPriority := movementPriority(result.Movements[right].Phase)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if result.Movements[left].DistanceNM != result.Movements[right].DistanceNM {
			return result.Movements[left].DistanceNM < result.Movements[right].DistanceNM
		}
		return result.Movements[left].ICAO < result.Movements[right].ICAO
	})
	return result
}

func movementPriority(phase domain.MovementPhase) int {
	switch phase {
	case domain.MovementApproach:
		return 0
	case domain.MovementDeparture:
		return 1
	default:
		return 2
	}
}

func distanceBearing(fromLatitude, fromLongitude, toLatitude, toLongitude float64) (float64, float64) {
	const (
		earthRadiusNM = 3440.065
		degreesToRad  = math.Pi / 180
		radiansToDeg  = 180 / math.Pi
	)
	fromLat := fromLatitude * degreesToRad
	toLat := toLatitude * degreesToRad
	deltaLat := (toLatitude - fromLatitude) * degreesToRad
	deltaLon := (toLongitude - fromLongitude) * degreesToRad
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(fromLat)*math.Cos(toLat)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	distance := earthRadiusNM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	y := math.Sin(deltaLon) * math.Cos(toLat)
	x := math.Cos(fromLat)*math.Sin(toLat) - math.Sin(fromLat)*math.Cos(toLat)*math.Cos(deltaLon)
	bearing := math.Mod(math.Atan2(y, x)*radiansToDeg+360, 360)
	return distance, bearing
}

func headingDelta(from, to float64) float64 {
	delta := math.Mod(math.Abs(from-to), 360)
	if delta > 180 {
		delta = 360 - delta
	}
	return delta
}
