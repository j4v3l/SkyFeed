# SkyFeed

[![CI](https://github.com/j4v3l/SkyFeed/actions/workflows/ci.yaml/badge.svg)](https://github.com/j4v3l/SkyFeed/actions/workflows/ci.yaml)
[![License](https://img.shields.io/github/license/j4v3l/SkyFeed)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Release](https://img.shields.io/github/v/release/j4v3l/SkyFeed?include_prereleases&label=preview)](https://github.com/j4v3l/SkyFeed/releases)
[![GHCR](https://img.shields.io/badge/GHCR-skyfeed-blue?logo=github)](https://github.com/j4v3l/SkyFeed/pkgs/container/skyfeed)

**Local-first ADS-B Discord bot with a private path for invited community [readsb](https://github.com/wiedehopf/readsb)/tar1090 feeders.**

SkyFeed polls your receiver once per second, keeps immutable in-memory
snapshots, evaluates indexed alert rules, and serves native Discord slash
commands **without** the privileged Message Content intent. SQLite, Discord, and
enrichment stay off the ingest critical path.

| | |
| --- | --- |
| **Primary source** | Local readsb JSON (`/data`) |
| **Optional fallback** | [airplanes.live](https://airplanes.live) point query around a *public* airport reference |
| **Enrichment** | Opt-in adsb.lol / ADSBDB (callsigns & public positions only) |
| **Deploy** | Docker Compose (Pi/ARM64 + amd64) or Nix / NixOS flake |
| **Privacy** | No Message Content intent; health/metrics omit private coordinates |

Public policy pages: [Terms](https://j4v3l.github.io/SkyFeed/legal/terms/) · [Privacy](https://j4v3l.github.io/SkyFeed/legal/privacy/)

## Table of contents

- [Quick start (Docker)](#quick-start)
- [Discord interface](#discord-interface)
- [Aircraft sources and privacy](#aircraft-sources-and-privacy)
- [Community feeders](#community-feeders)
- [Route and airport enrichment](#route-and-airport-enrichment)
- [Interesting aircraft](#interesting-aircraft-plane-alert-db)
- [Operations](#operations)
- [Nix / NixOS](#nix--nixos)
- [Development](#development-and-performance)
- [Contributing & support](#contributing--support)

## Quick start

**Requirements:** Docker Engine/Compose and a Discord application token. On a
Raspberry Pi use a **64-bit ARM64** OS.

In the [Discord Developer Portal](https://discord.com/developers/applications),
install the app with the `bot` and `applications.commands` scopes. Grant only
View Channels, Send Messages, Embed Links, Read Message History, Moderate
Members, Kick Members, and Ban Members; add Attach Files only if bounded report
attachments are enabled. Do **not** grant Administrator or Manage Roles, and do
**not** enable Message Content. Place the SkyFeed bot role above every member
role it may moderate.

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

The example profile enables only the local `readsb` source. Append
`airplanes-live` only after confirming its current availability and terms. KPBI
is the initial public weather/activity center. Discord, structured logs, health
JSON, and metrics expose only the airport code (`KPBI`), never the configured
center coordinates. Change `SKYFEED_PUBLIC_CENTER_*` only when you deliberately
choose a different published airport reference—not a private receiver site.

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml config
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml build
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed config check
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed source check
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml up -d
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml ps
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml logs -f skyfeed
```

Prefer a receiver IP in `SKYFEED_ADSB_BASE_URL`. If the receiver only serves a
named virtual host such as `adsb.local`, keep that URL and set
`SKYFEED_ADSB_HOST_IP` to its stable LAN address. The local/agent Compose files
then add an explicit container host mapping without relying on mDNS.

### Local airport weather and activity

`SKYFEED_PUBLIC_CENTER_AIRPORT_CODE`, latitude, and longitude also enable the
local-airport panel. This works with a readsb-only setup; `airplanes.live` is
optional. Use the published location of the airport, never your receiver's
location. The live dashboard includes a concise weather/activity update, and
`/airport CODE` offers a mobile-friendly overview with **Weather report** and
**Arrivals & departures** buttons.

SkyFeed labels a movement only after three compatible local ADS-B samples. It
uses distance from the airport, heading, motion toward/away, altitude,
climb/descent, speed, and ground state to identify a *likely approach*, *likely
departure*, or *likely landing*. These are useful local trends—not an official
runway, arrival, or departure feed.

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
`/airline`, `/squawk`, `/emergency`, `/traffic`, `/top live`, `/top traffic`,
`/privacy`, `/preferences units`, `/watch`, `/alerts`, `/reports`, `/audit`,
`/feeder`, `/feeders`, `/settings`, `/moderation`, and `/help`, plus **Lookup
aircraft** and **Delete with SkyFeed** message context menus. Aircraft results
begin with a concise card. The primary
row contains **Details**, **Refresh**, and **Close**; an invoker-bound **More
aircraft actions…** menu offers Track, Watch, and Route & weather when those
actions are available. Track plots are generated locally from a bounded,
memory-only 15-minute history and are never written to SQLite.
Nearby pages and component sessions expire. Buttons, select menus, HTTPS link
buttons, modals, and autocomplete use opaque versioned session IDs. Settings
and durable administration are private; allowed mentions default to none.

Use `/preferences units` to choose personal Imperial, Aviation, or Metric
measurements. Imperial is the default and uses miles, feet, miles per hour,
Fahrenheit, and inches of mercury. Aviation uses nautical miles, feet, knots,
Celsius, and inches of mercury; Metric uses kilometres, metres, kilometres per
hour, Celsius, and hectopascals. A personal choice overrides the server default
set by `/settings units`; scheduled reports, alerts, audits, flight leaders, and
the live dashboard use the server default. Command filters named `radius-nm`
and altitude-in-feet remain explicit canonical inputs regardless of display
preference.

`/alerts configure` can target the Movements category (takeoff, landing, and
approach, feeder-only) in addition to watches, emergencies, feeder health, and
interesting aircraft. Movement alerts use `#flight-alerts` unless a custom
destination is set. Approach geometry uses the configured public airport center
and never discloses receiver coordinates.

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
and post server rules. Grant `@SkyFeed Admin` **Manage Roles** and **Manage
Messages** so admins can assign Operator and Moderator roles and approve
message deletion. Grant the SkyFeed bot role **Manage Messages** so it can
perform approved deletions. Neither role needs Administrator.

```text
/settings channels purpose:Moderation log channel:#moderation-log
/settings channels purpose:Interesting aircraft channel:#interesting-aircraft
```

Viewer commands remain public. Operators see `/alerts` and `/reports` (Manage
Server) and manage server watch rules. Moderators see `/moderation` (Moderate
Members). Admins see `/settings` with Manage Server and every lower-tier
command; changing role bindings additionally requires Manage Roles.
Discord Administrators always see the full command list. Bot DMs stay Admin-only
at runtime even though Discord shows the command picker there. Moderators can
warn, timeout, remove timeouts, kick, ban, unban, and inspect bounded case
history. Kicks and bans require a private, invoker-bound confirmation that
expires after 60 seconds. Every attempted action creates a durable case; warning
DM delivery and Discord failures are recorded. Moderation log delivery retries
from a bounded SQLite outbox across restarts, and cases expire after 365 days in
bounded purge batches.

SkyFeed Admins with Manage Messages can use `/moderation delete-message` with a
Discord message link or ID, or choose **Apps → Delete with SkyFeed** on a
message. SkyFeed privately previews the target, requires a 3–400 character
reason and one-minute confirmation, then rechecks both the admin and bot
access. The moderation case stores IDs, reason, timestamps, and outcome—never
the deleted message content. Deletion records use the existing
`#moderation-log` delivery path.

Configure durable channel IDs with `/settings channels`. Names such as
`#adsb-alerts` and `#interesting-aircraft` are documentation only and are never
treated as identifiers.
Daily and weekly report schedules are delivered by the bounded outbound
scheduler and record their last successful run to prevent restart duplicates.
The reports destination also holds one persistent **Live flight leaders** card.
It is edited every five minutes by default and shows the fastest, slowest,
highest, and lowest fresh airborne aircraft across the deduplicated community
view. Set `SKYFEED_FLIGHT_LEADERS_INTERVAL=0` to disable it, or choose an
interval from `1m` through `1h`.
Operator, owner, and aircraft-type watch rules are visibly best-effort and use
only asynchronously cached ADSBDB metadata; they can never become emergencies.
Movement alerts require three consecutive compatible observations and are
labeled **likely takeoff**, **likely landing**, or **approach trend** because
they are inferred from ADS-B movement rather than authoritative airport events.

## Community feeders

SkyFeed keeps one Discord Gateway and one SQLite database, but can combine the
local receiver with up to 100 administrator-invited community receivers. The
default dashboard is a deduplicated **All feeders** view; supported commands
offer a feeder selector, and component sessions retain that scope. Landing,
departure, weather, rules, reports, and health remain isolated per feeder.

Community receivers are never exposed to SkyFeed or the internet. A
`skyfeed-agent` process polls readsb on the contributor's LAN, removes receiver
coordinates, compresses a normalized snapshot, signs it with an agent-generated
Ed25519 key, and sends it outbound. The central service stores only the public
key and durable replay sequence. It accepts no proxy or arbitrary URL command.

An Admin with Manage Server creates an ephemeral 15-minute invitation using
`/feeders invite`. `/feeders rename` changes the approved public name of either
the local receiver or an invited feeder and persists it across restarts;
`/feeders set-default` chooses the view used when a command omits its feeder
option. Other actions set the public airport/weather station, pause, rotate,
revoke, or test a feeder. Ordinary members see only approved public summaries.

Central ingress remains disabled by default. To put a private HTTPS reverse
proxy or mesh endpoint in front of loopback port 9091:

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml -f deploy/compose.ingress.yaml up -d
```

On the contributor's LAN, create `secrets/skyfeed-agent`, place the one-time
code in `secrets/skyfeed-agent/enrollment_code` with mode `0600`, set
`SKYFEED_AGENT_SERVER_URL` and the local `SKYFEED_ADSB_BASE_URL`, then run:

```sh
docker compose -f deploy/compose.agent.yaml up -d
```

After the agent reports a successful enrollment, delete only
`secrets/skyfeed-agent/enrollment_code`; keep the empty directory so Compose can
mount it on later restarts. The private key stays in the named agent data volume
with mode `0600` and never leaves that contributor's machine.

The agent keeps at most five latest-value snapshots during an outage. Redis is
intentionally absent: SQLite is authoritative for configuration and enrollment,
while bounded live state stays in memory. Redis is reconsidered only if SkyFeed
later runs multiple active application replicas with shared leadership.

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
airport data (ODbL)`). Full provider responses and live route cards remain in
bounded memory. SkyFeed stores only a derived, source-labeled route catalog and
hourly sighting counts for `/top traffic`; these rows are never populated from
ADSBDB and are purged if their provenance cannot be proven. The behavior is
covered by the same `/privacy` disclosure as other providers.

Upgrade note: migration 010 deliberately clears older derived route rankings
and rebuilds them from source-labeled adsb.lol sightings. It preserves server
settings, watch rules, moderation cases, and legacy report observation counts.

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
/settings channels purpose:High-interest aircraft channel:#high-interest-flights
/settings test purpose:Interesting aircraft
/settings test purpose:High-interest aircraft
```

The high-interest destination receives red, text-labeled cards for narrowly
matched custody and transport metadata such as Guantanamo, GTMO, offshore
detention, deportation/removal flights, rendition, detainee or prisoner
transport, Immigration and Customs Enforcement, and word-level `ICE` tags.
These labels come from a community-maintained reference list. They are useful
leads, not proof of an aircraft's operator, passengers, mission, origin, or
destination, and should be independently verified. `Police` does not match
`ICE`. If the high-interest destination is not bound, these alerts safely fall
back to the ordinary interesting-aircraft channel.

Tune delivery independently with `/alerts configure category:Interesting aircraft`
and `/alerts configure category:High-interest aircraft`. Alerts respect
`/settings pause-alerts` like other non-emergency categories.

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

Run the public preview with its semantic-version tag, or pin the digest recorded
in the GitHub Release for the strongest reproducibility:

```sh
SKYFEED_IMAGE=ghcr.io/j4v3l/skyfeed:0.1.1 docker compose --env-file .env -f deploy/compose.yaml up -d
SKYFEED_IMAGE=ghcr.io/j4v3l/skyfeed@sha256:RELEASE_DIGEST docker compose --env-file .env -f deploy/compose.yaml up -d
```

To rotate the Discord token, stop SkyFeed, replace
`secrets/discord_token` without changing its `0600` permissions, revoke the old
token in the Developer Portal, and start the service. Never paste a token into
an issue, commit, image build argument, or support log.

Disable all ADSBDB traffic immediately by setting
`SKYFEED_ADSBDB_ENABLED=false` and recreating the container.

See [operations.md](docs/operations.md) for failure and recovery procedures and
[cloud connectivity](deploy/cloud/README.md) for the private-tunnel design.

## Nix / NixOS

SkyFeed also ships a Nix flake for native binaries and hardened bot and agent
NixOS modules. Configuration uses the same `SKYFEED_*` keys as `.env.example`;
the NixOS service receives the Discord token through systemd `LoadCredential`
from a root-owned source file, never through the Nix store.

```sh
nix run github:j4v3l/SkyFeed -- version
nix run github:j4v3l/SkyFeed -- --help
```

Enable on NixOS (after creating the env file and token on the host):

```nix
inputs.skyfeed.url = "github:j4v3l/SkyFeed";
imports = [ inputs.skyfeed.nixosModules.default ];
services.skyfeed.enable = true;
```

Requires `nixpkgs-unstable` for Go 1.27 (`buildGo127Module`). See [docs/nix.md](docs/nix.md)
for the full `.env` → NixOS mapping, permissions, and module options.

## ADSBDB data-use notice

ADSBDB is opt-in and presentation-only. Route enrichment is independently off
by default. The software license and underlying dataset rights are separate;
confirm the intended private/public Discord use before enabling routes. ADSBDB
route data is transient, attributed when shown, excluded from exports,
and never stored in SQLite. Separate adsb.lol-derived route sighting analytics
may be stored for `/top traffic` with mandatory source metadata. Synthetic
route data is used in tests.

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

The current measured baseline is recorded in [performance.md](docs/performance.md).
Implemented capabilities and preview limitations are tracked in
[project-status.md](docs/project-status.md) and [roadmap.md](docs/roadmap.md).

Tests never contact Discord, a live receiver, or public enrichment APIs.

## Contributing & support

| Resource | Link |
| --- | --- |
| Contributing guide | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |
| Architecture | [docs/architecture.md](docs/architecture.md) |
| Code of conduct | [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) |
| Support / help | [SUPPORT.md](SUPPORT.md) |
| Security policy | [SECURITY.md](SECURITY.md) |
| Bug report | [New bug](https://github.com/j4v3l/SkyFeed/issues/new?template=bug_report.yml) |
| Feature request | [New feature](https://github.com/j4v3l/SkyFeed/issues/new?template=feature_request.yml) |

License: [Apache-2.0](LICENSE).
