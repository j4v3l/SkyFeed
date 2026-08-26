# SkyFeed

## Codex build plan for a high-performance, Dockerized ADS-B Discord bot

| Field | Decision |
|---|---|
| Call name | SkyFeed |
| Edition | Discord |
| Deployment | Local-first, cloud-ready |
| Primary language | Go 1.27.x, pinned to the current supported patch release during Phase 0 |
| Discord library | github.com/disgoorg/disgo |
| Live ADS-B source | readsb/tar1090 JSON at http://adsb.local/data |
| Optional enrichment | ADSBDB |
| Plan status | Implemented roadmap; target-host and live-guild acceptance gates remain |

---

## Implementation status

| Phase | Status | Evidence |
|---|---|---|
| Phase 0 | Implemented | Repository/dependency spikes pass, sanitized fixtures are retained, and the final container reaches and decodes all three receiver endpoints. See `docs/checkpoints/phase-0.md`. |
| Phase 1 | Implemented | Configuration, CLI, logging, lifecycle, health, metrics, pprof guard, and graceful shutdown tests pass. |
| Phase 2 | Implemented | Fixed-path bounded ingestion, immutable snapshots, source health, replay, benchmarks, and live receiver reconciliation pass. |
| Phase 3 | Implemented; guild smoke gate open | Twenty slash commands plus the aircraft context command, layered aircraft/weather/track actions, personal and guild units, native components, deferral, sessions, registration scopes, dashboard, fixed outbound priority lanes, and fake interaction tests pass. A real development-guild smoke test remains. |
| Phase 4 | Implemented | WAL SQLite, ten forward migrations, provenance-gated route analytics, backup/restore, pruned rules/cooldowns, transition-based emergency reporting, coalesced rollups, reports, schedules, and aligned permission tests pass. |
| Phase 5 | Implemented | Bounded asynchronous ADSBDB client/service, configurable cache policy, best-effort enrichment rules, attribution, and synthetic fault tests pass. |
| Phase 6 | Implemented; guild smoke gate open | Hardened distroless images build and run for linux/amd64 and linux/arm64; both pass final-image fixture checks, and the final container reaches the live receiver. Discord acceptance awaits the operator's guild ID. |
| Phase 7 | Optimized baseline complete; soak gate open | Snapshot metadata publications reuse immutable indexes, caches and tracks are bounded, and expanded benchmark/replay coverage exists. The required 24-hour intended-host soak and representative PGO decision remain release gates. |
| Phase 8 | Implemented for selected pattern | Private-tunnel deployment is documented and the future agent envelope is validated. Agent transport/PostgreSQL/leadership remain intentionally conditional because no multi-replica cloud platform was selected. |
| Phase 9 | Implemented; release gate open | CI/release workflows, license/vulnerability checks, operations, backup, upgrade, rollback, and token rotation documentation exist. v1.0 tagging and live disaster-recovery rehearsal await the open acceptance gates. |

---

### Trust, presentation, and information roadmap checkpoint — 2026-08-25

- [x] Typed live/enrichment/route provenance prevents ADSBDB routes from entering durable traffic rankings; migration 010 purges unproven derived route data.
- [x] Reports distinguish observations, peak tracked aircraft, and emergency transitions; rollups flush in coalesced 15-second batches.
- [x] Rule, weather, enrichment, interaction, track, and plot state have explicit retention and capacity bounds.
- [x] Route prefetch admission is typed and rotates fairly; Discord delivery uses isolated critical, alert, and background lanes.
- [x] Plane Alert and all HTTP adapters enforce body, redirect, timeout, and malformed-response protections with provider URL allowlists.
- [x] Discord uses concise layered cards, centralized emergency meanings, composite attribution, personal/guild units, plain-language weather, and permission-aware help.
- [x] Movement notifications require three compatible samples and are explicitly labeled as inferred trends.
- [x] Recent tracks are memory-only, sampled at most every five seconds, capped at 180 points for up to 2,000 aircraft, and rendered locally on demand.
- [x] Metadata-only snapshots reuse immutable aircraft/index/search data; search sorting no longer builds joined allocation-heavy keys.
- [ ] Complete the 24-hour ARM64 soak and live development-guild acceptance before v1.0.

---

## 1. Mission

Build a clean, idiomatic, production-grade Discord bot that turns one ADS-B feeder into a responsive server interface for:

- live receiver status;
- nearby-aircraft views;
- individual aircraft lookups;
- watchlists and meaningful alerts;
- emergency and squawk notifications;
- daily or on-demand reports;
- feeder-health monitoring;
- optional aircraft and route enrichment from ADSBDB.

The bot will run locally first, in Docker Compose, on the same LAN as the receiver. The design must also support a later cloud deployment without exposing the receiver directly to the public internet.

“Highest possible performance” means:

- measured against explicit latency, CPU, memory, and stability targets;
- zero database or third-party API work in the ingest hot path;
- bounded concurrency and queues;
- no unbounded goroutine creation;
- one immutable in-memory snapshot for lock-light reads;
- minimal container and operating-system overhead;
- profile-guided optimization only after representative profiling;
- no unsafe optimization that weakens correctness, security, or maintainability.

At the expected one-second receiver cadence, Discord network latency and Discord rate limits will usually dominate CPU time. SkyFeed must optimize its own work while respecting those external limits.

---

## 2. Locked technical decisions

| Area | Decision | Reason |
|---|---|---|
| Language | Go | Excellent concurrency, low memory use, fast startup, static builds, strong profiling, and first-class container behavior |
| Discord SDK | disgo | Active, idiomatic Go SDK covering Gateway, REST, interactions, commands, components, modals, and voice without requiring separate UI packages |
| Discord connection | Gateway | Local deployment needs only outbound WebSocket and HTTPS connections; no public inbound interaction endpoint |
| Discord UI | Native application commands, embeds, buttons, select menus, modals, autocomplete, and ephemeral responses | Matches Discord’s interaction model and keeps the interface accessible on desktop and mobile |
| Live state | Immutable in-memory snapshots published through atomic.Pointer | Commands read current state without database latency or broad locks |
| Local persistence | SQLite in WAL mode behind a repository interface | Reliable single-node persistence with little operational overhead |
| SQLite driver | Start with modernc.org/sqlite | Pure Go, cross-platform, and compatible with a static distroless image; benchmark before changing |
| ADSBDB | Optional asynchronous enrichment adapter | Enrichment improves presentation but must never determine live or safety-sensitive state |
| HTTP | One reusable standard-library client per upstream | Connection reuse, explicit timeouts, bounded bodies, and no unnecessary abstraction |
| Container | Multi-stage build plus distroless non-root runtime | Small attack surface and no build tools or shell in production |
| Architectures | linux/amd64 and linux/arm64 | Supports PCs, servers, Raspberry Pi-class ARM64 hosts, and common cloud platforms |
| Local orchestration | Docker Compose | Simple repeatable deployment with a persistent volume and hardened defaults |
| Cloud scale model | One active bot/alert leader; active-passive failover | A single feeder and Discord application do not benefit from uncontrolled active-active processing |
| Cloud receiver access | Private VPN or outbound LAN agent | Never publicly expose readsb/tar1090 JSON |
| Observability | Structured logs, low-cardinality Prometheus metrics, health/readiness endpoints, opt-in pprof | Enough evidence to tune performance without making telemetry the workload |

Do not include Telegram packages in the Discord edition. Discord’s own interaction components replace go-telegram/ui.

---

## 3. Verified receiver contract

The currently working feeder endpoints are:

| Payload | URL | Planned cadence |
|---|---|---:|
| Aircraft | http://adsb.local/data/aircraft.json | 1 second |
| Receiver | http://adsb.local/data/receiver.json | 30 seconds |
| Statistics | http://adsb.local/data/stats.json | 30 seconds |

Known non-working paths:

- http://adsb.local/tar1090/data/... currently returns 404.
- http://adsb.local/readsb/data/... is routed to the readsb API port and does not serve the static files.

The three working endpoints currently return valid JSON without authentication and include CORS headers. CORS is irrelevant to the Go service, but the lack of authentication means the feeder must remain on a trusted LAN or private network.

Implementation requirements:

- Make the base URL configurable.
- Join the fixed relative paths safely; do not accept arbitrary URLs from Discord commands.
- Use an IP address or LAN DNS hostname in Docker configuration when possible.
- Do not assume that mDNS names ending in .local resolve from a CGO-disabled container.
- Use receiver timestamps and local fetch timestamps to detect stale data.
- Reject oversized or invalid payloads without replacing the last known-good snapshot.
- Preserve unknown JSON fields by ignoring them; tolerate additive readsb schema changes.
- Record source health separately for aircraft, receiver, and stats payloads.

---

## 4. Product behavior

### 4.1 Server layout

SkyFeed should support these configurable channels:

| Channel | Purpose |
|---|---|
| #adsb-live | One pinned or configured dashboard message updated at a controlled interval |
| #adsb-alerts | Watchlist, geofence, altitude, operator, and other normal alerts |
| #adsb-emergencies | Emergency squawks and explicitly high-priority alerts |
| #adsb-reports | Scheduled summaries and requested reports |
| #adsb-admin | Startup, degraded-source, recovery, and operational notifications |

All channel IDs must be configuration values. Channel names are documentation only and must never be used as durable identifiers.

### 4.2 Slash commands

| Command | Behavior |
|---|---|
| /status | Receiver health, last refresh, tracked-aircraft count, message rate, range summary, bot uptime, and enrichment status |
| /nearby | Paginated nearby aircraft with radius, altitude, limit, and sort filters |
| /aircraft | Detailed current aircraft card selected by ICAO, registration, or callsign autocomplete |
| /watch | Add, remove, enable, disable, and list personal or server watch rules |
| /alerts | View and configure alert categories, cooldowns, and destinations |
| /reports | Generate a period report or manage scheduled reports |
| /feeder | Receiver, stats, range, and source-diagnostic information |
| /settings | Administrator-only server configuration and test actions |
| /help | Short task-oriented guide and permission-aware command list |

Commands must use Discord option types, validation, autocomplete, and subcommands rather than parsing free-form messages. Do not request the privileged Message Content intent.

### 4.3 Interaction rules

- Acknowledge or defer every interaction immediately; the engineering target is under 100 ms and Discord’s hard initial-response window must never be approached.
- Use ephemeral responses for settings, errors, watchlist administration, and permission failures.
- Use public responses only when they provide shared value.
- Give buttons stable custom IDs containing a version and opaque session ID, not raw user input.
- Bind paginated sessions to the initiating user unless a component is explicitly collaborative.
- Expire interaction sessions and delete their in-memory state.
- Validate guild, channel, role, and user authorization again on every component interaction.
- Render a useful fallback if an aircraft disappears between command selection and response.

### 4.4 Visual identity

The Discord UI must look like SkyFeed rather than a generic bot:

- dark radar-inspired palette;
- consistent title prefix and footer;
- compact status badges for live, stale, degraded, and offline;
- consistent field order across aircraft cards;
- nautical miles, feet, knots, and degrees as default aviation units;
- optional metric units at the guild or user level;
- accessible text equivalents for color-coded states;
- timestamps rendered with Discord timestamp syntax;
- links only to allowlisted external domains;
- concise cards that remain within Discord embed limits.

Initial presentation tokens:

| Token | Value | Use |
|---|---|---|
| Radar | #35D07F | Live and healthy |
| Scope | #37B5FF | Informational and selected states |
| Caution | #F3B63A | Stale, degraded, or attention needed |
| Emergency | #F05252 | Emergency squawk or critical source failure |
| Muted | #6B7280 | Unknown, unavailable, or secondary metadata |
| Title | SkyFeed • {view} | Consistent embed title pattern |
| Footer | Live readsb data • ADSBDB enrichment when shown | Clear source separation |

Suggested aircraft card:

1. Callsign, registration, aircraft type, and flag.
2. Distance and bearing from the receiver.
3. Altitude, ground speed, track, and vertical rate.
4. Squawk and alert state.
5. Owner/operator and manufacturer from ADSBDB when cached.
6. Route summary from ADSBDB when available and permitted.
7. Data-age footer plus ADSBDB attribution when enrichment is shown.

Create one internal presentation package that converts domain view models into Discord embeds and components. Domain and storage packages must not import Discord types.

---

## 5. ADSBDB integration

### 5.1 Role

ADSBDB is an optional presentation-enrichment provider. The receiver remains authoritative for:

- whether an aircraft is currently visible;
- position, altitude, speed, track, vertical rate, squawk, and message age;
- emergency detection;
- feeder-health state.

ADSBDB may add:

- aircraft type;
- ICAO type;
- manufacturer;
- registration;
- owner and owner country;
- operator flag;
- photo links;
- callsign route;
- airline;
- origin, midpoint, and destination.

Never suppress, delay, or change a live alert because ADSBDB is slow, unavailable, rate-limited, or returns different metadata.

### 5.2 API contract

Use:

- base URL: https://api.adsbdb.com/v0/
- combined lookup: /aircraft/{MODE_S}?callsign={CALLSIGN}

Normalize MODE_S/ICAO to uppercase hexadecimal and callsign to trimmed uppercase before cache-key generation.

The response may contain:

- aircraft and route;
- aircraft only;
- not found;
- an upstream error.

Treat every field as optional in the presentation layer.

### 5.3 Client decision

Phase 0 must compare:

1. a small internal typed client using net/http and encoding/json; and
2. github.com/nint8835/go-adsbdb pinned to a reviewed commit.

The preferred initial choice is the small internal client because the API surface needed by SkyFeed is narrow and the Go wrapper did not have a stable tagged release when this plan was written. Select the wrapper only if the spike proves that it reduces code without weakening timeout, body-limit, error, observability, and compatibility behavior.

Whichever implementation is selected must live behind:

~~~go
type Enricher interface {
    Lookup(ctx context.Context, icao, callsign string) (Enrichment, error)
}
~~~

No ADSBDB DTO may escape the adapter package.

### 5.4 Performance and resilience

- Never call ADSBDB from the receiver polling goroutine.
- Enqueue enrichment only for a newly seen ICAO or a meaningful callsign change.
- Use a bounded worker pool; start with 2 workers and benchmark.
- Use singleflight keyed by normalized ICAO and callsign.
- Use a token-bucket limiter even if the public service does not document a fixed quota.
- Use a short total request timeout; start at 2 seconds.
- Set connect, TLS handshake, response-header, and idle-connection timeouts.
- Limit response bodies before decoding.
- Retry only transient network failures, 429, and selected 5xx responses.
- Honor Retry-After.
- Use exponential backoff with jitter and a strict attempt/time budget.
- Open a circuit after repeated failures and probe recovery at a low rate.
- Serve cached enrichment while refreshing in the background.
- Do not log once per lookup in steady state.

Starting cache policy:

| Result | Memory TTL | Durable storage |
|---|---:|---|
| Aircraft metadata | 7–30 days | Allowed only after terms review |
| Route data | 6–12 hours | Disabled by default |
| Not found | 1–6 hours | No |
| Transient error | 15–60 seconds | No |

Make TTLs configurable within safe bounds.

### 5.5 Data-use guardrail

The ADSBDB software license and the underlying data rights are separate. ADSBDB’s repository notes restrictions on copying, publishing, or incorporating the route dataset into another database without permission.

Therefore:

- show route information only after confirming that the intended private/public Discord use is permitted, then keep it transient and clearly attributed;
- do not include route data in bulk exports;
- do not build a durable local route database by default;
- use synthetic route data in fixtures and snapshots;
- complete a terms/data-license review before enabling durable route storage or public redistribution;
- provide a configuration switch that disables route enrichment while keeping aircraft enrichment;
- document the attribution string and link in the Discord renderer.

---

## 6. Architecture

~~~mermaid
flowchart TD
    A["readsb JSON source"] --> B["Poll and normalize"]
    B --> C["Immutable live snapshot"]
    C --> D["Rules and alert queue"]
    C --> E["Discord commands and dashboard"]
    C --> F["Async ADSBDB enrichment"]
    F --> E
    D --> G["Discord outbound scheduler"]
    E --> G
    D --> H["SQLite writer"]
~~~

### 6.1 Package boundaries

1. **Source adapter**
   - fetches receiver JSON;
   - validates HTTP and payload bounds;
   - maps source DTOs into domain observations.

2. **State engine**
   - normalizes observations;
   - computes distance, bearing, and freshness;
   - produces an immutable snapshot;
   - publishes it atomically.

3. **Rule engine**
   - consumes each published snapshot;
   - evaluates indexed rules;
   - emits deduplicated domain alerts.

4. **Enrichment service**
   - consumes low-priority lookup requests;
   - maintains bounded caches;
   - writes presentation-safe enrichment alongside live state.

5. **Discord adapter**
   - registers commands;
   - handles interactions and components;
   - renders domain view models;
   - schedules outbound messages while respecting rate limits.

6. **Persistence**
   - asynchronously writes configuration, watchlists, cooldown state, and reports;
   - reads startup state before Gateway readiness;
   - never sits on the one-second ingest critical path.

7. **Operations**
   - health, readiness, metrics, logs, graceful shutdown, and optional profiling.

### 6.2 Hot path

The hot path is:

1. fetch aircraft.json;
2. bound and decode JSON;
3. normalize the current observation set;
4. compute derived distance/bearing values;
5. publish an immutable snapshot;
6. evaluate live rules;
7. enqueue alerts.

The hot path must not:

- access SQLite;
- call Discord;
- call ADSBDB;
- perform DNS for each aircraft;
- create one goroutine per aircraft;
- emit one log entry per aircraft;
- allocate presentation embeds;
- download photos;
- wait on report generation.

### 6.3 Snapshot model

Use one immutable Snapshot containing:

- source generation timestamp;
- local fetch and publish timestamps;
- receiver metadata;
- receiver statistics summary;
- aircraft slice sorted by stable internal criteria;
- map by normalized ICAO;
- secondary indexes required for command autocomplete and rules;
- current source-health state;
- enrichment references or immutable cached values.

Build a complete next snapshot, then publish it with atomic.Pointer[Snapshot]. Readers load once and never mutate it.

Avoid retaining old snapshots through long-lived sessions. Paginated views should store compact identifiers and filters, then re-read current state.

### 6.4 Queues and backpressure

All queues must be bounded and observable.

| Queue | Priority | Full behavior |
|---|---|---|
| Emergency alerts | Highest | Reserve capacity; block only for a short bounded duration, then persist a failure signal |
| Normal alerts | High | Coalesce duplicate events; drop oldest low-value duplicate if necessary |
| Dashboard refresh | Medium | Latest-value-wins; never accumulate refresh jobs |
| ADSBDB enrichment | Low | Drop duplicate or oldest low-priority lookup; it can be retried on a later observation |
| Persistence events | Medium | Batch; fail readiness only if durable configuration cannot be maintained |

Expose queue depth, drops, coalesces, and processing delay as metrics.

---

## 7. Repository layout

Codex should create or migrate toward:

~~~text
skyfeed/
├── cmd/
│   ├── skyfeed/
│   │   └── main.go
│   └── skyfeed-agent/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   ├── lifecycle.go
│   │   └── shutdown.go
│   ├── config/
│   │   ├── config.go
│   │   ├── validate.go
│   │   └── config_test.go
│   ├── domain/
│   │   ├── aircraft.go
│   │   ├── snapshot.go
│   │   ├── alert.go
│   │   ├── enrichment.go
│   │   └── units.go
│   ├── source/
│   │   ├── source.go
│   │   └── readsb/
│   │       ├── client.go
│   │       ├── dto.go
│   │       ├── normalize.go
│   │       └── fixtures_test.go
│   ├── state/
│   │   ├── engine.go
│   │   ├── snapshot.go
│   │   ├── geo.go
│   │   └── benchmark_test.go
│   ├── rules/
│   │   ├── engine.go
│   │   ├── index.go
│   │   ├── cooldown.go
│   │   └── benchmark_test.go
│   ├── enrichment/
│   │   ├── service.go
│   │   ├── cache.go
│   │   └── adsbdb/
│   │       ├── client.go
│   │       ├── dto.go
│   │       └── client_test.go
│   ├── discord/
│   │   ├── client.go
│   │   ├── commands.go
│   │   ├── permissions.go
│   │   ├── router.go
│   │   ├── sessions.go
│   │   ├── outbound.go
│   │   └── render/
│   │       ├── aircraft.go
│   │       ├── status.go
│   │       ├── reports.go
│   │       └── limits.go
│   ├── storage/
│   │   ├── storage.go
│   │   ├── migrations/
│   │   └── sqlite/
│   │       ├── store.go
│   │       └── store_test.go
│   ├── report/
│   │   ├── service.go
│   │   └── aggregate.go
│   ├── telemetry/
│   │   ├── logging.go
│   │   ├── metrics.go
│   │   └── pprof.go
│   └── health/
│       ├── server.go
│       └── check.go
├── test/
│   ├── fixtures/
│   ├── integration/
│   ├── replay/
│   └── soak/
├── deploy/
│   ├── compose.yaml
│   ├── compose.local.yaml
│   ├── compose.host.yaml
│   ├── compose.cloud.yaml
│   ├── systemd/
│   └── cloud/
├── .github/
│   └── workflows/
│       ├── ci.yaml
│       └── release.yaml
├── Dockerfile
├── .dockerignore
├── docker-bake.hcl
├── .env.example
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── SECURITY.md
├── LICENSE
└── SKYFEED_DISCORD_DOCKER_BUILD_PLAN.md
~~~

The agent command can remain a documented stub until the cloud-agent phase, but the source interface must be defined from the start so direct HTTP and agent-delivered snapshots use the same normalization contract.

---

## 8. Configuration contract

Configuration must load once, validate fully, redact secrets, and fail fast with actionable errors.

| Setting | Required | Initial/default behavior |
|---|---|---|
| SKYFEED_DISCORD_TOKEN_FILE | Yes | Read token from a mounted file |
| SKYFEED_DISCORD_APPLICATION_ID | Yes | Discord application ID |
| SKYFEED_DISCORD_GUILD_ID | Local/dev | Guild-scoped commands for rapid updates |
| SKYFEED_ADSB_BASE_URL | Yes | Use a LAN IP in Docker, ending at /data |
| SKYFEED_AIRCRAFT_POLL | No | 1s |
| SKYFEED_METADATA_POLL | No | 30s |
| SKYFEED_DATABASE_PATH | No | /var/lib/skyfeed/skyfeed.db |
| SKYFEED_ADSBDB_ENABLED | No | true after data-use notice is accepted |
| SKYFEED_ADSBDB_ROUTE_ENABLED | No | false until terms review |
| SKYFEED_ADSBDB_BASE_URL | No | https://api.adsbdb.com/v0 |
| SKYFEED_ADSBDB_TIMEOUT | No | 2s |
| SKYFEED_ADSBDB_WORKERS | No | 2 |
| SKYFEED_DASHBOARD_INTERVAL | No | 15s |
| SKYFEED_HEALTH_ADDR | No | 0.0.0.0:9090 inside the container |
| SKYFEED_PPROF_ADDR | No | Disabled |
| SKYFEED_LOG_LEVEL | No | info |
| SKYFEED_LOG_FORMAT | No | json |
| SKYFEED_TIMEZONE | No | UTC |
| SKYFEED_GUILD_CONFIG | No | Database-managed after initial setup |

Use duration parsing with minimum and maximum bounds. Do not let a command change upstream URLs, token paths, profiling binds, or database paths.

The .env.example file must contain placeholders only. Never commit a real Discord token, LAN credential, guild ID if the repository is public, or user-specific route.

---

## 9. Discord adapter design

### 9.1 disgo coverage

Use disgo for every SkyFeed-relevant Discord operation:

- client construction and lifecycle;
- Gateway connection and reconnects;
- REST requests;
- application-command registration;
- slash command and autocomplete handling;
- deferred and immediate interaction responses;
- follow-up and edit responses;
- embeds;
- buttons;
- string select menus;
- modals;
- permissions and allowed mentions;
- graceful close.

Do not mix another Discord SDK into the same binary.

Maintain this feature-coverage matrix during Phase 3:

| disgo/Discord capability | SkyFeed use |
|---|---|
| Gateway lifecycle | Local/cloud outbound connection, resume, reconnect, shutdown |
| REST client | Command registration, messages, edits, follow-ups |
| Slash commands and subcommands | Primary navigation and all administration |
| Autocomplete | ICAO, registration, callsign, and saved-rule selection |
| Deferred and ephemeral responses | Slow reports, settings, validation, and private administration |
| Embeds | Aircraft, feeder, status, alert, and report cards |
| Buttons | Page, refresh, watch, acknowledge, and close |
| Select menus | Sort/filter choices, rule and channel selection |
| Modals | Watch-rule and settings forms |
| Attachments | Optional bounded CSV/report export after data-use review |
| Allowed mentions | Explicit safe defaults for alert delivery |
| Voice | Intentionally out of scope; it does not serve the ADS-B dashboard workflow |

Do not add features merely to exercise an SDK surface. Every enabled capability must have a tested product use.

### 9.2 Command registration

- Use guild-scoped commands in local development because they update quickly.
- Support global command registration only in release mode.
- Define commands as data and compare the desired schema with the remote schema.
- Update only when the schema changes.
- Do not delete unrelated application commands without explicit ownership checks.
- Version command migrations.
- Make registration idempotent.

### 9.3 Outbound scheduler

Create one Discord outbound service that:

- assigns emergency, alert, interaction, dashboard, and report priorities;
- coalesces dashboard edits;
- limits report bursts;
- allows disgo/Discord headers to govern route limits;
- retries only safe operations;
- attaches an idempotency key to internal jobs;
- records success, rate-limit delay, permanent failure, and retry metrics;
- prevents allowed-mention surprises by defaulting to no mentions.

Do not attempt to bypass Discord rate limits with multiple clients or tokens.

### 9.4 Session manager

Implement a small project-owned component-session manager:

- opaque random session ID;
- initiating user ID;
- guild and channel IDs;
- view/filter state;
- creation and expiry timestamps;
- maximum sessions globally and per user;
- timer-wheel or periodic cleanup rather than one timer goroutine per session.

Session data is disposable and need not be stored in SQLite.

---

## 10. Rules and alerts

Initial rule types:

- exact ICAO;
- registration;
- callsign prefix or exact callsign;
- squawk;
- emergency flag;
- distance/radius;
- altitude band;
- operator or owner after enrichment;
- aircraft type after enrichment;
- feeder stale/offline/recovered;
- first seen after an optional quiet period.

Rules depending on enrichment must be labeled best-effort and cannot be emergency rules.

Deduplication state should include:

- rule ID;
- aircraft ICAO;
- condition fingerprint;
- last-fired timestamp;
- last-clear timestamp;
- cooldown;
- hysteresis state where applicable.

Prevent alert flapping with:

- minimum consecutive observations for normal stateful rules;
- separate enter and exit thresholds for radius/altitude boundaries;
- configurable cooldowns;
- immediate behavior for recognized emergency squawks;
- recovery messages only after a stable recovery window.

Persist watch rules and enough cooldown state to avoid a restart alert storm.

---

## 11. Persistence

### 11.1 Local SQLite

Use:

- WAL journal mode;
- foreign keys;
- busy timeout;
- prepared statements;
- explicit transactions;
- batched history/report writes;
- numbered, forward-only embedded migrations;
- one controlled writer path;
- context-aware queries;
- backup documentation.

Suggested tables:

- schema_migrations;
- guild_settings;
- channel_bindings;
- role_bindings;
- user_preferences;
- watch_rules;
- alert_state;
- feeder_events;
- report_rollups;
- message_bindings;

Do not store every one-second aircraft snapshot by default. Aggregate only what is needed for reports. Raw historical retention, if later requested, should be a separate opt-in subsystem with explicit disk limits.

### 11.2 Cloud persistence

Support storage through interfaces from day one, but implement PostgreSQL only when one of these is true:

- multiple application replicas are required;
- a managed relational database is desired;
- active-passive leadership needs a shared lease;
- SQLite volume semantics are unsuitable on the selected cloud platform.

Do not add PostgreSQL to the initial local stack merely for theoretical scale.

---

## 12. Docker design

### 12.1 Dockerfile

Create a multi-stage Dockerfile:

1. pinned Go 1.27.x builder image, or the current supported stable release selected by Phase 0;
2. download modules with go.mod/go.sum cache mounts;
3. compile with CGO_ENABLED=0 for TARGETOS/TARGETARCH;
4. use -trimpath and version/commit/build-date ldflags;
5. use -ldflags="-s -w" for release images after confirming stack traces remain useful;
6. run tests in CI, not in the final image stage;
7. copy only the binary into a pinned distroless static non-root runtime;
8. run as the runtime’s non-root user;
9. include CA roots and timezone support, using the runtime bundle or Go’s time/tzdata;
10. expose only the health/metrics port as metadata.

The binary must provide:

- skyfeed run;
- skyfeed healthcheck;
- skyfeed migrate;
- skyfeed version;
- skyfeed config check.

The healthcheck subcommand is necessary because a distroless image has no shell, curl, or wget.

### 12.2 Compose baseline

The baseline service must specify:

- restart: unless-stopped;
- init: true;
- read_only: true;
- cap_drop: ALL;
- security_opt: no-new-privileges:true;
- a named data volume mounted at /var/lib/skyfeed;
- tmpfs for /tmp with a small size and safe mount options;
- a read-only token file mounted below /run/secrets;
- localhost-only publication of the operational endpoint;
- a command-based healthcheck using the binary;
- stop_grace_period long enough for Discord close and SQLite flush;
- explicit logging rotation;
- resource guidance for CPU and memory;
- no privileged mode;
- no Docker socket;
- no host PID or IPC namespace.

Starting local envelope:

| Resource | Starting value | Tuning rule |
|---|---:|---|
| CPU limit | 1 core | Raise only if replay benchmarks or pprof show sustained CPU saturation |
| Memory limit | 256 MiB | Verify 24-hour high-load soak before lowering |
| GOMEMLIMIT | About 230 MiB | Leave roughly 10% container headroom |
| File descriptors | Conservative explicit limit | Verify Gateway, HTTP keep-alive, SQLite, and observability usage |
| ADSBDB workers | 2 | Tune by latency and upstream health, not by CPU availability |

Do not set GOMAXPROCS manually at first. Current Go releases are container-aware and derive it from cgroup CPU limits. Pin it only if a benchmark on the actual host proves a benefit.

### 12.3 Secret handling

For local Compose:

1. create a host file such as secrets/discord_token;
2. store only the token in it;
3. set host permissions to 0600;
4. bind-mount it read-only to /run/secrets/discord_token;
5. point SKYFEED_DISCORD_TOKEN_FILE at that path;
6. exclude secrets/ from Git.

Do not describe normal Compose environment variables as Docker secrets. Docker’s native secrets feature is a Swarm feature. In cloud deployments, use the platform’s secret manager and mount/inject the token at runtime.

### 12.4 Multi-platform build

Release:

- linux/amd64;
- linux/arm64.

Use Docker Buildx and build cache. A release workflow should:

1. test once per supported Go version/architecture as appropriate;
2. build multi-platform images;
3. generate provenance and an SBOM;
4. scan the image and Go dependencies;
5. tag immutable semantic-version and commit-SHA images;
6. optionally tag latest only from the protected release branch;
7. publish to GHCR or the chosen registry;
8. record the image digest in release notes.

Pin base images by digest in release Dockerfiles or through an automated, reviewed dependency-update process.

---

## 13. Local networking profiles

### 13.1 Default: receiver elsewhere on the LAN

Use normal Docker bridge networking and configure:

~~~text
SKYFEED_ADSB_BASE_URL=http://192.168.x.y/data
~~~

This is the preferred local deployment because it preserves container network isolation. Give the receiver a DHCP reservation or stable LAN DNS name.

Do not rely on adsb.local until resolution is proven inside the actual final image:

~~~sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml run --rm --no-deps skyfeed source check
~~~

### 13.2 Receiver on the same Linux host

Two valid choices:

1. bridge mode with an explicit host gateway mapping and a configured host name; or
2. an opt-in compose.host.yaml using network_mode: host.

Host networking can reduce network translation overhead, but the performance difference for three tiny JSON requests is normally immaterial. Use it only when it solves same-host reachability or measurements justify it. Document that it shares the host network namespace and weakens network isolation.

Do not assume that 127.0.0.1 inside a bridge container reaches the host.

### 13.3 Operational port

Publish the health/metrics port to localhost only:

~~~text
127.0.0.1:9090:9090
~~~

No inbound port is needed for Discord Gateway mode. The receiver needs no port exposed by SkyFeed.

---

## 14. Cloud-ready networking

A cloud container cannot reach adsb.local or a private LAN address without an explicit private path.

Support two cloud patterns:

### Pattern A: private network tunnel

- Join the cloud workload and receiver LAN through WireGuard, a managed mesh VPN, or equivalent.
- Continue using the HTTP source adapter.
- Restrict network policy to the receiver and required outbound HTTPS/WSS destinations.
- Prefer this for the first cloud deployment because it changes the least application code.

### Pattern B: SkyFeed LAN agent

- Run skyfeed-agent beside the receiver.
- Poll and normalize locally.
- Send signed/compressed snapshots outbound to the cloud service over mTLS HTTPS or gRPC.
- Buffer only a small bounded amount during outages.
- Use monotonic sequence numbers, timestamps, replay protection, and source identity.
- Never accept commands that turn the agent into a general LAN proxy.
- Keep emergency evaluation local as an optional later resilience feature.

The cloud service must treat agent snapshots through the same Source interface used by the direct readsb adapter.

Cloud scaling rules:

- Keep exactly one active Discord Gateway and alert-processing leader for a normal single-guild/single-feeder deployment.
- Use active-passive replicas and a lease if high availability is required.
- Do not run two independent active alert engines against the same guild.
- Add Discord sharding only when Discord’s guild-count requirements make it necessary.
- Use PostgreSQL and shared cache/leadership only when multiple replicas exist.
- Keep the deployment region close to the selected private-network ingress and Discord connectivity.

Never solve cloud reachability with public router port-forwarding to the unauthenticated receiver JSON.

---

## 15. Performance engineering specification

### 15.1 Initial targets

Targets are measured on the intended ARM64 local host and a representative amd64 machine.

| Metric | Acceptance target |
|---|---:|
| Normalize and publish 1,000-aircraft snapshot | p99 below 25 ms |
| Evaluate all indexed rules for one 1,000-aircraft snapshot | p99 below 10 ms |
| Live alert to outbound-queue admission | p99 below 100 ms after snapshot publication |
| Cached command handler before Discord network | p95 below 25 ms |
| Interaction acknowledge/defer | p99 below 100 ms |
| Steady resident memory at normal load | below 100 MiB |
| Local CPU at typical feeder load | below 5% of one Raspberry Pi 4-class core, measured over 15 minutes |
| Queue growth | none under 10x fixture replay |
| Goroutine growth | none after 24-hour soak |
| File-descriptor growth | none after 24-hour soak |
| ADSBDB impact on ingest | zero blocking and no missed polls |
| Recovery | clean reconnect/recovery after receiver, ADSBDB, DNS, and Discord fault injection |

If the real feeder produces more than 1,000 simultaneous aircraft, replace the benchmark size with the observed peak plus at least 50% headroom.

### 15.2 Allocation and concurrency rules

- Preallocate aircraft slices and maps using the payload count where safe.
- Reuse immutable configuration and compiled rule indexes.
- Avoid interface conversions and reflection in the per-aircraft loop.
- Avoid fmt in the hot path.
- Represent ICAO in a compact normalized form internally when benchmarks justify it.
- Use value types for small domain data and pointers only where absence or sharing is meaningful.
- Keep reusable HTTP transports for readsb and ADSBDB.
- Bound idle connections and per-host connections.
- Keep polling single-flight; never overlap aircraft polls indefinitely.
- Skip a late tick rather than accumulate poll work.
- Use context cancellation for shutdown.
- Use errgroup for a fixed set of long-lived services, not unbounded work.
- Do not add object pools until allocation profiles demonstrate a durable benefit.
- Do not replace encoding/json before benchmarks show it is a significant bottleneck.

### 15.3 Database rules

- Move every normal write behind a bounded asynchronous writer.
- Batch report events in a transaction.
- Keep command reads indexed and bounded.
- Set explicit query limits.
- Add indexes only for measured queries.
- Periodically checkpoint WAL without blocking the hot path.
- Expose database write delay and failure state.

### 15.4 Discord rules

- Edit one dashboard message every 15–30 seconds instead of posting every receiver tick.
- Coalesce dashboard requests.
- Cache registered command definitions and message IDs.
- Use autocomplete from the in-memory snapshot.
- Bound page size and rendered embed fields.
- Build reports away from interaction acknowledgement; defer first, then edit the response.
- Cache media URLs or Discord attachment IDs where permitted; do not download photos during a command.

### 15.5 Profiling and PGO

Create benchmarks before tuning:

- BenchmarkDecodeAircraftJSON;
- BenchmarkNormalizeSnapshot;
- BenchmarkDistanceBearing;
- BenchmarkRuleEngine;
- BenchmarkRenderAircraft;
- BenchmarkADSBDBCache;
- BenchmarkSQLiteBatch;
- BenchmarkReplayPipeline.

Use:

- go test -bench with benchmem;
- CPU profiles;
- allocation/heap profiles;
- mutex and block profiles when contention is suspected;
- execution traces only for focused investigations.

Enable pprof only on a separate localhost or tightly protected administrative address. It must be disabled by default in cloud templates.

PGO procedure:

1. reach functional stability;
2. capture a representative CPU profile from a safe replay or controlled production-like run;
3. sanitize and store default.pgo if it contains no sensitive labels;
4. rebuild;
5. compare multiple benchmark runs with benchstat;
6. keep PGO only if it improves representative performance without regressions or build instability;
7. refresh the profile when the workload or major code paths change.

Do not claim a performance improvement without before/after evidence.

---

## 16. Reliability and failure behavior

| Failure | Required behavior |
|---|---|
| aircraft.json timeout | Keep last good snapshot, mark age, retry next cadence with bounded backoff after repeated failures |
| receiver/stats timeout | Keep live aircraft pipeline running; show partial degraded state |
| Invalid JSON | Reject payload, increment metric, retain last good snapshot, rate-limit logs |
| Receiver offline | Emit one offline alert after threshold; emit one stable recovery alert |
| ADSBDB unavailable | Open circuit, use cache, omit enrichment, leave live pipeline unaffected |
| Discord Gateway disconnect | Let disgo reconnect; continue ingest/rules; bound pending outbound work |
| Discord rate limit | Respect route/global limits; preserve emergency priority without bypassing limits |
| SQLite temporarily busy | Retry inside a bounded budget; do not stall ingest |
| SQLite unavailable/corrupt | Fail readiness for durable admin operations, preserve live read-only commands when safe, emit operational error |
| Container SIGTERM | Stop accepting work, close Gateway, flush bounded persistence work, close DB, exit within grace period |
| Clock shift | Use monotonic durations where possible; use source timestamps for data age with sanity checks |
| Restart | Restore settings/rules/cooldowns, warm no untrusted cache, register commands idempotently |

Use jitter on reconnect and periodic work so multiple replicas or restarted services do not synchronize.

---

## 17. Security

- Request only View Channels, Send Messages, Embed Links, Attach Files if used, and Read Message History where needed.
- Do not request Administrator.
- Do not request Message Content intent.
- Restrict admin commands by configured guild roles and Discord permissions.
- Validate authorization server-side on every interaction.
- Default allowed mentions to none.
- Escape or sanitize user-controlled text in embed content and filenames.
- Permit only fixed readsb and ADSBDB hosts from validated configuration.
- Enforce HTTPS for ADSBDB and agent cloud transport.
- Keep receiver HTTP on a trusted LAN/private tunnel.
- Run the container non-root with all Linux capabilities dropped.
- Use a read-only root filesystem.
- Keep dependency and image vulnerability scanning in CI.
- Redact tokens, full interaction payloads, private URLs, and user identifiers where not needed.
- Do not expose metrics with high-cardinality ICAO, callsign, guild, channel, or user labels.
- Document token rotation and Discord application recovery.
- Add SECURITY.md with private vulnerability-reporting guidance.

---

## 18. Observability

### Logs

Use structured logging with:

- level;
- component;
- stable event name;
- duration;
- result;
- error class;
- correlation/job ID where useful.

Rate-limit repeated upstream errors. Never log every aircraft observation or every successful poll at info.

### Metrics

Include:

- source request count, status class, and latency;
- source payload bytes and aircraft count;
- snapshot build duration and age;
- rule evaluation duration and matches;
- queue depth, drops, and delay by small fixed queue type;
- Discord request outcome and rate-limit delay;
- interaction acknowledgement duration;
- ADSBDB cache hit/miss, request result, latency, circuit state;
- SQLite batch size, latency, and errors;
- process CPU, memory, goroutines, and file descriptors;
- last successful aircraft/receiver/stats timestamps.

Do not use aircraft IDs, callsigns, guild IDs, or user IDs as metric labels.

### Health

- /livez: process event loop is alive.
- /readyz: configuration loaded, database initialized, initial source state known, and Discord client ready as required.
- /healthz: summarized status for the container healthcheck.
- /metrics: optional Prometheus endpoint.

The healthcheck must have strict timeouts and must not make upstream network calls on every probe.

---

## 19. Testing strategy

### Unit

- DTO normalization;
- nullable/missing readsb fields;
- distance and bearing;
- stale-state calculation;
- rule matching, hysteresis, cooldown, and dedupe;
- ADSBDB mapping and cache keys;
- Discord embed limits;
- permission policy;
- configuration validation;
- database repositories.

### Contract

- Recorded, sanitized receiver payload shapes;
- synthetic ADSBDB success, partial, not-found, 429, malformed, and timeout cases;
- Discord interaction fixtures without calling Discord;
- schema migration from every supported database version.

### Integration

- httptest readsb server plus complete ingest pipeline;
- httptest ADSBDB server plus worker/circuit/cache;
- temporary SQLite database;
- fake Discord transport or adapter boundary;
- graceful shutdown;
- dashboard coalescing and alert priority.

### Fuzz

- receiver JSON decoders;
- ADSBDB decoder;
- custom component ID parser;
- command filters;
- duration/config parsing.

### Race

Run go test -race ./... on amd64 CI and during concurrency changes.

### Replay/load

- replay a peak sanitized aircraft fixture at 1x, 10x, and 50x cadence;
- create thousands of synthetic watch rules with realistic index distribution;
- introduce slow ADSBDB, Discord, and SQLite consumers;
- verify bounded queues and priority behavior;
- run 24-hour soak on ARM64 before the first stable release.

Tests must never rely on the live public ADSBDB service or the user’s LAN receiver.

---

## 20. CI and quality gates

Every pull request:

~~~sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
govulncheck ./...
go test -run=^$ -bench=. -benchmem ./internal/state ./internal/rules
docker buildx build --platform linux/amd64 --load .
docker compose -f deploy/compose.yaml -f deploy/compose.local.yaml config
~~~

CI should check formatting rather than mutate it. Add:

- golangci-lint only if its enabled rules are explicitly curated;
- dependency license reporting;
- secret scanning;
- Dockerfile and Compose linting;
- image vulnerability scanning;
- a container smoke test using a fake receiver;
- migration tests;
- generated-code drift checks if generation is introduced.

Block merge on:

- failing tests;
- race reports;
- invalid Compose configuration;
- high/critical exploitable vulnerabilities without a documented exception;
- benchmark regression above the agreed noise threshold on a controlled runner;
- accidental token or private receiver data.

Keep benchmark regression gating limited to stable dedicated runners; shared CI timing is too noisy for hard microbenchmark limits.

---

## 21. Codex implementation phases

Codex must complete phases in order. A phase is complete only when its definition of done passes.

### Phase 0 — repository and dependency spike

Tasks:

- inspect the existing repository, language, license, dirty worktree, and automation;
- preserve unrelated user changes;
- record Go and container host versions;
- verify disgo’s current stable API for Gateway, interactions, components, modals, autocomplete, and shutdown;
- compare internal ADSBDB client with go-adsbdb;
- benchmark or at least smoke-test modernc.org/sqlite on amd64 and ARM64;
- validate Docker reachability to the receiver using the planned LAN address;
- capture sanitized fixtures from aircraft, receiver, and stats payloads;
- write short architecture decision records.

Definition of done:

- dependency versions are pinned;
- receiver access works from a temporary container;
- no secrets or identifying raw data enter fixtures;
- source, storage, enrichment, and Discord interfaces are agreed;
- ADRs explain rejected alternatives.

### Phase 1 — clean skeleton and lifecycle

Tasks:

- create module/package layout;
- implement config parsing and validation;
- implement command tree and version/build metadata;
- add structured logger;
- add fixed-service lifecycle, signal handling, and graceful shutdown;
- add health server and built-in healthcheck;
- create Makefile and developer commands.

Definition of done:

- config check succeeds with valid local configuration;
- invalid configuration fails before Discord or receiver work starts;
- SIGTERM exits cleanly;
- unit tests, vet, staticcheck, and race test pass.

### Phase 2 — receiver ingestion and immutable state

Tasks:

- implement separate readsb DTOs;
- implement one reusable receiver HTTP client;
- add payload limits and timeouts;
- normalize all three payloads;
- compute distance/bearing;
- publish immutable snapshots;
- implement stale/degraded/offline state;
- add replay harness and benchmarks.

Definition of done:

- live and fixture sources produce the same domain shape;
- a malformed poll does not replace a good snapshot;
- no overlapping unbounded polls;
- 1,000-aircraft p99 targets pass;
- race detector passes.

### Phase 3 — Discord foundation and project UI

Tasks:

- create disgo Gateway client with minimal intents;
- register guild-scoped development commands idempotently;
- implement router, permission checks, deferral helper, error mapping, and outbound scheduler;
- implement /status, /nearby, /aircraft, and /help;
- implement embeds, autocomplete, buttons, select menus, modals, and session expiry;
- implement configurable live dashboard edit loop.

Definition of done:

- every interaction acknowledges within target in integration tests;
- all component types have a real user flow;
- UI consistently uses SkyFeed presentation rules;
- embed-limit tests pass;
- no privileged Message Content intent;
- dashboard work coalesces under a slow fake Discord transport.

### Phase 4 — rules, alerts, reports, and SQLite

Tasks:

- create migrations and repository interfaces;
- add settings, channel/role bindings, watch rules, alert state, and rollups;
- build indexed rule engine, cooldowns, hysteresis, and priority queues;
- implement /watch, /alerts, /reports, /feeder, and /settings;
- implement emergency and recovery paths;
- add report aggregation without raw snapshot retention.

Definition of done:

- restart does not produce an alert storm;
- emergency queue retains reserved capacity;
- normal rule evaluation passes target with the agreed rule set;
- migrations and backup/restore test pass;
- admin actions are authorization-tested.

### Phase 5 — ADSBDB enrichment

Tasks:

- implement the chosen Enricher adapter;
- add typed DTO mapping;
- add cache, singleflight, limiter, bounded workers, retry budget, and circuit breaker;
- enrich aircraft details and routes asynchronously;
- add attribution and unavailable/partial states;
- keep route persistence and export disabled;
- add synthetic contract fixtures.

Definition of done:

- ADSBDB cannot block or slow source publication;
- all error and partial-response cases are tested;
- cache hit path meets target;
- repeated identical lookups coalesce;
- terms/data-use note is visible in README and configuration;
- disabling ADSBDB removes all external lookups without breaking commands.

### Phase 6 — production local containers

Tasks:

- write multi-stage Dockerfile;
- add distroless non-root runtime;
- add Compose baseline and local/host overlays;
- add token-file workflow, volume, tmpfs, healthcheck, read-only filesystem, capability drop, logging rotation, and resource guidance;
- add amd64/arm64 Buildx configuration;
- test receiver reachability from the final image;
- document Raspberry Pi/ARM64 and Linux amd64 startup.

Definition of done:

- one documented command starts the local stack;
- container becomes healthy and Discord commands work;
- restart preserves database state;
- image contains no shell, compiler, source tree, or token;
- image runs as non-root;
- both architecture images build;
- receiver is not exposed by the deployment.

### Phase 7 — measured optimization

Tasks:

- run the full benchmark suite on target hosts;
- capture CPU, allocation, mutex, block, and trace evidence as needed;
- remove proven hot allocations/contention;
- tune HTTP transports and queues;
- verify GOMEMLIMIT and container limits;
- compare SQLite batch parameters;
- run 10x replay and 24-hour soak;
- evaluate PGO with benchstat.

Definition of done:

- all targets in Section 15 pass or an ADR documents an evidence-based revision;
- no goroutine, file-descriptor, queue, or memory leak;
- PGO is either included with evidence or explicitly rejected with evidence;
- the optimized code remains race-free and readable.

### Phase 8 — cloud readiness

Tasks:

- document private VPN deployment;
- implement skyfeed-agent only if cloud placement requires it;
- add mTLS identity, sequence/replay checks, bounded buffering, and cloud source adapter;
- add cloud secret-manager examples;
- add PostgreSQL/leadership only for multi-replica deployment;
- create cloud Compose/platform templates without provider lock-in.

Definition of done:

- cloud bot receives feeder data without public receiver exposure;
- loss/reconnect behavior is tested;
- exactly one leader sends alerts and owns the Gateway;
- secrets and persistent state survive restart through supported platform mechanisms.

### Phase 9 — release and operations

Tasks:

- finalize README quick starts;
- add upgrade, rollback, backup, restore, token rotation, and troubleshooting runbooks;
- add CI and signed multi-platform image release;
- generate SBOM/provenance;
- run security and disaster-recovery drills;
- tag v1.0 only after local soak.

Definition of done:

- a new operator can deploy from documentation;
- an upgrade and rollback are rehearsed;
- release image digest is recorded;
- all overall acceptance criteria pass.

---

## 22. Local operator workflow

The finished repository should support a workflow equivalent to:

~~~sh
cp .env.example .env
mkdir -p secrets
chmod 700 secrets
printf '%s' 'DISCORD_TOKEN_FROM_DEVELOPER_PORTAL' > secrets/discord_token
chmod 600 secrets/discord_token
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml config
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml up -d
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml ps
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.local.yaml logs -f skyfeed
~~~

Documentation must tell the operator to enter the token locally and never paste it into an issue, commit, image build argument, or support log.

Provide commands for:

- checking receiver reachability from the container;
- validating configuration without starting the Gateway;
- registering development commands;
- backing up SQLite safely;
- restoring a backup;
- viewing health and metrics locally;
- upgrading by immutable image tag;
- rolling back;
- rotating the Discord token;
- disabling ADSBDB immediately.

---

## 23. Overall definition of done

SkyFeed v1 is complete when:

- it runs locally with one Docker Compose command;
- it builds for amd64 and ARM64;
- it reads all three confirmed receiver URLs;
- it provides the complete Discord command and component UI;
- it uses disgo consistently;
- it enriches opportunistically through ADSBDB without touching the live hot path;
- it respects ADSBDB data-use restrictions and attribution;
- it persists settings and rules across restart;
- it detects and deduplicates emergencies and feeder outages;
- it is non-root, read-only, capability-free, and secret-file based;
- it requires no public inbound port for Discord;
- it has a safe private-network cloud path;
- it meets the performance targets on the intended host;
- it passes unit, integration, fuzz, race, replay, and soak tests;
- it has health, metrics, logs, backup, upgrade, and rollback procedures;
- no critical path depends on an unaudited unbounded queue or third-party lookup.

---

## 24. Guardrails for Codex

Codex implementing this plan must:

1. inspect before editing;
2. preserve unrelated user work and never use destructive Git commands;
3. make the smallest coherent changes per phase;
4. keep domain packages independent of Discord, SQLite, and ADSBDB DTOs;
5. use interfaces only at real boundaries, not around every function;
6. prefer standard library code until a dependency provides material value;
7. pin and review every direct dependency;
8. add tests with each behavior change;
9. run formatting, tests, race detection, vet, staticcheck, and relevant benchmarks;
10. validate Docker and Compose artifacts by actually building and starting them against fakes;
11. report commands run, results, measured performance, and remaining risks;
12. stop and ask before changing scope, exposing the receiver, adding a paid service, or altering external infrastructure;
13. never claim “highest performance” without reproducible measurements;
14. never make a live ADSBDB or Discord request from a unit test;
15. keep this plan’s checkboxes/status updated as implementation proceeds.

---

## 25. Codex master build prompt

Copy the following into Codex when the repository is ready:

~~~text
Build SkyFeed, the Dockerized, local-first and cloud-ready ADS-B Discord bot described in SKYFEED_DISCORD_DOCKER_BUILD_PLAN.md.

Treat that document as the implementation specification. Begin with Phase 0. Inspect the repository and preserve all unrelated existing changes. Do not jump directly to UI code. Confirm the actual receiver contract and Docker-network reachability first.

Use current stable Go, github.com/disgoorg/disgo, native Discord interactions, immutable in-memory snapshots, bounded queues, SQLite for the local single-node deployment, and asynchronous ADSBDB enrichment. Keep database, Discord, and ADSBDB work out of the one-second ingest hot path. Run as a non-root distroless multi-architecture container with a read-only root filesystem and a mounted token file.

Work phase by phase. For each phase:
1. state the files and decisions involved;
2. implement the smallest complete slice;
3. add or update tests;
4. run the required quality and performance checks;
5. fix failures;
6. summarize evidence and mark the definition of done;
7. stop for a materially ambiguous product or infrastructure decision.

Do not expose the receiver publicly. Local mode is the immediate priority. Preserve the Source boundary and deployment files needed for a later private-tunnel or outbound-agent cloud mode.

Do not optimize from intuition. Establish benchmarks, profile representative replay workloads, and retain changes only when evidence shows improvement without correctness, race, security, or maintainability regressions.
~~~

---

## 26. Primary references

- disgo repository: https://github.com/disgoorg/disgo
- Discord application commands: https://discord.com/developers/docs/interactions/application-commands
- Discord receiving and responding to interactions: https://discord.com/developers/docs/interactions/receiving-and-responding
- Discord Gateway: https://discord.com/developers/docs/events/gateway
- Discord rate limits: https://discord.com/developers/docs/topics/rate-limits
- ADSBDB website and API: https://www.adsbdb.com/
- ADSBDB repository and data notice: https://github.com/mrjackwills/adsbdb
- Go ADSBDB client considered by the spike: https://github.com/nint8835/go-adsbdb
- Docker multi-stage builds: https://docs.docker.com/build/building/multi-stage/
- Docker multi-platform builds: https://docs.docker.com/build/building/multi-platform/
- Docker Compose networking: https://docs.docker.com/compose/how-tos/networking/
- Docker host networking: https://docs.docker.com/engine/network/drivers/host/
- Docker Compose service reference: https://docs.docker.com/reference/compose-file/services/
- Go container-aware GOMAXPROCS: https://go.dev/blog/container-aware-gomaxprocs
- Go profile-guided optimization: https://go.dev/doc/pgo
- Go garbage collector guide: https://go.dev/doc/gc-guide
- Go release history: https://go.dev/doc/devel/release
- Go 1.27 release notes: https://go.dev/doc/go1.27

---

## 27. First implementation checkpoint

The first Codex checkpoint should return:

- repository inventory;
- current dirty-worktree report;
- dependency and license decision table;
- sanitized receiver fixture inventory;
- proof of receiver access from the intended container network;
- proposed ADRs;
- precise Phase 1 file list;
- any blocker requiring the operator’s decision.

It should not yet register production Discord commands, publish messages, expose network ports, or mutate cloud infrastructure.
