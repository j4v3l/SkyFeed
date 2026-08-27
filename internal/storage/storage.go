package storage

import (
	"context"
	"errors"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var (
	ErrEnrollmentInvalid = errors.New("feeder enrollment is invalid, expired, consumed, or revoked")
	ErrSequenceRejected  = errors.New("feeder sequence was rejected")
)

type Feeder struct {
	Descriptor      domain.FeederDescriptor
	GuildID         uint64
	OwnerUserID     uint64
	PublicKey       []byte
	LastSequence    uint64
	LastPayloadHash []byte
	LastSeenAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type FeederEnrollment struct {
	TokenHash []byte
	FeederID  domain.FeederID
	ExpiresAt time.Time
	CreatedAt time.Time
}

type SequenceAcceptance uint8

const (
	SequenceAccepted SequenceAcceptance = iota
	SequenceDuplicate
	SequenceRejected
)

type GuildSettings struct {
	GuildID         uint64
	Units           string
	Timezone        string
	AlertsPaused    bool
	MutedSquawks    string
	DefaultFeederID domain.FeederID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type UserPreference struct {
	GuildID   uint64
	UserID    uint64
	Units     string
	UpdatedAt time.Time
}

type ChannelBinding struct {
	GuildID   uint64
	Purpose   string
	ChannelID uint64
	UpdatedAt time.Time
}

type RoleBinding struct {
	GuildID   uint64
	Tier      string
	RoleID    uint64
	UpdatedAt time.Time
}

type ModerationCase struct {
	ID                    int64
	GuildID               uint64
	ModeratorID           uint64
	TargetUserID          uint64
	Action                string
	Reason                string
	Duration              time.Duration
	DeleteMessageDuration time.Duration
	Status                string
	DMStatus              string
	ErrorCode             string
	CreatedAt             time.Time
	CompletedAt           time.Time
}

type ModerationLog struct {
	ID            int64
	Case          ModerationCase
	Attempts      int
	NextAttemptAt time.Time
	CreatedAt     time.Time
}

type FeederEvent struct {
	GuildID  uint64
	FeederID domain.FeederID
	Kind     string
	Status   string
	Detail   string
	Occurred time.Time
}

type ReportRollup struct {
	GuildID               uint64
	FeederScope           domain.FeederID
	BucketStart           time.Time
	AircraftObservations  int64
	Messages              int64
	EmergencyObservations int64
	EmergencyEvents       int64
	MaximumRange          float64
	PeakTracked           int64
}

type AlertConfig struct {
	GuildID     uint64
	Category    string
	Enabled     bool
	Cooldown    time.Duration
	Destination uint64
	UpdatedAt   time.Time
}

type ReportSchedule struct {
	ID          int64
	GuildID     uint64
	Cadence     string
	Destination uint64
	Enabled     bool
	LastRun     time.Time
	UpdatedAt   time.Time
}

type ReportSummary struct {
	From                  time.Time
	To                    time.Time
	AircraftObservations  int64
	Messages              int64
	EmergencyObservations int64
	EmergencyEvents       int64
	MaximumRangeNM        float64
	PeakTracked           int64
	PeakHour              time.Time
	PeakAircraft          int64
}

type PlaneAlertReference struct {
	ICAO         string
	Registration string
	Operator     string
	AircraftType string
	ICAOType     string
	FlightGroup  string
	Tag1         string
	Tag2         string
	Tag3         string
	Category     string
	Link         string
	ImageLink1   string
	ImageLink2   string
	ImageLink3   string
	ImageLink4   string
	CommitHash   string
	UpdatedAt    time.Time
}

type InterestingSeen struct {
	GuildID     uint64
	ICAO        string
	FirstSeenAt time.Time
	Callsign    string
	FlightGroup string
}

type MessageBinding struct {
	GuildID   uint64
	Purpose   string
	ChannelID uint64
	MessageID uint64
	UpdatedAt time.Time
}

type Repository interface {
	Close() error
	EnsureGuild(context.Context, uint64) error
	UpsertGuildSettings(context.Context, GuildSettings) error
	GuildSettings(context.Context, uint64) (GuildSettings, error)
	UpsertFeeder(context.Context, Feeder) error
	Feeder(context.Context, domain.FeederID) (Feeder, error)
	Feeders(context.Context, uint64, int) ([]Feeder, error)
	CreateFeederEnrollment(context.Context, FeederEnrollment) error
	ConsumeFeederEnrollment(context.Context, []byte, []byte, time.Time) (Feeder, error)
	RevokeFeeder(context.Context, domain.FeederID, time.Time) error
	AcceptFeederSequence(context.Context, domain.FeederID, uint64, []byte, time.Time) (SequenceAcceptance, error)
	UpsertUserPreference(context.Context, UserPreference) error
	UserPreference(context.Context, uint64, uint64) (UserPreference, error)
	UpsertChannelBinding(context.Context, ChannelBinding) error
	ChannelBindings(context.Context, uint64) ([]ChannelBinding, error)
	UpsertRoleBinding(context.Context, RoleBinding) error
	DeleteRoleBinding(context.Context, uint64, string) error
	RoleBindings(context.Context, uint64) ([]RoleBinding, error)
	CreateWatchRule(context.Context, domain.WatchRule) (domain.WatchRule, error)
	UpdateWatchRule(context.Context, domain.WatchRule) error
	DeleteWatchRule(context.Context, int64, uint64) error
	WatchRules(context.Context, uint64, uint64, int) ([]domain.WatchRule, error)
	AllWatchRules(context.Context, uint64, int) ([]domain.WatchRule, error)
	UpsertAlertState(context.Context, domain.AlertState) error
	AlertStates(context.Context, int) ([]domain.AlertState, error)
	PurgeAlertStates(context.Context, time.Time, int) (int64, error)
	AppendFeederEvent(context.Context, FeederEvent) error
	RecentFeederEvents(context.Context, uint64, int) ([]FeederEvent, error)
	AddReportRollup(context.Context, ReportRollup) error
	UpsertAlertConfig(context.Context, AlertConfig) error
	AlertConfigs(context.Context, uint64) ([]AlertConfig, error)
	UpsertReportSchedule(context.Context, ReportSchedule) (ReportSchedule, error)
	ReportSchedules(context.Context, uint64) ([]ReportSchedule, error)
	MarkReportScheduleRun(context.Context, int64, uint64, time.Time) error
	ReportSummary(context.Context, uint64, time.Time, time.Time) (ReportSummary, error)
	ReportSummaryForScope(context.Context, uint64, domain.FeederID, time.Time, time.Time) (ReportSummary, error)
	UpsertMessageBinding(context.Context, MessageBinding) error
	MessageBinding(context.Context, uint64, string) (MessageBinding, bool, error)
	DeleteMessageBinding(context.Context, uint64, string) error
	PlaneAlertReferenceCount(context.Context) (int, error)
	PlaneAlertCommitHash(context.Context) (string, error)
	ReplacePlaneAlertReference(context.Context, []PlaneAlertReference, string) error
	PlaneAlertReferences(context.Context) ([]PlaneAlertReference, error)
	InterestingSeenICAOs(context.Context, uint64) ([]string, error)
	UpsertInterestingSeen(context.Context, InterestingSeen) error
	CreateModerationCase(context.Context, ModerationCase) (ModerationCase, error)
	CompleteModerationCase(context.Context, int64, uint64, string, string, string, time.Time) error
	ModerationCase(context.Context, int64, uint64) (ModerationCase, error)
	ModerationCases(context.Context, uint64, uint64, int) ([]ModerationCase, error)
	PendingModerationLogs(context.Context, time.Time, int) ([]ModerationLog, error)
	MarkModerationLogDelivered(context.Context, int64, time.Time) error
	MarkModerationLogFailed(context.Context, int64, string, time.Time) error
	PurgeModerationCases(context.Context, time.Time, int) (int64, error)
	RecordRouteSightings(context.Context, RouteSightingsBatch) error
	TopRouteRankings(context.Context, uint64, string, string, int, string) ([]RouteRankingRow, error)
	TopRouteRankingsForScope(context.Context, uint64, domain.FeederID, string, string, int, string) ([]RouteRankingRow, error)
	RouteTrafficCounts(context.Context, uint64, time.Time) (RouteTrafficCounts, error)
	AdminDigestLastRun(context.Context, uint64) (time.Time, error)
	MarkAdminDigestRun(context.Context, uint64, time.Time) error
}

type RouteTrafficCounts struct {
	CatalogEntries int64
	Sightings      int64
}

type WriteKind uint8

const (
	WriteAlertState WriteKind = iota
	WriteFeederEvent
	WriteReportRollup
	WriteInterestingSeen
	WriteRouteSightings
)

type WriteEvent struct {
	Kind        WriteKind
	AlertState  domain.AlertState
	Feeder      FeederEvent
	Rollup      ReportRollup
	Interesting InterestingSeen
	RouteBatch  RouteSightingsBatch
}

type BatchSink interface {
	ApplyBatch(context.Context, []WriteEvent) error
}
