package rules

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	movementCooldown      = 10 * time.Minute
	takeoffVerticalFPM    = 200
	takeoffMaxAltitudeFt  = 5000
	approachRadiusNM      = 8.0
	approachExitRadiusNM  = 12.0
	approachMinAltitudeFt = 500
	approachMaxAltitudeFt = 4000
	approachVerticalFPM   = -300
	approachHeadingDeg    = 90.0
	movementStaleAfter    = 30 * time.Minute
)

type MovementConfig struct {
	Latitude  float64
	Longitude float64
	HasCenter bool
}

type movementState struct {
	onGround     bool
	known        bool
	approaching  bool
	lastTakeoff  time.Time
	lastLanding  time.Time
	lastApproach time.Time
	lastSeen     time.Time
}

type MovementMonitor struct {
	config MovementConfig
	mu     sync.Mutex
	tracks map[string]*movementState
}

func NewMovementMonitor(config MovementConfig) *MovementMonitor {
	return &MovementMonitor{config: config, tracks: make(map[string]*movementState)}
}

func (monitor *MovementMonitor) Evaluate(guildID uint64, snapshot *domain.Snapshot) []domain.Alert {
	if snapshot == nil || guildID == 0 {
		return nil
	}
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = time.Now()
	}
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
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
		if state.known {
			if state.onGround && !aircraft.OnGround && takeoffLikely(aircraft) && now.Sub(state.lastTakeoff) >= movementCooldown {
				state.lastTakeoff = now
				alerts = append(alerts, movementAlert(guildID, aircraft, domain.RuleTakeoff, "Takeoff", movementDescription("Departed the ground", aircraft), now))
			}
			if !state.onGround && aircraft.OnGround && now.Sub(state.lastLanding) >= movementCooldown {
				state.lastLanding = now
				alerts = append(alerts, movementAlert(guildID, aircraft, domain.RuleLanding, "Landing", movementDescription("Touched down", aircraft), now))
			}
		}
		if alert, ok := monitor.evaluateApproach(guildID, aircraft, state, now); ok {
			alerts = append(alerts, alert)
		}
		state.onGround = aircraft.OnGround
		state.known = true
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

func (monitor *MovementMonitor) evaluateApproach(guildID uint64, aircraft domain.Aircraft, state *movementState, now time.Time) (domain.Alert, bool) {
	if !monitor.config.HasCenter || !aircraft.HasPosition || aircraft.OnGround {
		return domain.Alert{}, false
	}
	distance, fromAirport := distanceBearing(monitor.config.Latitude, monitor.config.Longitude, aircraft.Latitude, aircraft.Longitude)
	if distance > approachExitRadiusNM {
		state.approaching = false
		return domain.Alert{}, false
	}
	if state.approaching || now.Sub(state.lastApproach) < movementCooldown {
		return domain.Alert{}, false
	}
	if distance > approachRadiusNM {
		return domain.Alert{}, false
	}
	if !aircraft.HasAltitude || aircraft.AltitudeFeet < approachMinAltitudeFt || aircraft.AltitudeFeet > approachMaxAltitudeFt {
		return domain.Alert{}, false
	}
	if !aircraft.HasVerticalRate || aircraft.VerticalRateFPM > approachVerticalFPM {
		return domain.Alert{}, false
	}
	toAirport := math.Mod(fromAirport+180, 360)
	if aircraft.HasTrack && headingDelta(aircraft.TrackDegrees, toAirport) > approachHeadingDeg {
		return domain.Alert{}, false
	}
	state.approaching = true
	state.lastApproach = now
	description := fmt.Sprintf("Descending toward the public airport center • %.1f NM", distance)
	if aircraft.HasAltitude {
		description += fmt.Sprintf(" • %d ft", aircraft.AltitudeFeet)
	}
	return movementAlert(guildID, aircraft, domain.RuleApproach, "Approach", description, now), true
}

func takeoffLikely(aircraft domain.Aircraft) bool {
	if aircraft.HasVerticalRate && aircraft.VerticalRateFPM >= takeoffVerticalFPM {
		return true
	}
	return aircraft.HasAltitude && aircraft.AltitudeFeet > 0 && aircraft.AltitudeFeet <= takeoffMaxAltitudeFt
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

func movementDescription(prefix string, aircraft domain.Aircraft) string {
	parts := []string{prefix}
	if aircraft.HasAltitude {
		parts = append(parts, fmt.Sprintf("%d ft", aircraft.AltitudeFeet))
	}
	if aircraft.HasVerticalRate {
		parts = append(parts, fmt.Sprintf("%+d fpm", aircraft.VerticalRateFPM))
	}
	if aircraft.HasDistance {
		parts = append(parts, fmt.Sprintf("%.1f NM", aircraft.DistanceNM))
	}
	return strings.Join(parts, " • ")
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
