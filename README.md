# SkyFeed

SkyFeed is a local-first ADS-B Discord bot for one readsb/tar1090 feeder. It
polls the receiver once per second, publishes immutable in-memory snapshots,
evaluates indexed alert rules, and serves native Discord commands without the
privileged Message Content intent. SQLite, Discord, and enrichment work remain off
the ingest critical path.

Local readsb stays the primary aircraft source. When configured, airplanes.live
provides a privacy-safe point-query fallback around a public airport reference
(KPBI by default in the bundled Compose profile). Route and airport enrichment
via adsb.lol is opt-in at the HTTP layer and uses only callsigns and already-
public aircraft positions. Tests continue to use deterministic, privacy-reviewed
synthetic fixtures so no receiver-specific observations enter the repository.

Public verification pages:

- [Terms of Service](https://skyfeed-policies.javel-palmer.chatgpt.site/terms)
- [Privacy Policy](https://skyfeed-policies.javel-palmer.chatgpt.site/privacy)

## Quick start

Requirements are Docker Engine/Compose and a Discord application token. On a
Raspberry Pi use a 64-bit ARM64 operating system.

In the Discord Developer Portal, install the application with the `bot` and
`applications.commands` scopes. Grant only View Channels, Send Messages, Embed
Links, Read Message History, Moderate Members, Kick Members, and Ban Members;
add Attach Files only if bounded report attachments are enabled. Do not grant
Administrator or Manage Roles, and do not enable Message Content. Place the
SkyFeed bot role above every member role it may moderate—Discord hierarchy is
still enforced for the bot and the invoking moderator.

```sh
cp .env.example .env
mkdir -p secrets
chmod 700 secrets
printf '%s' 'TOKEN_FROM_DISCORD_DEVELOPER_PORTAL' > secrets/discord_token
chmod 600 secrets/discord_token
```

Edit `.env` locally. Use a stable receiver IP or ordinary LAN DNS hostname and
leave the base URL ending in `/data`; do not assume `.local` mDNS works in the
CGO-disabled image.

The example profile enables `readsb,airplanes-live` with KPBI as the public
fallback center and a 50 NM query radius. Discord, structured logs, health JSON,
and metrics expose only the airport code (`KPBI`), never the configured center
coordinates. Change `SKYFEED_PUBLIC_CENTER_*` only when you deliberately choose a
different published airport reference—not a private receiver site.

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml config
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml build
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed config check
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed source check
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml up -d
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml ps
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml logs -f skyfeed
```

Guild-scoped development commands synchronize idempotently at startup. A
release operator may set `SKYFEED_DISCORD_GLOBAL_COMMANDS=true` after first
validating the schema in a development guild. To sync the selected scope
explicitly without opening the Gateway:

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed commands sync
```

The image runs as the distroless `nonroot` user with a read-only root,
capabilities dropped, no shell, and the token mounted read-only below
`/run/secrets`. Discord Gateway mode needs no inbound public port. Health and
metrics bind only to `127.0.0.1:9090` on the host.

## Discord interface

SkyFeed registers `/status`, `/nearby`, `/aircraft`, `/route`, `/airport`,
`/squawk`, `/top`, `/privacy`, `/watch`, `/alerts`, `/reports`, `/feeder`,
`/settings`, `/moderation`, and `/help`. Nearby pages are bound to their
initiating user and expire. Buttons, select menus, modals, and autocomplete use
opaque versioned session IDs. Settings and durable administration are private;
allowed mentions default to none.

`/privacy` is ephemeral and mirrors the same typed disclosure returned by health
endpoints: provider names, the public airport code, query radius, retention, and
attribution. It never includes coordinates, receiver URLs, guild IDs, or other
deployment identifiers.

Role access uses existing Discord roles and never creates or edits them. A
Discord Administrator bootstraps the first binding, after which privileged
users need the configured tier and the native Discord permission for the
action:

```text
/settings roles bind tier:Admin role:@SkyFeed Admin
/settings roles bind tier:Operator role:@SkyFeed Operator
/settings roles bind tier:Moderator role:@SkyFeed Moderator
/settings roles list
```

Set `SKYFEED_DISCORD_ADMIN_ROLE_ID`, `SKYFEED_DISCORD_OPERATOR_ROLE_ID`, and
`SKYFEED_DISCORD_MODERATOR_ROLE_ID` in `.env` to auto-bind these on startup, or
run `python3 scripts/setup-discord-governance.py` to apply channel permissions
and post server rules. Grant `@SkyFeed Admin` the **Manage Roles** permission in
Discord so admins can assign Operator and Moderator roles to members.

```text
/settings channels purpose:Moderation log channel:#moderation-log
/settings channels purpose:Interesting aircraft channel:#interesting-aircraft
```

Viewer commands remain public. Operators manage server watch rules, alerts,
and report schedules. Moderators can warn, timeout, remove timeouts, kick, ban,
unban, and inspect bounded case history. Kicks and bans require a private,
invoker-bound confirmation that expires after 60 seconds. Every attempted
action creates a durable case; warning DM delivery and Discord failures are
recorded. Moderation log delivery retries from a bounded SQLite outbox across
restarts, and cases expire after 365 days in bounded purge batches.

Configure durable channel IDs with `/settings channels`. Names such as
`#adsb-alerts` and `#interesting-aircraft` are documentation only and are never
treated as identifiers.
Daily and weekly report schedules are delivered by the bounded outbound
scheduler and record their last successful run to prevent restart duplicates.
Operator, owner, and aircraft-type watch rules are visibly best-effort and use
only asynchronously cached ADSBDB metadata; they can never become emergencies.

## Aircraft sources and privacy

| Provider | Role | Notes |
| --- | --- | --- |
| `readsb` | Primary | Local receiver JSON (`/data/aircraft.json`, `receiver.json`, `stats.json`). Supplies receiver metadata and statistics. |
| `airplanes-live` | Ordered fallback | Point query around the configured public airport center when readsb aircraft data is unavailable. Never merged into readsb snapshots. |

airplanes.live is a non-commercial community feed with no SLA. SkyFeed enforces
its client-side rate limit of one request per second and caps the configured
radius at 250 NM. Use only for non-commercial SkyFeed operation and confirm the
current provider terms before production use.

When airplanes.live is active, Discord and logs identify the public center by
airport code only (for example `KPBI`). Center latitude and longitude exist
only in local configuration and outbound provider requests; they are not stored
in SQLite, rendered in embeds, emitted in metrics labels, or written to health
JSON.

Disable external aircraft fallback immediately by removing `airplanes-live` from
`SKYFEED_AIRCRAFT_PROVIDER_ORDER` or by clearing the public-center settings and
recreating the container.

## Route and airport enrichment

adsb.lol route and airport enrichment is enabled by default in the example
profile (`SKYFEED_ADSBLOL_ENABLED=true`). `/route` and `/airport` resolve data
from a bounded in-memory cache. Prefetch batches up to 50 visible aircraft and
sends only normalized callsigns plus each aircraft's already-public position to
`POST /api/0/routeset`; airport lookups use `GET /api/0/airport/{icao}`. No
receiver or home position is transmitted.

Route and airport responses are attributed in Discord (`adsb.lol route and
airport data (ODbL)`). Cached enrichment is transient, excluded from SQLite
exports, and covered by the same `/privacy` disclosure as other providers.

Disable adsb.lol traffic immediately by setting `SKYFEED_ADSBLOL_ENABLED=false`
and recreating the container.

## Interesting aircraft (plane-alert-db)

SkyFeed matches aircraft seen by your **local readsb feeder** against the
community [plane-alert-db](https://github.com/sdr-enthusiasts/plane-alert-db)
ICAO list (Mil, Gov, Pol, Civ). The reference CSV is downloaded on startup and
refreshed daily (`SKYFEED_PLANE_ALERT_REFRESH`, default `24h`). No coordinates
are sent to plane-alert-db—only local ICAO hex matching against a cached SQLite
reference table.

Each ICAO triggers **one first-sighting alert per guild** (not every overflight).
airplanes.live fallback aircraft are excluded; only `readsb` provider sightings
qualify.

Create a read-only Discord channel (deny Send Messages for `@everyone`; allow
the bot to Send Messages and Embed Links—the same pattern as `#flight-alerts`):

```text
/settings channels purpose:Interesting aircraft channel:#interesting-aircraft
/settings test purpose:Interesting aircraft
```

Tune delivery with `/alerts configure category:Interesting aircraft`. Alerts
respect `/settings pause-alerts` like other non-emergency categories.

Enable in `.env`:

```env
SKYFEED_PLANE_ALERT_ENABLED=true
SKYFEED_PLANE_ALERT_REFRESH=24h
```

Health JSON reports a `planealert` component when matching is active.

## Operations

Local endpoints:

```sh
curl --fail http://127.0.0.1:9090/livez
curl --fail http://127.0.0.1:9090/readyz
curl --fail http://127.0.0.1:9090/healthz
curl --fail http://127.0.0.1:9090/metrics
```

`/livez`, `/readyz`, and `/healthz` return JSON snapshots. Each includes the same
typed `privacy` object as `/privacy`: provider names, public airport code, query
radius in nautical miles, retention categories, and attribution notices. An empty
airport code and zero radius mean no external point-query source is configured.
No coordinate, receiver URL, guild ID, or other deployment-identifier field is
present.

Aggregate health also reports component status for `aircraft_source`,
`receiver_source`, `stats_source`, `adsbdb`, `adsblol`, and `planealert`.
Readiness requires a known aircraft source, Discord Gateway readiness, and SQLite
initialization.

Prometheus-style metrics use fixed low-cardinality labels only (`provider`,
`capability`, `priority`, `result`, `kind`). Useful series for the expanded
provider stack:

| Metric | Meaning |
| --- | --- |
| `skyfeed_aircraft_provider_active{provider}` | Which aircraft provider currently supplies snapshots (`readsb` or `airplanes-live`). |
| `skyfeed_source_*{provider,capability}` | Per-provider request counts, errors, latency, payload bytes, capability support, and health for `aircraft`, `receiver`, and `statistics`. |
| `skyfeed_adsbdb_*` | Optional ADSBDB enrichment cache and circuit metrics when `SKYFEED_ADSBDB_ENABLED=true`. |
| `skyfeed_adsblol_*` | adsb.lol route/airport cache, queue, batch, and circuit metrics when route enrichment is enabled. |
| `skyfeed_snapshot_aircraft`, `skyfeed_snapshot_age_seconds` | Current snapshot size and age. |

Metrics never label ICAO, callsign, guild, channel, user IDs, airport codes, or
coordinates.

Create a consistent SQLite backup inside the persistent volume:

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml exec skyfeed \
  /skyfeed backup /var/lib/skyfeed/skyfeed-backup.db
```

Restore only while the application is stopped. The restore validates the
backup and preserves the prior database beside it with a `.pre-restore-*`
suffix:

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml stop skyfeed
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed \
  restore /var/lib/skyfeed/skyfeed-backup.db
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml up -d
```

Upgrade and rollback with immutable image tags:

```sh
SKYFEED_IMAGE=ghcr.io/j4v3l/skyfeed:1.0.1 docker compose --env-file .env -f deploy/compose.yaml up -d
SKYFEED_IMAGE=ghcr.io/j4v3l/skyfeed:1.0.0 docker compose --env-file .env -f deploy/compose.yaml up -d
```

To rotate the Discord token, stop SkyFeed, replace
`secrets/discord_token` without changing its `0600` permissions, revoke the old
token in the Developer Portal, and start the service. Never paste a token into
an issue, commit, image build argument, or support log.

Disable all ADSBDB traffic immediately by setting
`SKYFEED_ADSBDB_ENABLED=false` and recreating the container.

See [operations.md](docs/operations.md) for failure and recovery procedures and
[cloud connectivity](deploy/cloud/README.md) for the private-tunnel design.

## ADSBDB data-use notice

ADSBDB is opt-in and presentation-only. Route enrichment is independently off
by default. The software license and underlying dataset rights are separate;
confirm the intended private/public Discord use before enabling routes. Route
data is transient, attributed when shown, excluded from exports, and never
stored in SQLite. Synthetic route data is used in tests.

## Development and performance

Go 1.27.0 is pinned. Run:

```sh
make check
go test -run=^$ -bench=. -benchmem ./internal/state ./internal/rules
go run ./test/replay -iterations 100
go run ./test/soak -duration 24h
```

The Docker release target builds `linux/amd64` and `linux/arm64`, emits SBOM and
provenance attestations, scans dependencies/images, and signs pushed images.
PGO is deliberately deferred until a representative ARM64 profile exists; see
[ADR 0007](docs/adr/0007-pgo-deferred.md).

The current measured baseline and remaining release gates are recorded in the
[implementation checkpoint](docs/checkpoints/implementation-status.md).

Tests never contact Discord, a live receiver, or public enrichment APIs. See
[SECURITY.md](SECURITY.md) for private vulnerability reporting.
