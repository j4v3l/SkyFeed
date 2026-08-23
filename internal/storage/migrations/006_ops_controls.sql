ALTER TABLE guild_settings ADD COLUMN alerts_paused INTEGER NOT NULL DEFAULT 0 CHECK (alerts_paused IN (0, 1));
ALTER TABLE guild_settings ADD COLUMN muted_squawks TEXT NOT NULL DEFAULT '';
