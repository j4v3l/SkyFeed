package domain

import "time"

type Aircraft struct {
	ICAO            string
	SourceType      string
	Callsign        string
	Registration    string
	AircraftType    string
	Category        string
	Squawk          string
	Emergency       string
	Latitude        float64
	Longitude       float64
	HasPosition     bool
	AltitudeFeet    int
	HasAltitude     bool
	OnGround        bool
	GroundSpeedKts  float64
	HasGroundSpeed  bool
	TrackDegrees    float64
	HasTrack        bool
	VerticalRateFPM int
	HasVerticalRate bool
	Messages        uint64
	Seen            time.Duration
	SeenPosition    time.Duration
	RSSI            float64
	HasRSSI         bool
	DistanceNM      float64
	BearingDegrees  float64
	HasDistance     bool
}

type AircraftBatch struct {
	GeneratedAt time.Time
	Messages    uint64
	Aircraft    []Aircraft
}

type AircraftKey struct {
	ICAO         string
	Callsign     string
	Registration string
}
