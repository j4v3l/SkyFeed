CREATE TABLE admin_digest_state (
    guild_id INTEGER PRIMARY KEY,
    last_run_at TEXT NOT NULL,
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);
