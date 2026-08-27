CREATE TABLE channel_bindings_v3 (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('live', 'alerts', 'emergencies', 'reports', 'admin', 'moderation', 'interesting', 'high-interest')),
    channel_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO channel_bindings_v3(guild_id, purpose, channel_id, updated_at)
SELECT guild_id, purpose, channel_id, updated_at FROM channel_bindings;
DROP TABLE channel_bindings;
ALTER TABLE channel_bindings_v3 RENAME TO channel_bindings;
