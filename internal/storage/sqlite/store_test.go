package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestStorePersistenceAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "skyfeed.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	if err := store.UpsertGuildSettings(ctx, storage.GuildSettings{GuildID: 42, Units: "aviation", Timezone: "UTC", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: 42, Purpose: "alerts", ChannelID: 99, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: 42, Purpose: "moderation", ChannelID: 101, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: 42, Purpose: "high-interest", ChannelID: 102, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: 42, Tier: "moderator", RoleID: 77, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	moderationCase, err := store.CreateModerationCase(ctx, storage.ModerationCase{GuildID: 42, ModeratorID: 7, TargetUserID: 8, Action: "timeout", Reason: "Repeated disruption", Duration: time.Hour, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteModerationCase(ctx, moderationCase.ID, 42, "succeeded", "not-attempted", "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rule, err := store.CreateWatchRule(ctx, domain.WatchRule{GuildID: 42, UserID: 7, Type: domain.RuleICAO, Value: "abc123", Enabled: true, Cooldown: time.Minute, MinimumObservations: 2, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == 0 || rule.Value != "ABC123" {
		t.Fatalf("rule = %+v", rule)
	}
	state := domain.AlertState{RuleID: rule.ID, AircraftICAO: "ABC123", ConditionFingerprint: "icao:ABC123", LastFiredAt: now, ConsecutiveMatches: 2, Active: true}
	if err := store.UpsertAlertState(ctx, state); err != nil {
		t.Fatal(err)
	}
	systemState := domain.AlertState{RuleID: -1, AircraftICAO: "DEF456", ConditionFingerprint: "emergency:7700:", LastFiredAt: now, ConsecutiveMatches: 1, Active: true}
	if err := store.UpsertAlertState(ctx, systemState); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendFeederEvent(ctx, storage.FeederEvent{GuildID: 42, Kind: "feeder:offline:aircraft", Status: "offline", Occurred: now}); err != nil {
		t.Fatal(err)
	}
	schedule, err := store.UpsertReportSchedule(ctx, storage.ReportSchedule{GuildID: 42, Cadence: "daily", Destination: 100, Enabled: true, LastRun: now})
	if err != nil {
		t.Fatal(err)
	}
	runAt := now.Add(24 * time.Hour)
	if err := store.MarkReportScheduleRun(ctx, schedule.ID, 42, runAt); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rules, err := reopened.WatchRules(ctx, 42, 7, 10)
	if err != nil || len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	states, err := reopened.AlertStates(ctx, 10)
	if err != nil || len(states) != 2 || !states[0].Active || !states[1].Active {
		t.Fatalf("states=%+v err=%v", states, err)
	}
	events, err := reopened.RecentFeederEvents(ctx, 42, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "feeder:offline:aircraft" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	bindings, err := reopened.ChannelBindings(ctx, 42)
	if err != nil || len(bindings) != 3 {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
	roles, err := reopened.RoleBindings(ctx, 42)
	if err != nil || len(roles) != 1 || roles[0].Tier != "moderator" || roles[0].RoleID != 77 {
		t.Fatalf("roles=%+v err=%v", roles, err)
	}
	storedCase, err := reopened.ModerationCase(ctx, moderationCase.ID, 42)
	if err != nil || storedCase.Status != "succeeded" || storedCase.Duration != time.Hour {
		t.Fatalf("case=%+v err=%v", storedCase, err)
	}
	logs, err := reopened.PendingModerationLogs(ctx, now.Add(time.Minute), 10)
	if err != nil || len(logs) != 1 || logs[0].Case.ID != moderationCase.ID {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	if err := reopened.MarkModerationLogFailed(ctx, logs[0].ID, "synthetic delivery failure", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	logs, err = reopened.PendingModerationLogs(ctx, now.Add(30*time.Minute), 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("early retry logs=%+v err=%v", logs, err)
	}
	logs, err = reopened.PendingModerationLogs(ctx, now.Add(2*time.Hour), 10)
	if err != nil || len(logs) != 1 || logs[0].Attempts != 1 {
		t.Fatalf("retry logs=%+v err=%v", logs, err)
	}
	if err := reopened.MarkModerationLogDelivered(ctx, logs[0].ID, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	schedules, err := reopened.ReportSchedules(ctx, 42)
	if err != nil || len(schedules) != 1 || !schedules[0].LastRun.Equal(runAt) {
		t.Fatalf("schedules=%+v err=%v", schedules, err)
	}
}

func TestModerationRetentionIsBounded(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 1); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(-2, 0, 0)
	for range 3 {
		value, createErr := store.CreateModerationCase(ctx, storage.ModerationCase{GuildID: 1, ModeratorID: 2, TargetUserID: 3, Action: "warn", Reason: "Synthetic retention case", CreatedAt: old})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if completeErr := store.CompleteModerationCase(ctx, value.ID, 1, "succeeded", "delivered", "", old); completeErr != nil {
			t.Fatal(completeErr)
		}
	}
	removed, err := store.PurgeModerationCases(ctx, time.Now().UTC().AddDate(-1, 0, 0), 2)
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	remaining, err := store.ModerationCases(ctx, 1, 0, 10)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("remaining=%d err=%v", len(remaining), err)
	}
}

func TestStoreScopesUpdates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertGuildSettings(ctx, storage.GuildSettings{GuildID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWatchRule(ctx, 999, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := store.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: 1, Tier: "owner", RoleID: 2}); err == nil {
		t.Fatal("invalid role tier was accepted")
	}
}

func TestGuildSettingsPersistAlertPauseAndMutedSquawks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Unix(1_700_000_000, 0).UTC()
	if err := store.UpsertGuildSettings(ctx, storage.GuildSettings{
		GuildID: 7, Units: "aviation", Timezone: "UTC", AlertsPaused: true, MutedSquawks: "7700,7600", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GuildSettings(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AlertsPaused || settings.MutedSquawks != "7700,7600" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestUserUnitPreferencePersistsAndValidates(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertUserPreference(context.Background(), storage.UserPreference{GuildID: 7, UserID: 9, Units: "metric"}); err != nil {
		t.Fatal(err)
	}
	preference, err := store.UserPreference(context.Background(), 7, 9)
	if err != nil || preference.Units != "metric" {
		t.Fatalf("preference=%+v err=%v", preference, err)
	}
	if err := store.UpsertUserPreference(context.Background(), storage.UserPreference{GuildID: 7, UserID: 9, Units: "nautical-ish"}); err == nil {
		t.Fatal("invalid units accepted")
	}
}

func TestPurgeAlertStatesRemovesOnlyOldInactiveRows(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, state := range []domain.AlertState{
		{RuleID: -1, AircraftICAO: "OLD001", ConditionFingerprint: "old", LastClearAt: now.Add(-8 * 24 * time.Hour)},
		{RuleID: -1, AircraftICAO: "NEW001", ConditionFingerprint: "new", LastClearAt: now.Add(-time.Hour)},
		{RuleID: -1, AircraftICAO: "ACTIVE", ConditionFingerprint: "active", LastFiredAt: now.Add(-8 * 24 * time.Hour), Active: true},
	} {
		if err := store.UpsertAlertState(context.Background(), state); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.PurgeAlertStates(context.Background(), now.Add(-7*24*time.Hour), 100)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	states, err := store.AlertStates(context.Background(), 100)
	if err != nil || len(states) != 2 {
		t.Fatalf("states=%+v err=%v", states, err)
	}
}

func TestDeleteMessageBindingRemovesDashboard(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMessageBinding(ctx, storage.MessageBinding{GuildID: 9, Purpose: "dashboard", ChannelID: 1, MessageID: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteMessageBinding(ctx, 9, "dashboard"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.MessageBinding(ctx, 9, "dashboard"); err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestDeleteMessageBindingByTargetRemovesOnlyMatchingMessage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 9); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []storage.MessageBinding{
		{GuildID: 9, Purpose: "dashboard", ChannelID: 1, MessageID: 2},
		{GuildID: 9, Purpose: "flight-leaders", ChannelID: 3, MessageID: 4},
	} {
		if err := store.UpsertMessageBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteMessageBindingByTarget(ctx, 9, 3, 4); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.MessageBinding(ctx, 9, "flight-leaders"); err != nil || found {
		t.Fatalf("flight leader binding found=%v err=%v", found, err)
	}
	if _, found, err := store.MessageBinding(ctx, 9, "dashboard"); err != nil || !found {
		t.Fatalf("dashboard binding found=%v err=%v", found, err)
	}
}

func TestReportSummaryUsesCompleteHourBuckets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 1); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	if err := store.AddReportRollup(ctx, storage.ReportRollup{GuildID: 1, BucketStart: start, AircraftObservations: 10, PeakTracked: 3, EmergencyEvents: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddReportRollup(ctx, storage.ReportRollup{GuildID: 1, BucketStart: start.Add(time.Hour), AircraftObservations: 1, PeakTracked: 1}); err != nil {
		t.Fatal(err)
	}

	summary, err := store.ReportSummary(ctx, 1, start.Add(time.Minute), start.Add(time.Hour+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !summary.From.Equal(start) || !summary.To.Equal(start.Add(time.Hour)) || summary.AircraftObservations != 10 || summary.EmergencyEvents != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.PeakTracked != 3 {
		t.Fatalf("peak tracked aircraft = %d, want 3", summary.PeakTracked)
	}
	if !summary.PeakHour.Equal(start) || summary.PeakAircraft != 3 {
		t.Fatalf("peak hour = %+v", summary)
	}
}

func TestBackupCanBeOpenedAndRestored(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := Open(ctx, filepath.Join(directory, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertGuildSettings(ctx, storage.GuildSettings{GuildID: 123}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(directory, "backup.db")
	if err := store.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	preserved, err := Restore(ctx, backup, filepath.Join(directory, "live.db"))
	if err != nil || preserved == "" {
		t.Fatalf("restore preserved=%q err=%v", preserved, err)
	}
	restored, err := Open(ctx, filepath.Join(directory, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.GuildSettings(ctx, 123); err != nil {
		t.Fatalf("restored settings: %v", err)
	}
}

func TestPlaneAlertReferenceAndInterestingSeen(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplacePlaneAlertReference(ctx, []storage.PlaneAlertReference{{
		ICAO: "AE1234", Operator: "USAF", FlightGroup: "Mil", CommitHash: "abc",
	}}, "abc"); err != nil {
		t.Fatal(err)
	}
	count, err := store.PlaneAlertReferenceCount(ctx)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	hash, err := store.PlaneAlertCommitHash(ctx)
	if err != nil || hash != "abc" {
		t.Fatalf("hash=%q err=%v", hash, err)
	}
	now := time.Now().UTC()
	if err := store.UpsertInterestingSeen(ctx, storage.InterestingSeen{GuildID: 5, ICAO: "AE1234", FirstSeenAt: now, Callsign: "RCH1", FlightGroup: "Mil"}); err != nil {
		t.Fatal(err)
	}
	seen, err := store.InterestingSeenICAOs(ctx, 5)
	if err != nil || len(seen) != 1 || seen[0] != "AE1234" {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
	if err := store.UpsertChannelBinding(ctx, storage.ChannelBinding{GuildID: 5, Purpose: "interesting", ChannelID: 77, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSQLiteBatch(b *testing.B) {
	store, err := Open(context.Background(), filepath.Join(b.TempDir(), "skyfeed.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertGuildSettings(context.Background(), storage.GuildSettings{GuildID: 1}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := store.AddReportRollup(context.Background(), storage.ReportRollup{GuildID: 1, BucketStart: time.Unix(1_700_000_000, 0), AircraftObservations: 1, Messages: 10}); err != nil {
			b.Fatal(err)
		}
	}
}
