CREATE TABLE channel_bindings_v2 (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('live', 'alerts', 'emergencies', 'reports', 'admin', 'moderation')),
    channel_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO channel_bindings_v2(guild_id, purpose, channel_id, updated_at)
SELECT guild_id, purpose, channel_id, updated_at FROM channel_bindings;
DROP TABLE channel_bindings;
ALTER TABLE channel_bindings_v2 RENAME TO channel_bindings;

CREATE TABLE role_bindings_v2 (
    guild_id INTEGER NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('operator', 'moderator', 'admin')),
    role_id INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (guild_id, purpose),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
INSERT INTO role_bindings_v2(guild_id, purpose, role_id, updated_at)
SELECT guild_id, purpose, role_id, updated_at FROM role_bindings
WHERE purpose IN ('operator', 'moderator', 'admin');
DROP TABLE role_bindings;
ALTER TABLE role_bindings_v2 RENAME TO role_bindings;

CREATE TABLE moderation_cases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    moderator_id INTEGER NOT NULL,
    target_user_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('warn', 'timeout', 'remove-timeout', 'kick', 'ban', 'unban')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 3 AND 400),
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds BETWEEN 0 AND 2419200),
    delete_message_seconds INTEGER NOT NULL DEFAULT 0 CHECK (delete_message_seconds BETWEEN 0 AND 604800),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    dm_status TEXT NOT NULL DEFAULT 'not-attempted' CHECK (dm_status IN ('not-attempted', 'delivered', 'failed')),
    error_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    completed_at TEXT,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX moderation_cases_guild_time ON moderation_cases(guild_id, created_at DESC, id DESC);
CREATE INDEX moderation_cases_target_time ON moderation_cases(guild_id, target_user_id, created_at DESC, id DESC);

CREATE TABLE moderation_log_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    case_id INTEGER NOT NULL UNIQUE,
    guild_id INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000000),
    next_attempt_at TEXT NOT NULL,
    delivered_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES moderation_cases(id) ON DELETE CASCADE,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
CREATE INDEX moderation_log_outbox_due ON moderation_log_outbox(delivered_at, next_attempt_at, id);
