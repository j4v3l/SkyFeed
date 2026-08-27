---
layout: default
title: Architecture
---

# Architecture

SkyFeed is a local-first ADS-B Discord bot. One active process owns the Discord
Gateway, live alert evaluation, SQLite database, and aggregate view. A directly
connected readsb receiver is the default source; invited community receivers
send normalized snapshots through outbound-only agents.

## Data flow

1. Source adapters fetch bounded readsb JSON or accept authenticated agent
   snapshots.
2. The state engine normalizes observations, calculates derived values, and
   publishes immutable snapshots through atomic pointers.
3. Time-sensitive rules run on each feeder publication. A coalesced aggregate
   snapshot supports community-wide views without delaying emergencies.
4. Bounded queues carry alerts, persistence work, enrichment, reports, and
   Discord delivery outside the ingest path.
5. Discord commands read the current in-memory view; SQLite stores durable
   configuration and aggregates rather than one-second raw tracks.

The source adapter, state engine, rule engine, enrichment services, Discord
adapter, storage layer, and operations endpoints communicate through domain
types. Discord, SQLite, and third-party DTOs do not enter the domain model.

## Live state and concurrency

Snapshots are immutable after publication. Readers load once and do not retain
old snapshots in long-lived sessions. Polling is single-flight, late ticks are
skipped, and worker counts and queues have fixed limits. Dashboard refreshes
use latest-value-wins coalescing. Discord delivery uses separate critical,
alert, and background lanes so a slow report or dashboard retry cannot delay an
emergency.

The aggregate view rebuilds at most four times per second. Duplicate ICAO
observations select the freshest valid aircraft and retain the approved feeders
that currently see it. Track history is memory-only, sampled at most every five
seconds, and capped at 5,000 aircraft with 180 points each.

## Persistence

SQLite runs in WAL mode with foreign keys, a busy timeout, embedded forward-only
migrations, and one controlled writer path. Configuration, role/channel
bindings, watch rules, alert state, feeder enrollment, moderation cases,
report rollups, and derived source-labeled route sightings survive restart.
Raw one-second snapshots and track points are not stored.

Redis and PostgreSQL are intentionally absent. They become relevant only if a
future deployment introduces multiple active application replicas and shared
leadership. See [ADR 0010](adr/0010-single-process-multi-feeder-without-redis.md).

## Providers and provenance

The receiver remains authoritative for live position, altitude, speed, track,
vertical rate, squawk, visibility, and emergency state. Optional providers may
add presentation data but cannot suppress or delay live alerts.

- ADSBDB is presentation-only; route data is transient and excluded from bulk
  exports and durable storage.
- adsb.lol may supply attributed route and airport information. Only
  source-labeled adsb.lol route sightings may enter derived traffic rankings.
- airplanes.live is an optional fallback around an explicitly public airport
  center and is never presented as receiver data.
- AviationWeather.gov supplies bounded, cached METAR and TAF information.
- plane-alert-db is downloaded for local ICAO matching; observations are not
  uploaded to it. Its classifications are community metadata to verify.

## Community feeders

`skyfeed-agent` polls a receiver on its private LAN, strips private receiver
coordinates, compresses normalized snapshots, and signs a canonical envelope
with an agent-generated Ed25519 key. Enrollment uses a one-time 256-bit code;
the central service stores only its hash during enrollment and only the public
key afterward. Sequence numbers, timestamps, payload hashes, size limits,
aircraft limits, and a bounded verification pool protect ingress from replay
and resource exhaustion.

Ingress is disabled by default and binds to loopback. Operators must place it
behind a private mesh or authenticated HTTPS reverse proxy. It never accepts a
general proxy command or arbitrary receiver URL.

## Deployment and security

The release image is a static, non-root distroless container with a read-only
root filesystem and no shell. Compose drops all Linux capabilities, mounts the
Discord token from a read-only file, and publishes health/metrics to localhost.
Nix packages both binaries; hardened NixOS services use systemd credentials.

The Discord adapter requests no Message Content intent and defaults allowed
mentions to none. Admin and moderation actions combine configured roles with
native Discord permissions and hierarchy checks. Metrics use low-cardinality
labels and omit aircraft, guild, channel, user, airport, and receiver IDs.

## Performance approach

SkyFeed optimizes measured work: bounded allocation, reusable HTTP transports,
precomputed rule indexes, immutable reads, and batched persistence. It retains
the standard JSON decoder and does not ship PGO because representative target
host profiles have not shown either change is warranted. Current measurements
and remaining target-host caveats are in [performance.md](performance.md).

Architecture decisions are recorded in the [ADR index](adr/README.md).
