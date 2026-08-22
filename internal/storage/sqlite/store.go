package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("storage record not found")

const (
	moderationCaseSelect  = `SELECT id, guild_id, moderator_id, target_user_id, action, reason, duration_seconds, delete_message_seconds, status, dm_status, error_code, created_at, completed_at FROM moderation_cases`
	moderationCaseColumns = `c.id, c.guild_id, c.moderator_id, c.target_user_id, c.action, c.reason, c.duration_seconds, c.delete_message_seconds, c.status, c.dm_status, c.error_code, c.created_at, c.completed_at`
)

type rowScanner interface {
	Scan(...any) error
}

type Store struct{ db *sql.DB }

func Open(ctx context.Context, databasePath string) (*Store, error) {
	if !filepath.IsAbs(databasePath) {
		return nil, errors.New("SQLite database path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, fmt.Errorf("create SQLite directory: %w", err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`, `PRAGMA synchronous=NORMAL`} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure SQLite: %w", err)
		}
	}
	if err := storage.ApplyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) EnsureGuild(ctx context.Context, guildID uint64) error {
	now := formatTime(time.Now().UTC())
	_, err := store.db.ExecContext(ctx, `INSERT INTO guild_settings(guild_id, units, timezone, created_at, updated_at) VALUES (?, 'aviation', 'UTC', ?, ?) ON CONFLICT(guild_id) DO NOTHING`, guildID, now, now)
	return wrap("ensure guild", err)
}

func (store *Store) UpsertGuildSettings(ctx context.Context, settings storage.GuildSettings) error {
	now := settings.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	created := settings.CreatedAt.UTC()
	if created.IsZero() {
		created = now
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO guild_settings(guild_id, units, timezone, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(guild_id) DO UPDATE SET units=excluded.units, timezone=excluded.timezone, updated_at=excluded.updated_at`, settings.GuildID, valueOr(settings.Units, "aviation"), valueOr(settings.Timezone, "UTC"), formatTime(created), formatTime(now))
	return wrap("upsert guild settings", err)
}

func (store *Store) GuildSettings(ctx context.Context, guildID uint64) (storage.GuildSettings, error) {
	var settings storage.GuildSettings
	var created, updated string
	err := store.db.QueryRowContext(ctx, `SELECT guild_id, units, timezone, created_at, updated_at FROM guild_settings WHERE guild_id=?`, guildID).Scan(&settings.GuildID, &settings.Units, &settings.Timezone, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.GuildSettings{}, ErrNotFound
	}
	if err != nil {
		return storage.GuildSettings{}, fmt.Errorf("get guild settings: %w", err)
	}
	settings.CreatedAt, err = parseTime(created)
	if err == nil {
		settings.UpdatedAt, err = parseTime(updated)
	}
	return settings, err
}

func (store *Store) UpsertChannelBinding(ctx context.Context, binding storage.ChannelBinding) error {
	now := binding.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO channel_bindings(guild_id, purpose, channel_id, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(guild_id, purpose) DO UPDATE SET channel_id=excluded.channel_id, updated_at=excluded.updated_at`, binding.GuildID, binding.Purpose, binding.ChannelID, formatTime(now))
	return wrap("upsert channel binding", err)
}

func (store *Store) ChannelBindings(ctx context.Context, guildID uint64) ([]storage.ChannelBinding, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT guild_id, purpose, channel_id, updated_at FROM channel_bindings WHERE guild_id=? ORDER BY purpose LIMIT 20`, guildID)
	if err != nil {
		return nil, fmt.Errorf("list channel bindings: %w", err)
	}
	defer rows.Close()
	result := make([]storage.ChannelBinding, 0, 5)
	for rows.Next() {
		var binding storage.ChannelBinding
		var updated string
		if err := rows.Scan(&binding.GuildID, &binding.Purpose, &binding.ChannelID, &updated); err != nil {
			return nil, fmt.Errorf("scan channel binding: %w", err)
		}
		binding.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		result = append(result, binding)
	}
	return result, rows.Err()
}

func (store *Store) UpsertRoleBinding(ctx context.Context, binding storage.RoleBinding) error {
	now := binding.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO role_bindings(guild_id, purpose, role_id, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(guild_id, purpose) DO UPDATE SET role_id=excluded.role_id, updated_at=excluded.updated_at`, binding.GuildID, binding.Tier, binding.RoleID, formatTime(now))
	return wrap("upsert role binding", err)
}

func (store *Store) DeleteRoleBinding(ctx context.Context, guildID uint64, tier string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM role_bindings WHERE guild_id=? AND purpose=?`, guildID, tier)
	if err != nil {
		return fmt.Errorf("delete role binding: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) RoleBindings(ctx context.Context, guildID uint64) ([]storage.RoleBinding, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT guild_id, purpose, role_id, updated_at FROM role_bindings WHERE guild_id=? ORDER BY purpose LIMIT 10`, guildID)
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}
	defer rows.Close()
	result := make([]storage.RoleBinding, 0, 3)
	for rows.Next() {
		var binding storage.RoleBinding
		var updated string
		if err := rows.Scan(&binding.GuildID, &binding.Tier, &binding.RoleID, &updated); err != nil {
			return nil, fmt.Errorf("scan role binding: %w", err)
		}
		binding.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		result = append(result, binding)
	}
	return result, rows.Err()
}

func (store *Store) CreateWatchRule(ctx context.Context, rule domain.WatchRule) (domain.WatchRule, error) {
	now := time.Now().UTC()
	if !rule.CreatedAt.IsZero() {
		now = rule.CreatedAt.UTC()
	}
	if rule.Cooldown == 0 {
		rule.Cooldown = 15 * time.Minute
	}
	if rule.MinimumObservations == 0 {
		rule.MinimumObservations = 2
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO watch_rules(guild_id, user_id, server_scope, rule_type, rule_value, enabled, cooldown_seconds, minimum_observations, enter_threshold, exit_threshold, best_effort_enrichment, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, rule.GuildID, rule.UserID, rule.ServerScope, rule.Type, strings.ToUpper(strings.TrimSpace(rule.Value)), rule.Enabled, int64(rule.Cooldown/time.Second), rule.MinimumObservations, rule.EnterThreshold, rule.ExitThreshold, rule.BestEffortEnrichment, formatTime(now), formatTime(now))
	if err != nil {
		return domain.WatchRule{}, fmt.Errorf("create watch rule: %w", err)
	}
	rule.ID, err = result.LastInsertId()
	if err != nil {
		return domain.WatchRule{}, fmt.Errorf("watch rule ID: %w", err)
	}
	rule.Value = strings.ToUpper(strings.TrimSpace(rule.Value))
	rule.CreatedAt, rule.UpdatedAt = now, now
	return rule, nil
}

func (store *Store) UpdateWatchRule(ctx context.Context, rule domain.WatchRule) error {
	now := time.Now().UTC()
	result, err := store.db.ExecContext(ctx, `UPDATE watch_rules SET rule_value=?, enabled=?, cooldown_seconds=?, minimum_observations=?, enter_threshold=?, exit_threshold=?, updated_at=? WHERE id=? AND guild_id=?`, strings.ToUpper(strings.TrimSpace(rule.Value)), rule.Enabled, int64(rule.Cooldown/time.Second), rule.MinimumObservations, rule.EnterThreshold, rule.ExitThreshold, formatTime(now), rule.ID, rule.GuildID)
	if err != nil {
		return fmt.Errorf("update watch rule: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) DeleteWatchRule(ctx context.Context, id int64, guildID uint64) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM watch_rules WHERE id=? AND guild_id=?`, id, guildID)
	if err != nil {
		return fmt.Errorf("delete watch rule: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) WatchRules(ctx context.Context, guildID, userID uint64, limit int) ([]domain.WatchRule, error) {
	return store.watchRules(ctx, `guild_id=? AND (server_scope=1 OR user_id=?)`, []any{guildID, userID}, limit)
}

func (store *Store) AllWatchRules(ctx context.Context, guildID uint64, limit int) ([]domain.WatchRule, error) {
	return store.watchRules(ctx, `guild_id=?`, []any{guildID}, limit)
}

func (store *Store) watchRules(ctx context.Context, predicate string, arguments []any, limit int) ([]domain.WatchRule, error) {
	limit = min(max(limit, 1), 500)
	arguments = append(arguments, limit)
	query := `SELECT id, guild_id, user_id, server_scope, rule_type, rule_value, enabled, cooldown_seconds, minimum_observations, enter_threshold, exit_threshold, best_effort_enrichment, created_at, updated_at FROM watch_rules WHERE ` + predicate + ` ORDER BY id LIMIT ?`
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list watch rules: %w", err)
	}
	defer rows.Close()
	result := make([]domain.WatchRule, 0, min(limit, 32))
	for rows.Next() {
		var rule domain.WatchRule
		var cooldown int64
		var created, updated string
		if err := rows.Scan(&rule.ID, &rule.GuildID, &rule.UserID, &rule.ServerScope, &rule.Type, &rule.Value, &rule.Enabled, &cooldown, &rule.MinimumObservations, &rule.EnterThreshold, &rule.ExitThreshold, &rule.BestEffortEnrichment, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan watch rule: %w", err)
		}
		rule.Cooldown = time.Duration(cooldown) * time.Second
		rule.CreatedAt, err = parseTime(created)
		if err == nil {
			rule.UpdatedAt, err = parseTime(updated)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (store *Store) UpsertAlertState(ctx context.Context, state domain.AlertState) error {
	if state.RuleID <= 0 {
		_, err := store.db.ExecContext(ctx, `INSERT INTO system_alert_state(rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(rule_id, aircraft_icao, condition_fingerprint) DO UPDATE SET last_fired_at=excluded.last_fired_at, last_clear_at=excluded.last_clear_at, consecutive_matches=excluded.consecutive_matches, active=excluded.active`, state.RuleID, state.AircraftICAO, state.ConditionFingerprint, nullableTime(state.LastFiredAt), nullableTime(state.LastClearAt), state.ConsecutiveMatches, state.Active)
		return wrap("upsert system alert state", err)
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO alert_state(rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(rule_id, aircraft_icao, condition_fingerprint) DO UPDATE SET last_fired_at=excluded.last_fired_at, last_clear_at=excluded.last_clear_at, consecutive_matches=excluded.consecutive_matches, active=excluded.active`, state.RuleID, state.AircraftICAO, state.ConditionFingerprint, nullableTime(state.LastFiredAt), nullableTime(state.LastClearAt), state.ConsecutiveMatches, state.Active)
	return wrap("upsert alert state", err)
}

func (store *Store) AlertStates(ctx context.Context, limit int) ([]domain.AlertState, error) {
	limit = min(max(limit, 1), 10_000)
	rows, err := store.db.QueryContext(ctx, `SELECT rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active FROM (
SELECT rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active FROM alert_state
UNION ALL
SELECT rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active FROM system_alert_state
) ORDER BY rule_id, aircraft_icao LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list alert states: %w", err)
	}
	defer rows.Close()
	result := make([]domain.AlertState, 0, min(limit, 64))
	for rows.Next() {
		var state domain.AlertState
		var fired, cleared sql.NullString
		if err := rows.Scan(&state.RuleID, &state.AircraftICAO, &state.ConditionFingerprint, &fired, &cleared, &state.ConsecutiveMatches, &state.Active); err != nil {
			return nil, err
		}
		if fired.Valid {
			state.LastFiredAt, err = parseTime(fired.String)
		}
		if err == nil && cleared.Valid {
			state.LastClearAt, err = parseTime(cleared.String)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, rows.Err()
}

func (store *Store) AppendFeederEvent(ctx context.Context, event storage.FeederEvent) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO feeder_events(guild_id, kind, status, detail, occurred_at) VALUES (?, ?, ?, ?, ?)`, event.GuildID, event.Kind, event.Status, event.Detail, formatTime(event.Occurred.UTC()))
	return wrap("append feeder event", err)
}

func (store *Store) RecentFeederEvents(ctx context.Context, guildID uint64, limit int) ([]storage.FeederEvent, error) {
	limit = min(max(limit, 1), 100)
	rows, err := store.db.QueryContext(ctx, `SELECT guild_id, kind, status, detail, occurred_at FROM feeder_events WHERE guild_id=? ORDER BY occurred_at DESC, id DESC LIMIT ?`, guildID, limit)
	if err != nil {
		return nil, fmt.Errorf("list feeder events: %w", err)
	}
	defer rows.Close()
	result := make([]storage.FeederEvent, 0, min(limit, 10))
	for rows.Next() {
		var event storage.FeederEvent
		var occurred string
		if err := rows.Scan(&event.GuildID, &event.Kind, &event.Status, &event.Detail, &occurred); err != nil {
			return nil, err
		}
		event.Occurred, err = parseTime(occurred)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (store *Store) AddReportRollup(ctx context.Context, rollup storage.ReportRollup) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO report_rollups(guild_id, bucket_start, aircraft_seen, messages, emergencies, maximum_range, distinct_icaos) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(guild_id, bucket_start) DO UPDATE SET aircraft_seen=aircraft_seen+excluded.aircraft_seen, messages=messages+excluded.messages, emergencies=emergencies+excluded.emergencies, maximum_range=MAX(maximum_range, excluded.maximum_range), distinct_icaos=MAX(distinct_icaos, excluded.distinct_icaos)`, rollup.GuildID, formatTime(rollup.BucketStart.UTC()), rollup.AircraftSeen, rollup.Messages, rollup.Emergencies, rollup.MaximumRange, rollup.DistinctICAOs)
	return wrap("add report rollup", err)
}

func (store *Store) UpsertAlertConfig(ctx context.Context, value storage.AlertConfig) error {
	now := value.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO alert_configs(guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(guild_id, category) DO UPDATE SET enabled=excluded.enabled, cooldown_seconds=excluded.cooldown_seconds, destination_channel_id=excluded.destination_channel_id, updated_at=excluded.updated_at`, value.GuildID, value.Category, value.Enabled, int64(value.Cooldown/time.Second), value.Destination, formatTime(now))
	return wrap("upsert alert config", err)
}

func (store *Store) AlertConfigs(ctx context.Context, guildID uint64) ([]storage.AlertConfig, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at FROM alert_configs WHERE guild_id=? ORDER BY category LIMIT 10`, guildID)
	if err != nil {
		return nil, fmt.Errorf("list alert configs: %w", err)
	}
	defer rows.Close()
	result := make([]storage.AlertConfig, 0, 3)
	for rows.Next() {
		var value storage.AlertConfig
		var cooldown int64
		var updated string
		if err := rows.Scan(&value.GuildID, &value.Category, &value.Enabled, &cooldown, &value.Destination, &updated); err != nil {
			return nil, err
		}
		value.Cooldown = time.Duration(cooldown) * time.Second
		value.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store *Store) UpsertReportSchedule(ctx context.Context, value storage.ReportSchedule) (storage.ReportSchedule, error) {
	now := value.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lastRun := value.LastRun.UTC()
	if lastRun.IsZero() {
		lastRun = now
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO report_schedules(guild_id, cadence, destination_channel_id, enabled, updated_at, last_run_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(guild_id, cadence, destination_channel_id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at`, value.GuildID, value.Cadence, value.Destination, value.Enabled, formatTime(now), formatTime(lastRun))
	if err != nil {
		return storage.ReportSchedule{}, fmt.Errorf("upsert report schedule: %w", err)
	}
	if value.ID == 0 {
		value.ID, _ = result.LastInsertId()
	}
	value.UpdatedAt, value.LastRun = now, lastRun
	return value, nil
}

func (store *Store) ReportSchedules(ctx context.Context, guildID uint64) ([]storage.ReportSchedule, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id, guild_id, cadence, destination_channel_id, enabled, updated_at, last_run_at FROM report_schedules WHERE guild_id=? ORDER BY id LIMIT 100`, guildID)
	if err != nil {
		return nil, fmt.Errorf("list report schedules: %w", err)
	}
	defer rows.Close()
	result := make([]storage.ReportSchedule, 0, 8)
	for rows.Next() {
		var value storage.ReportSchedule
		var updated string
		var lastRun sql.NullString
		if err := rows.Scan(&value.ID, &value.GuildID, &value.Cadence, &value.Destination, &value.Enabled, &updated, &lastRun); err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTime(updated)
		if err == nil && lastRun.Valid {
			value.LastRun, err = parseTime(lastRun.String)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store *Store) MarkReportScheduleRun(ctx context.Context, id int64, guildID uint64, ranAt time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE report_schedules SET last_run_at=? WHERE id=? AND guild_id=?`, formatTime(ranAt.UTC()), id, guildID)
	if err != nil {
		return fmt.Errorf("mark report schedule run: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) ReportSummary(ctx context.Context, guildID uint64, from, to time.Time) (storage.ReportSummary, error) {
	result := storage.ReportSummary{From: from, To: to}
	err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(aircraft_seen), 0), COALESCE(SUM(messages), 0), COALESCE(SUM(emergencies), 0), COALESCE(MAX(maximum_range), 0), COALESCE(MAX(distinct_icaos), 0)
FROM report_rollups WHERE guild_id=? AND bucket_start>=? AND bucket_start<?`, guildID, formatTime(from.UTC()), formatTime(to.UTC())).Scan(&result.AircraftSeen, &result.Messages, &result.Emergencies, &result.MaximumRangeNM, &result.DistinctICAOs)
	if err != nil {
		return storage.ReportSummary{}, fmt.Errorf("report summary: %w", err)
	}
	return result, nil
}

func (store *Store) UpsertMessageBinding(ctx context.Context, value storage.MessageBinding) error {
	now := value.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO message_bindings(guild_id, purpose, channel_id, message_id, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(guild_id, purpose) DO UPDATE SET channel_id=excluded.channel_id, message_id=excluded.message_id, updated_at=excluded.updated_at`, value.GuildID, value.Purpose, value.ChannelID, value.MessageID, formatTime(now))
	return wrap("upsert message binding", err)
}

func (store *Store) MessageBinding(ctx context.Context, guildID uint64, purpose string) (storage.MessageBinding, bool, error) {
	var value storage.MessageBinding
	var updated string
	err := store.db.QueryRowContext(ctx, `SELECT guild_id, purpose, channel_id, message_id, updated_at FROM message_bindings WHERE guild_id=? AND purpose=?`, guildID, purpose).Scan(&value.GuildID, &value.Purpose, &value.ChannelID, &value.MessageID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.MessageBinding{}, false, nil
	}
	if err != nil {
		return storage.MessageBinding{}, false, fmt.Errorf("get message binding: %w", err)
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err == nil, err
}

func (store *Store) CreateModerationCase(ctx context.Context, value storage.ModerationCase) (storage.ModerationCase, error) {
	now := value.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO moderation_cases(guild_id, moderator_id, target_user_id, action, reason, duration_seconds, delete_message_seconds, status, dm_status, error_code, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 'not-attempted', '', ?)`, value.GuildID, value.ModeratorID, value.TargetUserID, value.Action, strings.TrimSpace(value.Reason), int64(value.Duration/time.Second), int64(value.DeleteMessageDuration/time.Second), formatTime(now))
	if err != nil {
		return storage.ModerationCase{}, fmt.Errorf("create moderation case: %w", err)
	}
	value.ID, err = result.LastInsertId()
	if err != nil {
		return storage.ModerationCase{}, fmt.Errorf("moderation case ID: %w", err)
	}
	value.Status = "pending"
	value.DMStatus = "not-attempted"
	value.CreatedAt = now
	return value, nil
}

func (store *Store) CompleteModerationCase(ctx context.Context, id int64, guildID uint64, status, dmStatus, errorCode string, completedAt time.Time) error {
	completedAt = completedAt.UTC()
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin moderation completion: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE moderation_cases SET status=?, dm_status=?, error_code=?, completed_at=? WHERE id=? AND guild_id=? AND status='pending'`, status, dmStatus, truncateStorage(errorCode, 160), formatTime(completedAt), id, guildID)
	if err == nil {
		var changed int64
		changed, err = result.RowsAffected()
		if err == nil && changed == 0 {
			err = ErrNotFound
		}
	}
	if err == nil {
		_, err = transaction.ExecContext(ctx, `DELETE FROM moderation_log_outbox WHERE id=(
SELECT id FROM moderation_log_outbox ORDER BY CASE WHEN delivered_at IS NOT NULL THEN 0 ELSE 1 END, created_at, id LIMIT 1
) AND (SELECT COUNT(*) FROM moderation_log_outbox)>=10000`)
	}
	if err == nil {
		_, err = transaction.ExecContext(ctx, `INSERT INTO moderation_log_outbox(case_id, guild_id, attempts, next_attempt_at, delivered_at, last_error, created_at) VALUES (?, ?, 0, ?, NULL, '', ?)`, id, guildID, formatTime(completedAt), formatTime(completedAt))
	}
	if err != nil {
		_ = transaction.Rollback()
		return fmt.Errorf("complete moderation case: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit moderation completion: %w", err)
	}
	return nil
}

func (store *Store) ModerationCase(ctx context.Context, id int64, guildID uint64) (storage.ModerationCase, error) {
	row := store.db.QueryRowContext(ctx, moderationCaseSelect+` WHERE id=? AND guild_id=?`, id, guildID)
	value, err := scanModerationCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ModerationCase{}, ErrNotFound
	}
	if err != nil {
		return storage.ModerationCase{}, fmt.Errorf("get moderation case: %w", err)
	}
	return value, nil
}

func (store *Store) ModerationCases(ctx context.Context, guildID, targetUserID uint64, limit int) ([]storage.ModerationCase, error) {
	limit = min(max(limit, 1), 100)
	predicate := ` WHERE guild_id=?`
	arguments := []any{guildID}
	if targetUserID != 0 {
		predicate += ` AND target_user_id=?`
		arguments = append(arguments, targetUserID)
	}
	arguments = append(arguments, limit)
	rows, err := store.db.QueryContext(ctx, moderationCaseSelect+predicate+` ORDER BY created_at DESC, id DESC LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list moderation cases: %w", err)
	}
	defer rows.Close()
	result := make([]storage.ModerationCase, 0, min(limit, 20))
	for rows.Next() {
		value, scanErr := scanModerationCase(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan moderation case: %w", scanErr)
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store *Store) PendingModerationLogs(ctx context.Context, now time.Time, limit int) ([]storage.ModerationLog, error) {
	limit = min(max(limit, 1), 100)
	rows, err := store.db.QueryContext(ctx, `SELECT o.id, o.attempts, o.next_attempt_at, o.created_at, `+moderationCaseColumns+`
FROM moderation_log_outbox o JOIN moderation_cases c ON c.id=o.case_id
WHERE o.delivered_at IS NULL AND o.next_attempt_at<=? ORDER BY o.next_attempt_at, o.id LIMIT ?`, formatTime(now.UTC()), limit)
	if err != nil {
		return nil, fmt.Errorf("list moderation log outbox: %w", err)
	}
	defer rows.Close()
	result := make([]storage.ModerationLog, 0, min(limit, 20))
	for rows.Next() {
		var value storage.ModerationLog
		var nextAttempt, created string
		var duration, deleteDuration int64
		var caseCreated string
		var completed sql.NullString
		if err := rows.Scan(&value.ID, &value.Attempts, &nextAttempt, &created, &value.Case.ID, &value.Case.GuildID, &value.Case.ModeratorID, &value.Case.TargetUserID, &value.Case.Action, &value.Case.Reason, &duration, &deleteDuration, &value.Case.Status, &value.Case.DMStatus, &value.Case.ErrorCode, &caseCreated, &completed); err != nil {
			return nil, fmt.Errorf("scan moderation log: %w", err)
		}
		value.Case.Duration = time.Duration(duration) * time.Second
		value.Case.DeleteMessageDuration = time.Duration(deleteDuration) * time.Second
		if value.NextAttemptAt, err = parseTime(nextAttempt); err == nil {
			value.CreatedAt, err = parseTime(created)
		}
		if err == nil {
			value.Case.CreatedAt, err = parseTime(caseCreated)
		}
		if err == nil && completed.Valid {
			value.Case.CompletedAt, err = parseTime(completed.String)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store *Store) MarkModerationLogDelivered(ctx context.Context, id int64, deliveredAt time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE moderation_log_outbox SET delivered_at=?, last_error='' WHERE id=? AND delivered_at IS NULL`, formatTime(deliveredAt.UTC()), id)
	if err != nil {
		return fmt.Errorf("mark moderation log delivered: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) MarkModerationLogFailed(ctx context.Context, id int64, lastError string, nextAttemptAt time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE moderation_log_outbox SET attempts=attempts+1, last_error=?, next_attempt_at=? WHERE id=? AND delivered_at IS NULL`, truncateStorage(lastError, 160), formatTime(nextAttemptAt.UTC()), id)
	if err != nil {
		return fmt.Errorf("mark moderation log failed: %w", err)
	}
	return requireChanged(result)
}

func (store *Store) PurgeModerationCases(ctx context.Context, before time.Time, limit int) (int64, error) {
	limit = min(max(limit, 1), 1000)
	result, err := store.db.ExecContext(ctx, `DELETE FROM moderation_cases WHERE id IN (SELECT id FROM moderation_cases WHERE created_at<? ORDER BY created_at LIMIT ?)`, formatTime(before.UTC()), limit)
	if err != nil {
		return 0, fmt.Errorf("purge moderation cases: %w", err)
	}
	return result.RowsAffected()
}

func (store *Store) ApplyBatch(ctx context.Context, events []storage.WriteEvent) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin persistence batch: %w", err)
	}
	for _, event := range events {
		switch event.Kind {
		case storage.WriteAlertState:
			state := event.AlertState
			table := "alert_state"
			if state.RuleID <= 0 {
				table = "system_alert_state"
			}
			_, err = transaction.ExecContext(ctx, `INSERT INTO `+table+`(rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(rule_id, aircraft_icao, condition_fingerprint) DO UPDATE SET last_fired_at=excluded.last_fired_at, last_clear_at=excluded.last_clear_at, consecutive_matches=excluded.consecutive_matches, active=excluded.active`, state.RuleID, state.AircraftICAO, state.ConditionFingerprint, nullableTime(state.LastFiredAt), nullableTime(state.LastClearAt), state.ConsecutiveMatches, state.Active)
		case storage.WriteFeederEvent:
			value := event.Feeder
			_, err = transaction.ExecContext(ctx, `INSERT INTO feeder_events(guild_id, kind, status, detail, occurred_at) VALUES (?, ?, ?, ?, ?)`, value.GuildID, value.Kind, value.Status, value.Detail, formatTime(value.Occurred.UTC()))
		case storage.WriteReportRollup:
			value := event.Rollup
			_, err = transaction.ExecContext(ctx, `INSERT INTO report_rollups(guild_id, bucket_start, aircraft_seen, messages, emergencies, maximum_range, distinct_icaos) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(guild_id, bucket_start) DO UPDATE SET aircraft_seen=aircraft_seen+excluded.aircraft_seen, messages=messages+excluded.messages, emergencies=emergencies+excluded.emergencies, maximum_range=MAX(maximum_range, excluded.maximum_range), distinct_icaos=MAX(distinct_icaos, excluded.distinct_icaos)`, value.GuildID, formatTime(value.BucketStart.UTC()), value.AircraftSeen, value.Messages, value.Emergencies, value.MaximumRange, value.DistinctICAOs)
		default:
			err = fmt.Errorf("unsupported persistence event kind %d", event.Kind)
		}
		if err != nil {
			_ = transaction.Rollback()
			return fmt.Errorf("apply persistence batch: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit persistence batch: %w", err)
	}
	return nil
}

func (store *Store) Checkpoint(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
	return wrap("checkpoint WAL", err)
}

// Backup creates a transactionally consistent SQLite copy using VACUUM INTO.
// The destination must be a new absolute path controlled by the operator.
func (store *Store) Backup(ctx context.Context, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("SQLite backup path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	quoted := strings.ReplaceAll(destination, "'", "''")
	if _, err := store.db.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return fmt.Errorf("backup SQLite: %w", err)
	}
	return nil
}

// Restore replaces a stopped database with a validated backup and preserves
// the former database beside it as a timestamped pre-restore copy.
func Restore(ctx context.Context, backupPath, databasePath string) (string, error) {
	if !filepath.IsAbs(backupPath) || !filepath.IsAbs(databasePath) {
		return "", errors.New("SQLite restore paths must be absolute")
	}
	backup, err := sql.Open("sqlite", "file:"+backupPath+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("open backup: %w", err)
	}
	var integrity string
	err = backup.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	_ = backup.Close()
	if err != nil || integrity != "ok" {
		return "", fmt.Errorf("backup integrity check failed: result=%q error=%w", integrity, err)
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(databasePath), ".skyfeed-restore-*.db")
	if err != nil {
		return "", fmt.Errorf("create restore file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	source, err := os.Open(backupPath)
	if err != nil {
		return "", fmt.Errorf("read backup: %w", err)
	}
	if _, err := io.Copy(temporary, source); err != nil {
		_ = source.Close()
		return "", fmt.Errorf("copy backup: %w", err)
	}
	_ = source.Close()
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync restore: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close restore: %w", err)
	}
	preserved := ""
	preservedSidecars := make(map[string]string)
	if _, err := os.Stat(databasePath); err == nil {
		preserved = databasePath + ".pre-restore-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.Rename(databasePath, preserved); err != nil {
			return "", fmt.Errorf("preserve current database: %w", err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			sidecar := databasePath + suffix
			preservedSidecar := preserved + suffix
			if err := os.Rename(sidecar, preservedSidecar); err == nil {
				preservedSidecars[preservedSidecar] = sidecar
			} else if !errors.Is(err, os.ErrNotExist) {
				_ = os.Rename(preserved, databasePath)
				for from, to := range preservedSidecars {
					_ = os.Rename(from, to)
				}
				return "", fmt.Errorf("preserve SQLite sidecar %s: %w", suffix, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(temporaryPath, databasePath); err != nil {
		if preserved != "" {
			_ = os.Rename(preserved, databasePath)
			for from, to := range preservedSidecars {
				_ = os.Rename(from, to)
			}
		}
		return "", fmt.Errorf("activate restored database: %w", err)
	}
	cleanup = false
	return preserved, nil
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func requireChanged(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time: %w", err)
	}
	return parsed, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func scanModerationCase(scanner rowScanner) (storage.ModerationCase, error) {
	var value storage.ModerationCase
	var duration, deleteDuration int64
	var created string
	var completed sql.NullString
	if err := scanner.Scan(&value.ID, &value.GuildID, &value.ModeratorID, &value.TargetUserID, &value.Action, &value.Reason, &duration, &deleteDuration, &value.Status, &value.DMStatus, &value.ErrorCode, &created, &completed); err != nil {
		return storage.ModerationCase{}, err
	}
	value.Duration = time.Duration(duration) * time.Second
	value.DeleteMessageDuration = time.Duration(deleteDuration) * time.Second
	var err error
	value.CreatedAt, err = parseTime(created)
	if err == nil && completed.Valid {
		value.CompletedAt, err = parseTime(completed.String)
	}
	return value, err
}

func truncateStorage(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

var _ storage.Repository = (*Store)(nil)
var _ storage.BatchSink = (*Store)(nil)
