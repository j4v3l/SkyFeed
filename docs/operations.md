# SkyFeed operations

## Startup checks

1. Validate Compose rendering and `config check`.
2. Run `source check` from the final image. It must parse all three fixed JSON
   endpoints; HTTP 200 with an empty body is a failure.
3. Run `commands sync` in the development guild.
4. Start the service and require `/livez`, `/readyz`, and `/healthz` to pass.
5. Confirm `/metrics` has recent aircraft/receiver/stats success timestamps.

## Receiver failures

SkyFeed retains the last good snapshot, marks each source independently, and
does not replace state with empty, malformed, non-2xx, or oversized payloads.
An aircraft-source outage emits one high-priority alert; recovery requires two
healthy observations. `receiver.json` or `stats.json` failures do not stop the
aircraft pipeline.

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
- Discord disconnects leave ingest/rules active while bounded outbound queues
  coalesce dashboards and normal duplicate alerts.
- pprof is disabled unless `SKYFEED_PPROF_ADDR` is explicitly set to loopback.
- Metrics never label ICAO, callsign, guild, channel, or user IDs.

## Role and moderation recovery

A Discord Administrator can bootstrap or repair `/settings roles` bindings.
SkyFeed uses existing roles only; it never creates roles and does not need
Manage Roles. Keep the bot role above members it may moderate and grant the bot
only Moderate Members, Kick Members, and Ban Members in addition to its normal
message permissions.

Configure a `moderation` channel binding before enabling staff workflows.
Completed actions are not rolled back when log delivery fails: the durable
outbox retries across restarts with bounded backoff. Inspect `/moderation case`
or `/moderation history` privately to reconcile an action. Cases older than 365
days are deleted in bounded batches. A missing or inaccessible moderation-log
channel should be treated as an operational fault.

Before granting staff access, test a hierarchy rejection and a harmless warning
against a consenting test account. Never test kick or ban against an unrelated
user.

## Disaster recovery drill

Quarterly: create a backup, stop the service, restore into a disposable volume,
run migrations and repository tests, start against a fake receiver, verify
rules/cooldowns, then remove the disposable environment. Record elapsed time,
image digest, backup size, and any manual correction.
