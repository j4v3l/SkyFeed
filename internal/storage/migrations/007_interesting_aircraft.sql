CREATE TABLE channel_bindings_v2 (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('live', 'alerts', 'emergencies', 'reports', 'admin', 'moderation', 'interesting')),
    channel_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO channel_bindings_v2(guild_id, purpose, channel_id, updated_at)
SELECT guild_id, purpose, channel_id, updated_at FROM channel_bindings;
DROP TABLE channel_bindings;
ALTER TABLE channel_bindings_v2 RENAME TO channel_bindings;

CREATE TABLE alert_configs_v2 (
    guild_id INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('watch', 'emergency', 'feeder', 'interesting')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    cooldown_seconds INTEGER NOT NULL DEFAULT 900 CHECK (cooldown_seconds BETWEEN 0 AND 604800),
    destination_channel_id INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, category),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO alert_configs_v2(guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at)
SELECT guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at FROM alert_configs;
DROP TABLE alert_configs;
ALTER TABLE alert_configs_v2 RENAME TO alert_configs;

CREATE TABLE plane_alert_reference (
    icao TEXT PRIMARY KEY,
    registration TEXT NOT NULL DEFAULT '',
    operator TEXT NOT NULL DEFAULT '',
    aircraft_type TEXT NOT NULL DEFAULT '',
    icao_type TEXT NOT NULL DEFAULT '',
    flight_group TEXT NOT NULL DEFAULT '',
    tag1 TEXT NOT NULL DEFAULT '',
    tag2 TEXT NOT NULL DEFAULT '',
    tag3 TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    link TEXT NOT NULL DEFAULT '',
    image_link_1 TEXT NOT NULL DEFAULT '',
    image_link_2 TEXT NOT NULL DEFAULT '',
    image_link_3 TEXT NOT NULL DEFAULT '',
    image_link_4 TEXT NOT NULL DEFAULT '',
    commit_hash TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE interesting_aircraft_seen (
    guild_id INTEGER NOT NULL,
    icao TEXT NOT NULL,
    first_seen_at TEXT NOT NULL,
    callsign TEXT NOT NULL DEFAULT '',
    flight_group TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (guild_id, icao),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX interesting_aircraft_seen_guild_time ON interesting_aircraft_seen(guild_id, first_seen_at DESC);
