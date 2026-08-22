CREATE TABLE system_alert_state (
    rule_id INTEGER NOT NULL,
    aircraft_icao TEXT NOT NULL,
    condition_fingerprint TEXT NOT NULL,
    last_fired_at TEXT,
    last_clear_at TEXT,
    consecutive_matches INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    PRIMARY KEY (rule_id, aircraft_icao, condition_fingerprint)
);
