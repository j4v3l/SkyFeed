ALTER TABLE guild_settings RENAME COLUMN units TO units_v1;
ALTER TABLE guild_settings ADD COLUMN units TEXT NOT NULL DEFAULT 'imperial'
    CHECK (units IN ('imperial', 'aviation', 'metric'));

ALTER TABLE user_preferences RENAME COLUMN units TO units_v1;
ALTER TABLE user_preferences ADD COLUMN units TEXT NOT NULL DEFAULT 'imperial'
    CHECK (units IN ('imperial', 'aviation', 'metric'));

-- Guild defaults intentionally move to Imperial. Explicit personal choices do not.
UPDATE user_preferences SET units = units_v1;
