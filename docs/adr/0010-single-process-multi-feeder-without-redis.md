# ADR 0010: Single-process multi-feeder state without Redis

Status: accepted

SkyFeed supports one local receiver and up to 100 invited outbound LAN agents,
while retaining exactly one active Discord Gateway and alert leader. SQLite WAL
is authoritative for feeder registration, one-time enrollment hashes, public
metadata, agent keys, replay sequences, rules, and report rollups. Per-feeder
snapshots, aggregate indexes, tracks, movement state, weather caches, and queues
are bounded and disposable in-process data.

Redis would add another failure mode, secret, network dependency, and source of
consistency ambiguity without improving this single-active-process deployment.
It is therefore rejected for the local and initial community architecture.
Reconsider it only alongside multiple active application replicas, a shared
leadership design, and a demonstrated need for cross-process ephemeral state.

Community receivers remain private. Each `skyfeed-agent` polls its own LAN and
sends only normalized, signed snapshots outbound over private HTTPS; the central
service never accepts arbitrary receiver URLs or acts as a LAN proxy.
