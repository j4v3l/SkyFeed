package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationFromEveryPriorSchemaVersion(t *testing.T) {
	for version := 1; version <= 10; version++ {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			applyMigrationsThrough(t, ctx, db, version)
			if err := ApplyMigrations(ctx, db); err != nil {
				t.Fatalf("upgrade from schema %d: %v", version, err)
			}
			var latest int
			if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&latest); err != nil || latest != 13 {
				t.Fatalf("latest schema=%d err=%v", latest, err)
			}
		})
	}
}

func TestMigrationElevenPreservesLocalDataAndCreatesAggregateReports(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsThrough(t, ctx, db, 10)
	const at = "2026-08-26T12:00:00Z"
	statements := []string{
		`INSERT INTO guild_settings(guild_id, units, timezone, created_at, updated_at) VALUES (42, 'aviation', 'UTC', '` + at + `', '` + at + `')`,
		`INSERT INTO watch_rules(id, guild_id, user_id, server_scope, rule_type, rule_value, enabled, cooldown_seconds, minimum_observations, enter_threshold, exit_threshold, best_effort_enrichment, created_at, updated_at) VALUES (7, 42, 8, 0, 'icao', 'ABC123', 1, 900, 2, 0, 0, 0, '` + at + `', '` + at + `')`,
		`INSERT INTO alert_state(rule_id, aircraft_icao, condition_fingerprint, consecutive_matches, active) VALUES (7, 'ABC123', 'watch', 2, 1)`,
		`INSERT INTO system_alert_state(rule_id, aircraft_icao, condition_fingerprint, consecutive_matches, active) VALUES (-1, 'ABC123', 'emergency', 1, 1)`,
		`INSERT INTO feeder_events(guild_id, kind, status, detail, occurred_at) VALUES (42, 'offline', 'open', 'test', '` + at + `')`,
		`INSERT INTO report_rollups(guild_id, bucket_start, aircraft_observations, messages, emergency_observations, emergency_events, maximum_range, peak_tracked) VALUES (42, '` + at + `', 10, 20, 3, 1, 12.5, 4)`,
		`INSERT INTO route_catalog(callsign, source, origin_iata, destination_iata, updated_at) VALUES ('SKY1', 'adsb-lol', 'PBI', 'JFK', '` + at + `')`,
		`INSERT INTO route_sightings(guild_id, icao, callsign, bucket_start, sightings) VALUES (42, 'ABC123', 'SKY1', '` + at + `', 2)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed schema 10: %v", err)
		}
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM feeders WHERE id='local' AND guild_id=42`, 1},
		{`SELECT COUNT(*) FROM watch_rules WHERE id=7 AND feeder_scope='local'`, 1},
		{`SELECT COUNT(*) FROM alert_state WHERE rule_id=7 AND feeder_scope='local'`, 1},
		{`SELECT COUNT(*) FROM system_alert_state WHERE rule_id=-1 AND feeder_scope='local'`, 1},
		{`SELECT COUNT(*) FROM feeder_events WHERE guild_id=42 AND feeder_id='local'`, 1},
		{`SELECT COUNT(*) FROM report_rollups WHERE guild_id=42 AND feeder_scope IN ('local', 'all')`, 2},
		{`SELECT COUNT(*) FROM route_sightings WHERE guild_id=42 AND feeder_id='local'`, 1},
		{`SELECT COUNT(*) FROM route_sightings WHERE guild_id=42 AND feeder_id='all'`, 0},
		{`SELECT COUNT(*) FROM route_catalog WHERE callsign='SKY1' AND source='adsb-lol'`, 1},
		{`SELECT COUNT(*) FROM guild_settings WHERE guild_id=42 AND default_feeder_id='all'`, 1},
	}
	for _, check := range checks {
		var count int
		if err := db.QueryRowContext(ctx, check.query).Scan(&count); err != nil || count != check.want {
			t.Fatalf("%s: count=%d want=%d err=%v", check.query, count, check.want, err)
		}
	}
}

func applyMigrationsThrough(t *testing.T, ctx context.Context, db *sql.DB, maximum int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if entry.IsDir() || !ok {
			continue
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version > maximum {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(script)); err != nil {
			t.Fatalf("migration %d: %v", version, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, '2026-01-01T00:00:00Z')`, version); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationTenPurgesDerivedRoutesAndPreservesLegacyCounters(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if entry.IsDir() || !ok {
			continue
		}
		version, parseErr := strconv.Atoi(prefix)
		if parseErr != nil || version >= 10 {
			continue
		}
		script, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, execErr := db.ExecContext(ctx, string(script)); execErr != nil {
			t.Fatalf("migration %d: %v", version, execErr)
		}
		if _, execErr := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, '2026-01-01T00:00:00Z')`, version); execErr != nil {
			t.Fatal(execErr)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO guild_settings(guild_id, units, timezone, created_at, updated_at) VALUES (1, 'aviation', 'UTC', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO route_catalog(callsign, origin_iata, destination_iata, plausible, updated_at) VALUES ('SF1', 'AAA', 'BBB', 1, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO route_sightings(guild_id, icao, callsign, bucket_start, sightings) VALUES (1, 'ABC123', 'SF1', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_rollups(guild_id, bucket_start, aircraft_seen, messages, emergencies, maximum_range, distinct_icaos) VALUES (1, '2026-01-01T00:00:00Z', 42, 100, 3, 12.5, 7)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO system_alert_state(rule_id, aircraft_icao, condition_fingerprint, last_fired_at, consecutive_matches, active) VALUES
(-1, 'ABC123', 'emergency:7700:', '2026-01-01T00:00:00Z', 1, 1),
(-1, 'ABC123', 'emergency:7600:general', '2026-01-02T00:00:00Z', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"route_catalog", "route_sightings"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var observations, peak, legacyEmergencies, events int64
	if err := db.QueryRowContext(ctx, `SELECT aircraft_observations, peak_tracked, emergency_observations, emergency_events FROM report_rollups WHERE guild_id=1`).Scan(&observations, &peak, &legacyEmergencies, &events); err != nil {
		t.Fatal(err)
	}
	if observations != 42 || peak != 7 || legacyEmergencies != 3 || events != 0 {
		t.Fatalf("migrated counters observations=%d peak=%d legacy_emergencies=%d events=%d", observations, peak, legacyEmergencies, events)
	}
	var emergencyRows, active int
	var fingerprint, firedAt string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), condition_fingerprint, last_fired_at, active FROM system_alert_state WHERE rule_id=-1 AND aircraft_icao='ABC123'`).Scan(&emergencyRows, &fingerprint, &firedAt, &active); err != nil {
		t.Fatal(err)
	}
	if emergencyRows != 1 || fingerprint != "emergency" || firedAt != "2026-01-02T00:00:00Z" || active != 1 {
		t.Fatalf("migrated emergency rows=%d fingerprint=%q fired=%q active=%d", emergencyRows, fingerprint, firedAt, active)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO route_catalog(callsign, source, plausible, updated_at) VALUES ('SF2', 'adsbdb', 1, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("invalid durable route provenance accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO route_catalog(callsign, plausible, updated_at) VALUES ('SF3', 1, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("missing durable route provenance accepted")
	}
}
