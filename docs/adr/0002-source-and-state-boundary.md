# 0002: Source normalization and immutable state boundary

- Status: Accepted
- Date: 2026-08-22

## Decision

Source adapters return normalized domain frames, not readsb or transport DTOs.
The source boundary exposes independent aircraft, receiver, and statistics
fetch operations because they have separate cadences and health states. Each
result carries the source generation time and local fetch time.

The state engine owns scheduling, freshness, last-known-good retention,
derived values, and complete snapshot construction. It publishes one immutable
snapshot through `atomic.Pointer`; readers load once and never mutate it.

The future LAN agent implements the same normalized source contract. It cannot
act as a general-purpose proxy.

## Consequences

Invalid or oversized input cannot replace a good snapshot. Source DTO changes
remain isolated. Database, Discord, enrichment, presentation, and per-aircraft
goroutines are excluded from the ingest hot path.
