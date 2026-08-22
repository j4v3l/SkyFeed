# 0006: Private receiver networking and cloud boundary

- Status: Accepted
- Date: 2026-08-22

## Decision

Use normal Docker bridge networking and a configured LAN IP or proven LAN DNS
name for local deployment. Join only the fixed `aircraft.json`, `receiver.json`,
and `stats.json` paths under the validated `/data` base URL.

Do not expose the unauthenticated receiver publicly. A future cloud deployment
uses either a private tunnel or an outbound, authenticated LAN agent. Exactly
one active leader owns the Discord Gateway and alert engine.

## Consequences

`.local` resolution is never assumed even when it happens to work on one Docker
host. Receiver addresses remain local configuration and are redacted from
committed evidence. Host networking is an opt-in reachability workaround, not a
performance default.
