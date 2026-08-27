# ADR 0008: Private tunnel is the initial cloud path

Status: superseded for community feeders by ADR 0010; private tunnels remain supported

No cloud platform or multi-replica requirement has been selected. Pattern A—a
private VPN/mesh path to the existing readsb HTTP adapter—changes the fewest
trusted components and preserves identical normalization behavior.

The initial release deliberately deferred the outbound LAN agent. ADR 0010
records the later community-feeder requirement and the implemented signed,
outbound-only agent transport. PostgreSQL and leadership leasing remain
rejected while exactly one active application process owns the Gateway and
SQLite database. The receiver must never be publicly exposed.
