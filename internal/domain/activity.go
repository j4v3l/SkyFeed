package domain

import "time"

type MovementPhase string

const (
	MovementApproach  MovementPhase = "approach"
	MovementDeparture MovementPhase = "departure"
	MovementLanded    MovementPhase = "landed"
)

type AirportMovement struct {
	Phase           MovementPhase
	ICAO            string
	Callsign        string
	Confidence      int
	DistanceNM      float64
	HasDistance     bool
	BearingDegrees  float64
	AltitudeFeet    int
	HasAltitude     bool
	VerticalRateFPM int
	HasVerticalRate bool
	GroundSpeedKts  float64
	HasGroundSpeed  bool
	ObservedAt      time.Time
	Evidence        string
}

type AirportActivity struct {
	AirportCode string
	UpdatedAt   time.Time
	Configured  bool
	Movements   []AirportMovement
}
