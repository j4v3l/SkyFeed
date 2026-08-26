-- Route analytics are derived and previously lacked provider provenance. Purge
-- only these rebuildable rows before making adsb.lol provenance mandatory.
DELETE FROM route_sightings;
DELETE FROM route_catalog;

CREATE TEMP TABLE migration_emergency_state AS
SELECT
    rule_id,
    aircraft_icao,
    'emergency' AS condition_fingerprint,
    MAX(last_fired_at) AS last_fired_at,
    MAX(last_clear_at) AS last_clear_at,
    MAX(consecutive_matches) AS consecutive_matches,
    MAX(active) AS active
FROM system_alert_state
WHERE rule_id = -1
GROUP BY rule_id, aircraft_icao;

DELETE FROM system_alert_state WHERE rule_id = -1;

INSERT INTO system_alert_state(rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active)
SELECT rule_id, aircraft_icao, condition_fingerprint, last_fired_at, last_clear_at, consecutive_matches, active
FROM migration_emergency_state;

DROP TABLE migration_emergency_state;

DROP TABLE route_catalog;

CREATE TABLE route_catalog (
    callsign TEXT PRIMARY KEY,
    source TEXT NOT NULL CHECK (source = 'adsb-lol'),
    airline_name TEXT NOT NULL DEFAULT '',
    airline_icao TEXT NOT NULL DEFAULT '',
    airline_iata TEXT NOT NULL DEFAULT '',
    origin_icao TEXT NOT NULL DEFAULT '',
    origin_iata TEXT NOT NULL DEFAULT '',
    origin_name TEXT NOT NULL DEFAULT '',
    origin_country_iso TEXT NOT NULL DEFAULT '',
    destination_icao TEXT NOT NULL DEFAULT '',
    destination_iata TEXT NOT NULL DEFAULT '',
    destination_name TEXT NOT NULL DEFAULT '',
    destination_country_iso TEXT NOT NULL DEFAULT '',
    plausible INTEGER NOT NULL DEFAULT 1 CHECK (plausible IN (0, 1)),
    plausibility_known INTEGER NOT NULL DEFAULT 0 CHECK (plausibility_known IN (0, 1)),
    updated_at TEXT NOT NULL
);

ALTER TABLE report_rollups RENAME COLUMN aircraft_seen TO aircraft_observations;
ALTER TABLE report_rollups RENAME COLUMN distinct_icaos TO peak_tracked;
ALTER TABLE report_rollups RENAME COLUMN emergencies TO emergency_observations;
ALTER TABLE report_rollups ADD COLUMN emergency_events INTEGER NOT NULL DEFAULT 0;
