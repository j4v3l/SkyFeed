CREATE TABLE guild_settings (
    guild_id INTEGER PRIMARY KEY,
    units TEXT NOT NULL DEFAULT 'aviation' CHECK (units IN ('aviation', 'metric')),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE channel_bindings (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('live', 'alerts', 'emergencies', 'reports', 'admin')),
    channel_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE TABLE role_bindings (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('operator', 'moderator', 'admin')),
    role_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE TABLE user_preferences (
    guild_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    units TEXT NOT NULL DEFAULT 'aviation' CHECK (units IN ('aviation', 'metric')),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, user_id),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE TABLE watch_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    server_scope INTEGER NOT NULL DEFAULT 0 CHECK (server_scope IN (0, 1)),
    rule_type TEXT NOT NULL,
    rule_value TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    cooldown_seconds INTEGER NOT NULL DEFAULT 900 CHECK (cooldown_seconds BETWEEN 0 AND 604800),
    minimum_observations INTEGER NOT NULL DEFAULT 2 CHECK (minimum_observations BETWEEN 1 AND 60),
    enter_threshold REAL NOT NULL DEFAULT 0,
    exit_threshold REAL NOT NULL DEFAULT 0,
    best_effort_enrichment INTEGER NOT NULL DEFAULT 0 CHECK (best_effort_enrichment IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX watch_rules_guild_enabled_type ON watch_rules(guild_id, enabled, rule_type);
CREATE INDEX watch_rules_user ON watch_rules(guild_id, user_id, id);

CREATE TABLE alert_state (
    rule_id INTEGER NOT NULL,
    aircraft_icao TEXT NOT NULL,
    condition_fingerprint TEXT NOT NULL,
    last_fired_at TEXT,
    last_clear_at TEXT,
    consecutive_matches INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    PRIMARY KEY (rule_id, aircraft_icao, condition_fingerprint),
    FOREIGN KEY (rule_id) REFERENCES watch_rules(id) ON DELETE CASCADE
);

CREATE TABLE feeder_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX feeder_events_guild_time ON feeder_events(guild_id, occurred_at DESC);

CREATE TABLE report_rollups (
    guild_id INTEGER NOT NULL,
    bucket_start TEXT NOT NULL,
    aircraft_seen INTEGER NOT NULL DEFAULT 0,
    messages INTEGER NOT NULL DEFAULT 0,
    emergencies INTEGER NOT NULL DEFAULT 0,
    maximum_range REAL NOT NULL DEFAULT 0,
    distinct_icaos INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (guild_id, bucket_start),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE TABLE message_bindings (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL,
    channel_id INTEGER NOT NULL,
    message_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
