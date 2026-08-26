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
