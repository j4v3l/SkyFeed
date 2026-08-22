# SkyFeed Privacy Policy

Effective date: August 22, 2026

SkyFeed is an independently operated Discord bot that presents aircraft data
from an operator-controlled ADS-B receiver. This policy explains what SkyFeed
processes, why it is processed, how long it is retained, and the choices
available to Discord users and server operators.

## Information SkyFeed processes

Depending on enabled features, SkyFeed may process:

- Discord server, channel, role, message, and user IDs needed for configuration,
  permissions, interaction sessions, delivery, and moderation cases;
- user preferences and personal or server aircraft watch rules;
- moderation case details, including moderator and target user IDs, action,
  reason, timing, result, and warning-DM delivery status;
- operational events and low-cardinality service logs needed for security,
  reliability, and troubleshooting;
- live ADS-B aircraft observations supplied by the configured receiver; and
- transient aircraft and callsign lookup values sent to ADSBDB when enrichment
  is explicitly enabled.

SkyFeed does not request Discord's Message Content intent. It does not sell
personal information, use information for advertising, or create a durable
route database. Route enrichment is disabled by default.

## How information is used

Information is used only to operate requested bot features, enforce configured
access controls, deliver alerts and reports, maintain moderation audit records,
protect the service, diagnose failures, and comply with applicable obligations.

## Storage and retention

The local deployment stores configuration, role and channel bindings, watch
rules, alert state, report rollups, message bindings, and moderation cases in
the server operator's SQLite database. Raw one-second aircraft snapshots are
not stored by default.

Moderation cases are retained for 365 days so authorized server staff can
review actions and resolve disputes. They are then removed in bounded purge
batches. Disposable interaction sessions expire automatically. Operational
logs and backups should be retained only as long as the server operator needs
them. Transient ADSBDB route data is not durably stored by default.

## Sharing and service providers

SkyFeed sends interaction responses and configured messages to Discord. When
ADSBDB enrichment is enabled, normalized aircraft ICAO and callsign values may
be sent to ADSBDB. A hosting, VPN, registry, or secret-management provider may
process limited operational data when chosen by the server operator. SkyFeed
does not otherwise disclose data except when required by law, to protect users
or the service, or with the operator's direction.

Discord and ADSBDB operate under their own terms and privacy practices.

## Security

SkyFeed is designed for least-privilege Discord access, a non-root read-only
container, secret-file token handling, private receiver networking, bounded
queues, and redacted low-cardinality telemetry. No security measure can
guarantee absolute protection. Server operators are responsible for securing
their host, database, backups, Discord roles, and bot token.

## Access, correction, and deletion

Users may ask the server operator to review or delete preferences and watch
rules associated with their Discord user ID. Requests involving moderation
records may be denied or delayed during the 365-day audit-retention period when
the record is needed for safety, abuse prevention, dispute resolution, or legal
compliance.

For privacy or deletion requests, email `jj4v3l@gmail.com`. Include the Discord
server and user IDs needed to locate the record, but never send a Discord token,
password, private receiver URL, or other secret.

## Children and international use

SkyFeed is not directed to children below the minimum age required by Discord
or applicable law. Server operators are responsible for using SkyFeed only
where permitted and for providing any notices or obtaining any consent their
deployment requires.

## Changes

This policy may be updated as SkyFeed's features or legal obligations change.
The effective date above will be revised for material updates.

## Contact

SkyFeed privacy and support: `jj4v3l@gmail.com`
