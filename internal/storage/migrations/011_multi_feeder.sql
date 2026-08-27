CREATE TABLE feeders (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 48),
    guild_id INTEGER NOT NULL,
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    public_area TEXT NOT NULL DEFAULT '' CHECK (length(public_area) <= 120),
    airport_icao TEXT NOT NULL DEFAULT '' CHECK (length(airport_icao) IN (0, 4)),
    weather_station_icao TEXT NOT NULL DEFAULT '' CHECK (length(weather_station_icao) IN (0, 4)),
    latitude REAL,
    longitude REAL,
    has_center INTEGER NOT NULL DEFAULT 0 CHECK (has_center IN (0, 1)),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('local', 'agent')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    owner_user_id INTEGER NOT NULL DEFAULT 0,
    public_key BLOB,
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    last_payload_hash BLOB,
    last_seen_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX feeders_guild_enabled ON feeders(guild_id, enabled, display_name);

INSERT INTO feeders(id, guild_id, display_name, source_kind, enabled, created_at, updated_at)
SELECT 'local', guild_id, 'Local feeder', 'local', 1, created_at, updated_at
FROM guild_settings;

CREATE TABLE feeder_enrollments (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    feeder_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (feeder_id) REFERENCES feeders(id) ON DELETE CASCADE
);
CREATE INDEX feeder_enrollments_expiry ON feeder_enrollments(expires_at, consumed_at, revoked_at);

ALTER TABLE guild_settings ADD COLUMN default_feeder_id TEXT NOT NULL DEFAULT 'all';
ALTER TABLE watch_rules ADD COLUMN feeder_scope TEXT NOT NULL DEFAULT 'local';
ALTER TABLE feeder_events ADD COLUMN feeder_id TEXT NOT NULL DEFAULT 'local';

CREATE TABLE alert_state_v2 (
    rule_id INTEGER NOT NULL,
    feeder_scope TEXT NOT NULL DEFAULT 'local',
    aircraft_icao TEXT NOT NULL,
    condition_fingerprint TEXT NOT NULL,
    last_fired_at TEXT,
    last_clear_at TEXT,
    consecutive_matches INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    PRIMARY KEY (rule_id, feeder_scope, aircraft_icao, condition_fingerprint),
    FOREIGN KEY (rule_id) REFERENCES watch_rules(id) ON DELETE CASCADE
);
INSERT INTO alert_state_v2(rule_id, feeder_scope, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active)
SELECT rule_id, 'local', aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active
FROM alert_state;
DROP TABLE alert_state;
ALTER TABLE alert_state_v2 RENAME TO alert_state;

CREATE TABLE system_alert_state_v2 (
    rule_id INTEGER NOT NULL,
    feeder_scope TEXT NOT NULL DEFAULT 'local',
    aircraft_icao TEXT NOT NULL,
    condition_fingerprint TEXT NOT NULL,
    last_fired_at TEXT,
    last_clear_at TEXT,
    consecutive_matches INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    PRIMARY KEY (rule_id, feeder_scope, aircraft_icao, condition_fingerprint)
);
INSERT INTO system_alert_state_v2(rule_id, feeder_scope, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active)
SELECT rule_id, 'local', aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active
FROM system_alert_state;
DROP TABLE system_alert_state;
ALTER TABLE system_alert_state_v2 RENAME TO system_alert_state;

CREATE TABLE report_rollups_v2 (
    guild_id INTEGER NOT NULL,
    feeder_scope TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    aircraft_observations INTEGER NOT NULL DEFAULT 0,
    messages INTEGER NOT NULL DEFAULT 0,
    emergency_observations INTEGER NOT NULL DEFAULT 0,
    emergency_events INTEGER NOT NULL DEFAULT 0,
    maximum_range REAL NOT NULL DEFAULT 0,
    peak_tracked INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (guild_id, feeder_scope, bucket_start),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO report_rollups_v2(guild_id, feeder_scope, bucket_start, aircraft_observations, messages, emergency_observations, emergency_events, maximum_range, peak_tracked)
SELECT guild_id, 'local', bucket_start, aircraft_observations, messages, emergency_observations, emergency_events, maximum_range, peak_tracked
FROM report_rollups;
INSERT INTO report_rollups_v2(guild_id, feeder_scope, bucket_start, aircraft_observations, messages, emergency_observations, emergency_events, maximum_range, peak_tracked)
SELECT guild_id, 'all', bucket_start, aircraft_observations, messages, emergency_observations, emergency_events, maximum_range, peak_tracked
FROM report_rollups;
DROP TABLE report_rollups;
ALTER TABLE report_rollups_v2 RENAME TO report_rollups;
CREATE INDEX report_rollups_guild_scope_time ON report_rollups(guild_id, feeder_scope, bucket_start);

CREATE TABLE route_sightings_v2 (
    guild_id INTEGER NOT NULL,
    feeder_id TEXT NOT NULL DEFAULT 'local',
    icao TEXT NOT NULL,
    callsign TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    sightings INTEGER NOT NULL DEFAULT 1 CHECK (sightings > 0),
    PRIMARY KEY (guild_id, feeder_id, icao, bucket_start),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO route_sightings_v2(guild_id, feeder_id, icao, callsign, bucket_start, sightings)
SELECT guild_id, 'local', icao, callsign, bucket_start, sightings FROM route_sightings;
DROP TABLE route_sightings;
ALTER TABLE route_sightings_v2 RENAME TO route_sightings;
CREATE INDEX route_sightings_guild_bucket ON route_sightings(guild_id, bucket_start);
CREATE INDEX route_sightings_guild_feeder_bucket ON route_sightings(guild_id, feeder_id, bucket_start);
CREATE INDEX route_sightings_callsign ON route_sightings(callsign);
