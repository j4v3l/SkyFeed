package report

import (
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const LiveLeaderMaxAge = 15 * time.Second

type AircraftLeader struct {
	Aircraft domain.Aircraft
	Age      time.Duration
	Found    bool
}

// LiveLeaders is a constant-size view built from one immutable snapshot pass.
// Aircraft may lead more than one category. No input slice or index is retained.
type LiveLeaders struct {
	Fastest  AircraftLeader
	Slowest  AircraftLeader
	Highest  AircraftLeader
	Lowest   AircraftLeader
	Eligible int
}

func SelectLiveLeaders(snapshot *domain.Snapshot, now time.Time) LiveLeaders {
	var leaders LiveLeaders
	if snapshot == nil {
		return leaders
	}
	publicationAge := now.Sub(snapshot.PublishedAt)
	if publicationAge < 0 {
		publicationAge = 0
	}
	for _, aircraft := range snapshot.Aircraft {
		if aircraft.OnGround {
			continue
		}
		age := publicationAge + max(aircraft.Seen, 0)
		if age > LiveLeaderMaxAge {
			continue
		}
		eligible := false
		if aircraft.HasGroundSpeed && aircraft.GroundSpeedKts > 0 {
			eligible = true
			candidate := AircraftLeader{Aircraft: aircraft, Age: age, Found: true}
			if !leaders.Fastest.Found || aircraft.GroundSpeedKts > leaders.Fastest.Aircraft.GroundSpeedKts ||
				(aircraft.GroundSpeedKts == leaders.Fastest.Aircraft.GroundSpeedKts && preferLeader(candidate, leaders.Fastest)) {
				leaders.Fastest = candidate
			}
			if !leaders.Slowest.Found || aircraft.GroundSpeedKts < leaders.Slowest.Aircraft.GroundSpeedKts ||
				(aircraft.GroundSpeedKts == leaders.Slowest.Aircraft.GroundSpeedKts && preferLeader(candidate, leaders.Slowest)) {
				leaders.Slowest = candidate
			}
		}
		if aircraft.HasAltitude {
			eligible = true
			candidate := AircraftLeader{Aircraft: aircraft, Age: age, Found: true}
			if !leaders.Highest.Found || aircraft.AltitudeFeet > leaders.Highest.Aircraft.AltitudeFeet ||
				(aircraft.AltitudeFeet == leaders.Highest.Aircraft.AltitudeFeet && preferLeader(candidate, leaders.Highest)) {
				leaders.Highest = candidate
			}
			if !leaders.Lowest.Found || aircraft.AltitudeFeet < leaders.Lowest.Aircraft.AltitudeFeet ||
				(aircraft.AltitudeFeet == leaders.Lowest.Aircraft.AltitudeFeet && preferLeader(candidate, leaders.Lowest)) {
				leaders.Lowest = candidate
			}
		}
		if eligible {
			leaders.Eligible++
		}
	}
	return leaders
}

func preferLeader(candidate, current AircraftLeader) bool {
	if candidate.Age != current.Age {
		return candidate.Age < current.Age
	}
	return strings.Compare(candidate.Aircraft.ICAO, current.Aircraft.ICAO) < 0
}
