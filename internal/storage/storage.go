package storage

import (
	"context"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type GuildSettings struct {
	GuildID   uint64
	Units     string
	Timezone  string
	CreatedAt time.Time
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
	Kind     string
	Status   string
	Detail   string
	Occurred time.Time
}

type ReportRollup struct {
	GuildID       uint64
	BucketStart   time.Time
	AircraftSeen  int64
	Messages      int64
	Emergencies   int64
	MaximumRange  float64
	DistinctICAOs int64
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
	From           time.Time
	To             time.Time
	AircraftSeen   int64
	Messages       int64
	Emergencies    int64
	MaximumRangeNM float64
	DistinctICAOs  int64
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
	AppendFeederEvent(context.Context, FeederEvent) error
	RecentFeederEvents(context.Context, uint64, int) ([]FeederEvent, error)
	AddReportRollup(context.Context, ReportRollup) error
	UpsertAlertConfig(context.Context, AlertConfig) error
	AlertConfigs(context.Context, uint64) ([]AlertConfig, error)
	UpsertReportSchedule(context.Context, ReportSchedule) (ReportSchedule, error)
	ReportSchedules(context.Context, uint64) ([]ReportSchedule, error)
	MarkReportScheduleRun(context.Context, int64, uint64, time.Time) error
	ReportSummary(context.Context, uint64, time.Time, time.Time) (ReportSummary, error)
	UpsertMessageBinding(context.Context, MessageBinding) error
	MessageBinding(context.Context, uint64, string) (MessageBinding, bool, error)
	CreateModerationCase(context.Context, ModerationCase) (ModerationCase, error)
	CompleteModerationCase(context.Context, int64, uint64, string, string, string, time.Time) error
	ModerationCase(context.Context, int64, uint64) (ModerationCase, error)
	ModerationCases(context.Context, uint64, uint64, int) ([]ModerationCase, error)
	PendingModerationLogs(context.Context, time.Time, int) ([]ModerationLog, error)
	MarkModerationLogDelivered(context.Context, int64, time.Time) error
	MarkModerationLogFailed(context.Context, int64, string, time.Time) error
	PurgeModerationCases(context.Context, time.Time, int) (int64, error)
}

type WriteKind uint8

const (
	WriteAlertState WriteKind = iota
	WriteFeederEvent
	WriteReportRollup
)

type WriteEvent struct {
	Kind       WriteKind
	AlertState domain.AlertState
	Feeder     FeederEvent
	Rollup     ReportRollup
}

type BatchSink interface {
	ApplyBatch(context.Context, []WriteEvent) error
}
