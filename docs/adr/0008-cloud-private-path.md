# ADR 0008: Private tunnel is the initial cloud path

Status: accepted

No cloud platform or multi-replica requirement has been selected. Pattern A—a
private VPN/mesh path to the existing readsb HTTP adapter—changes the fewest
trusted components and preserves identical normalization behavior.

The outbound LAN agent remains a compiled, documented stub. PostgreSQL,
leadership leasing, and an active agent transport are rejected for the initial
single-feeder deployment because they add operational and security state
without a current requirement. The receiver must never be publicly exposed.
