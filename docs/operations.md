# SkyFeed operations

## Startup checks

1. Validate Compose rendering and `config check`.
2. Run `source check` from the final image. It must parse all three fixed JSON
   endpoints; HTTP 200 with an empty body is a failure.
3. Run `commands sync` in the development guild.
4. Start the service and require `/livez`, `/readyz`, and `/healthz` to pass.
5. Confirm `/metrics` has recent aircraft/receiver/stats success timestamps and
   that `skyfeed_aircraft_provider_active` reflects the expected provider.
6. When airplanes.live fallback is configured, confirm health JSON `privacy`
   shows the public airport code and radius only—never coordinates.
7. When adsb.lol enrichment is enabled, confirm the `adsblol` component is
   `healthy` and `skyfeed_adsblol_requests_total` advances during live traffic.

## Receiver and aircraft-source failures

SkyFeed retains the last good snapshot, marks each source independently, and
does not replace state with empty, malformed, non-2xx, or oversized payloads.
An aircraft-source outage emits one high-priority alert; recovery requires two
healthy observations. `receiver.json` or `stats.json` failures do not stop the
aircraft pipeline.

When `airplanes-live` is configured after `readsb`, aircraft failover is
ordered and anti-flapping: readsb remains primary for receiver metadata and
statistics, while airplanes.live supplies aircraft snapshots only when readsb
aircraft polling fails. Fallback success is healthy; provider changes do not
create false message deltas. airplanes.live enforces one request per second and
rejects radii outside 1–250 NM. Treat HTTP 429 responses as operational signals
rather than ingest faults.

Route and airport enrichment failures open a circuit, serve cached values when
available, and never block ingest, emergency alerts, or snapshot publication.
Disable adsb.lol immediately with `SKYFEED_ADSBLOL_ENABLED=false`.

## Health and metrics

Health JSON is returned by `/livez`, `/readyz`, and `/healthz`. Besides aggregate
status, readiness, uptime, and component messages, each response embeds the same
`privacy` object used by `/privacy` (providers, public airport code, radius,
retention, attribution). Coordinates, receiver URLs, and guild identifiers are
never included.

Component keys to monitor:

| Component | Healthy when |
| --- | --- |
| `aircraft_source` | Active aircraft provider (`readsb` or `airplanes-live`) is healthy or degraded/stale within policy |
| `receiver_source` | Local receiver metadata is healthy, or disabled when unsupported |
| `stats_source` | Local statistics are healthy, or disabled when unsupported |
| `adsbdb` | `healthy` when opt-in enrichment is enabled, otherwise `disabled` |
| `adsblol` | `healthy` when route enrichment is enabled, otherwise `disabled` |

Prometheus metrics at `/metrics` intentionally use fixed labels only. Provider
observability series:

- `skyfeed_aircraft_provider_active{provider="readsb|airplanes-live"}`
- `skyfeed_source_capability_supported`, `skyfeed_source_health`,
  `skyfeed_source_requests_total`, `skyfeed_source_errors_total`,
  `skyfeed_source_payload_bytes_total`, `skyfeed_source_request_duration_seconds`,
  and `skyfeed_source_last_success_timestamp_seconds` with
  `{provider,capability}` where `capability` is `aircraft`, `receiver`, or
  `statistics`.
- `skyfeed_adsbdb_*` when ADSBDB enrichment is enabled.
- `skyfeed_adsblol_cache_total{result="hit|miss"}`,
  `skyfeed_adsblol_requests_total`, `skyfeed_adsblol_failures_total`,
  `skyfeed_adsblol_circuit_rejects_total`, `skyfeed_adsblol_queue_drops_total`,
  `skyfeed_adsblol_batches_total`, and
  `skyfeed_adsblol_cache_entries{kind="route|airport"}` when adsb.lol enrichment
  is enabled.

Never expect ICAO, callsign, guild, channel, user, airport-code, or coordinate
labels in metrics output.

## Database

SQLite uses WAL, foreign keys, a busy timeout, forward-only migrations, and one
bounded batch writer. A full writer queue or failed durable batch is an
operational fault. Use the binary's `backup` command while running. Stop the
application before `restore`; the old database and any WAL/SHM sidecars are
preserved under the same timestamped rollback prefix.

## Upgrade and rollback

Back up SQLite, record the current immutable image tag/digest, pull the target
tag, recreate the service, and confirm health plus command schema. Roll back by
restoring the prior immutable image. Restore the database only if the forward
migration is incompatible with the prior binary; migrations themselves never
run backward.

## Fault isolation

- ADSBDB failures open a circuit and never block ingest or emergency state.
- adsb.lol route/airport failures open a separate circuit and never block ingest
  or emergency state.
- airplanes.live fallback is cancellation-safe and never logs URLs or center
  coordinates on errors.
- Discord disconnects leave ingest/rules active while bounded outbound queues
  coalesce dashboards and normal duplicate alerts.
- pprof is disabled unless `SKYFEED_PPROF_ADDR` is explicitly set to loopback.
- Metrics never label ICAO, callsign, guild, channel, user IDs, airport codes,
  or coordinates.

## Role and moderation recovery

A Discord Administrator can bootstrap or repair `/settings roles` bindings.
SkyFeed uses existing roles only; it never creates roles and does not need
Manage Roles on the bot. Keep the bot role above members it may moderate and grant the bot
only Moderate Members, Kick Members, and Ban Members in addition to its normal
message permissions.

On startup, SkyFeed can auto-bind access tiers when these env vars are set:
`SKYFEED_DISCORD_ADMIN_ROLE_ID`, `SKYFEED_DISCORD_OPERATOR_ROLE_ID`, and
`SKYFEED_DISCORD_MODERATOR_ROLE_ID`. The `@SkyFeed Admin` Discord role should
include **Manage Roles** so admins can assign Operator and Moderator roles to
members. Run `python3 scripts/setup-discord-governance.py` to apply channel
permission overwrites and post server rules to `#server-guide`.

### Channel access by role

| Channel group | Who can view | Who can post |
| --- | --- | --- |
| Bot feeds (`#live-radar`, `#flight-alerts`, `#emergency-squawks`, `#flight-reports`, `#interesting-aircraft`) | Everyone | SkyFeed bot only |
| Staff (`#operations-log`, `#moderation-log`) | Admin, Operator, Moderator | Staff roles + bot |
| Rules (`#welcome`, `#server-guide`) | Everyone | Admin + bot |
| `#announcements` | Everyone | Admin + bot |
| Community (chat, spotting, `#bot-commands`) | Everyone | Everyone |

Viewer slash commands work in any channel without Send Messages permission.

Configure a `moderation` channel binding before enabling staff workflows.
Completed actions are not rolled back when log delivery fails: the durable
outbox retries across restarts with bounded backoff. Inspect `/moderation case`
or `/moderation history` privately to reconcile an action. Cases older than 365
days are deleted in bounded batches. A missing or inaccessible moderation-log
channel should be treated as an operational fault.

## Interesting aircraft channel

SkyFeed posts first sightings of plane-alert-db matches (Mil, Gov, Pol, Civ) to
the `interesting` channel binding. Matching is **feeder-only**: aircraft must
come from the local `readsb` provider; airplanes.live fallback sightings never
trigger interesting alerts.

Configure the binding before expecting delivery:

```text
/settings channels purpose:Interesting aircraft channel:#interesting-aircraft
/settings test purpose:Interesting aircraft
```

Verify health component `planealert` is `healthy` and logs show
`plane-alert-db reference updated` with a non-zero record count. Confirm
`interesting_aircraft_seen` grows only on first ICAO sightings (restarts restore
the seen set from SQLite).

Use `/alerts configure category:Interesting aircraft` to disable delivery or
override cooldown/destination. Pause/resume via `/settings pause-alerts` and
`/settings resume-alerts` applies to interesting alerts as well.

Before granting staff access, test a hierarchy rejection and a harmless warning
against a consenting test account. Never test kick or ban against an unrelated
user.

## Disaster recovery drill

Quarterly: create a backup, stop the service, restore into a disposable volume,
run migrations and repository tests, start against a fake receiver, verify
rules/cooldowns, then remove the disposable environment. Record elapsed time,
image digest, backup size, and any manual correction.
