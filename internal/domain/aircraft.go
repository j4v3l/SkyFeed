package domain

import "time"

type ProviderID string

const (
	ProviderUnknown       ProviderID = "unknown"
	ProviderReadsb        ProviderID = "readsb"
	ProviderAirplanesLive ProviderID = "airplanes-live"
)

func (provider ProviderID) Known() bool {
	switch provider {
	case ProviderReadsb, ProviderAirplanesLive:
		return true
	default:
		return false
	}
}

type Capability uint8

const (
	CapabilityAircraft Capability = 1 << iota
	CapabilityReceiver
	CapabilityStatistics
)

func (capability Capability) String() string {
	switch capability {
	case CapabilityAircraft:
		return "aircraft"
	case CapabilityReceiver:
		return "receiver"
	case CapabilityStatistics:
		return "stats"
	default:
		return "unknown"
	}
}

type Capabilities uint8

func CapabilitiesOf(capabilities ...Capability) Capabilities {
	var supported Capabilities
	for _, capability := range capabilities {
		supported |= Capabilities(capability)
	}
	return supported
}

func (capabilities Capabilities) Supports(capability Capability) bool {
	return capabilities&Capabilities(capability) != 0
}

type Aircraft struct {
	ICAO            string
	Provider        ProviderID
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
	Provider            ProviderID
	GeneratedAt         time.Time
	Messages            uint64
	MessageCounterValid bool
	Aircraft            []Aircraft
}

type AircraftKey struct {
	ICAO         string
	Callsign     string
	Registration string
}
