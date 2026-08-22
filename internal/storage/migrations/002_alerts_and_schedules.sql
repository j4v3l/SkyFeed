CREATE TABLE alert_configs (
    guild_id INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('watch', 'emergency', 'feeder')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    cooldown_seconds INTEGER NOT NULL DEFAULT 900 CHECK (cooldown_seconds BETWEEN 0 AND 604800),
    destination_channel_id INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, category),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE TABLE report_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    cadence TEXT NOT NULL CHECK (cadence IN ('daily', 'weekly')),
    destination_channel_id INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    updated_at TEXT NOT NULL,
    UNIQUE (guild_id, cadence, destination_channel_id),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX report_schedules_guild ON report_schedules(guild_id, enabled, id);
