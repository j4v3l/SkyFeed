package domain

import "time"

type RuleType string

const (
	RuleICAO           RuleType = "icao"
	RuleRegistration   RuleType = "registration"
	RuleCallsign       RuleType = "callsign"
	RuleCallsignPrefix RuleType = "callsign-prefix"
	RuleSquawk         RuleType = "squawk"
	RuleEmergency      RuleType = "emergency"
	RuleRadius         RuleType = "radius"
	RuleAltitude       RuleType = "altitude"
	RuleFirstSeen      RuleType = "first-seen"
	RuleFeeder         RuleType = "feeder"
	RuleOperator       RuleType = "operator"
	RuleOwner          RuleType = "owner"
	RuleAircraftType   RuleType = "aircraft-type"
	RuleInteresting    RuleType = "interesting"
	RuleTakeoff        RuleType = "takeoff"
	RuleLanding        RuleType = "landing"
	RuleApproach       RuleType = "approach"
)

type WatchRule struct {
	ID                   int64
	GuildID              uint64
	FeederScope          FeederID
	UserID               uint64
	ServerScope          bool
	Type                 RuleType
	Value                string
	Enabled              bool
	Cooldown             time.Duration
	MinimumObservations  int
	EnterThreshold       float64
	ExitThreshold        float64
	BestEffortEnrichment bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AlertPriority uint8

const (
	AlertNormal AlertPriority = iota
	AlertEmergency
)

type Alert struct {
	ID                   string
	RuleID               int64
	GuildID              uint64
	FeederID             FeederID
	UserID               uint64
	AircraftICAO         string
	Callsign             string
	ConditionFingerprint string
	Type                 RuleType
	Priority             AlertPriority
	Title                string
	Description          string
	RouteSummary         string
	InterestingGroup     string
	InterestingOperator  string
	InterestingTags      string
	InterestingLink      string
	InterestingImage     string
	ObservedAt           time.Time
	Cooldown             time.Duration
}

type AlertState struct {
	RuleID               int64
	FeederScope          FeederID
	AircraftICAO         string
	ConditionFingerprint string
	LastFiredAt          time.Time
	LastClearAt          time.Time
	ConsecutiveMatches   int
	Active               bool
}
