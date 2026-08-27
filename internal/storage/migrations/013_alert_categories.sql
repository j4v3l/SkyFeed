CREATE TABLE alert_configs_v3 (
    guild_id INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('watch', 'emergency', 'feeder', 'interesting', 'high-interest', 'movements')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    cooldown_seconds INTEGER NOT NULL DEFAULT 900 CHECK (cooldown_seconds BETWEEN 0 AND 604800),
    destination_channel_id INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, category),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO alert_configs_v3(guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at)
SELECT guild_id, category, enabled, cooldown_seconds, destination_channel_id, updated_at FROM alert_configs;
DROP TABLE alert_configs;
ALTER TABLE alert_configs_v3 RENAME TO alert_configs;
