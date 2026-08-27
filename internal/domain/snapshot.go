package domain

import "time"

type HealthStatus string

const (
	HealthUnknown  HealthStatus = "unknown"
	HealthHealthy  HealthStatus = "healthy"
	HealthStale    HealthStatus = "stale"
	HealthOffline  HealthStatus = "offline"
	HealthDegraded HealthStatus = "degraded"
	HealthDisabled HealthStatus = "disabled"
)

type SourceHealth struct {
	Provider            ProviderID
	Status              HealthStatus
	LastAttempt         time.Time
	LastSuccess         time.Time
	ConsecutiveFailures uint64
	ErrorClass          string
}

type Health struct {
	Aircraft SourceHealth
	Receiver SourceHealth
	Stats    SourceHealth
}

type Receiver struct {
	Version     string
	Refresh     time.Duration
	HistorySize int
	Latitude    float64
	Longitude   float64
	HasPosition bool
	FetchedAt   time.Time
}

type Statistics struct {
	WindowStart       time.Time
	WindowEnd         time.Time
	Messages          uint64
	MessageRate       float64
	MaxRangeNM        float64
	TrackedAircraft   int
	SingleMessageOnly int
	FetchedAt         time.Time
}

// Snapshot is immutable after publication. Writers must build fresh slices and
// maps; readers load one pointer and never alter its contents.
type Snapshot struct {
	FeederID            FeederID
	Feeders             []FeederSummary
	Sequence            uint64
	ActiveProvider      ProviderID
	ProviderChangedAt   time.Time
	Capabilities        Capabilities
	SourceGeneratedAt   time.Time
	FetchedAt           time.Time
	PublishedAt         time.Time
	Receiver            Receiver
	Statistics          Statistics
	ReceiverMessages    uint64
	MessageCounterValid bool
	Aircraft            []Aircraft
	ByICAO              map[string]int
	Search              []AircraftKey
	Health              Health
}

func (snapshot *Snapshot) LookupICAO(icao string) (Aircraft, bool) {
	if snapshot == nil {
		return Aircraft{}, false
	}
	index, ok := snapshot.ByICAO[icao]
	if !ok || index < 0 || index >= len(snapshot.Aircraft) {
		return Aircraft{}, false
	}
	return snapshot.Aircraft[index], true
}
