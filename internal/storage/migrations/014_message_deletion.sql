CREATE TABLE moderation_cases_v2 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    moderator_id INTEGER NOT NULL,
    target_user_id INTEGER NOT NULL,
    target_channel_id INTEGER,
    target_message_id INTEGER,
    target_message_created_at TEXT,
    action TEXT NOT NULL CHECK (action IN ('warn', 'timeout', 'remove-timeout', 'kick', 'ban', 'unban', 'delete-message')),
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

INSERT INTO moderation_cases_v2(
    id, guild_id, moderator_id, target_user_id, action, reason,
    duration_seconds, delete_message_seconds, status, dm_status, error_code,
    created_at, completed_at
)
SELECT id, guild_id, moderator_id, target_user_id, action, reason,
       duration_seconds, delete_message_seconds, status, dm_status, error_code,
       created_at, completed_at
FROM moderation_cases;

CREATE TABLE moderation_log_outbox_v2 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    case_id INTEGER NOT NULL UNIQUE,
    guild_id INTEGER NOT NULL,
    attempts INTEGER NOT NULL CHECK (attempts BETWEEN 0 AND 1000000),
    next_attempt_at TEXT NOT NULL,
    delivered_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES moderation_cases_v2(id) ON DELETE CASCADE,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

INSERT INTO moderation_log_outbox_v2(id, case_id, guild_id, attempts, next_attempt_at, delivered_at, last_error, created_at)
SELECT id, case_id, guild_id, attempts, next_attempt_at, delivered_at, last_error, created_at
FROM moderation_log_outbox;

DROP TABLE moderation_log_outbox;
DROP TABLE moderation_cases;
ALTER TABLE moderation_cases_v2 RENAME TO moderation_cases;
ALTER TABLE moderation_log_outbox_v2 RENAME TO moderation_log_outbox;

CREATE INDEX moderation_cases_guild_time ON moderation_cases(guild_id, created_at DESC, id DESC);
CREATE INDEX moderation_cases_target_time ON moderation_cases(guild_id, target_user_id, created_at DESC, id DESC);
CREATE INDEX moderation_cases_message ON moderation_cases(guild_id, target_channel_id, target_message_id);
CREATE INDEX moderation_log_outbox_due ON moderation_log_outbox(delivered_at, next_attempt_at, id);
