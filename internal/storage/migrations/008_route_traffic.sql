CREATE TABLE route_catalog (
    callsign TEXT PRIMARY KEY,
    airline_name TEXT NOT NULL DEFAULT '',
    airline_icao TEXT NOT NULL DEFAULT '',
    airline_iata TEXT NOT NULL DEFAULT '',
    origin_icao TEXT NOT NULL DEFAULT '',
    origin_iata TEXT NOT NULL DEFAULT '',
    origin_name TEXT NOT NULL DEFAULT '',
    origin_country_iso TEXT NOT NULL DEFAULT '',
    destination_icao TEXT NOT NULL DEFAULT '',
    destination_iata TEXT NOT NULL DEFAULT '',
    destination_name TEXT NOT NULL DEFAULT '',
    destination_country_iso TEXT NOT NULL DEFAULT '',
    plausible INTEGER NOT NULL DEFAULT 1 CHECK (plausible IN (0, 1)),
    plausibility_known INTEGER NOT NULL DEFAULT 0 CHECK (plausibility_known IN (0, 1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE route_sightings (
    guild_id INTEGER NOT NULL,
    icao TEXT NOT NULL,
    callsign TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    sightings INTEGER NOT NULL DEFAULT 1 CHECK (sightings > 0),
    PRIMARY KEY (guild_id, icao, bucket_start),
    FOREIGN KEY (guild_id) REFERENCES guild_settings(guild_id) ON DELETE CASCADE
);

CREATE INDEX route_sightings_guild_bucket ON route_sightings(guild_id, bucket_start);
CREATE INDEX route_sightings_callsign ON route_sightings(callsign);
