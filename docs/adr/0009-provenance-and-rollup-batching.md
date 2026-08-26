# ADR 0009: Provenance-gated analytics and coalesced report rollups

- Status: Accepted
- Date: 2026-08-25

## Context

ADSBDB route responses are presentation-only and cannot be copied into a
durable local route database. Report rollups also used ambiguous counters and
wrote once per aircraft snapshot. Correct emergency-event reporting requires a
separate transition counter while preserving the legacy observation count.

Adding the explicit report columns raises the isolated SQLite upsert from the
previous roughly 19–22 microseconds to 22.6–24.4 microseconds on the Apple M3
Pro benchmark host. That individual-operation difference exceeds the normal
10% benchmark noise allowance at the edge of the observed ranges.

## Decision

Every durable route ranking row must carry typed `adsb-lol` provenance.
Migration 010 purges the derived route catalog and sightings before rebuilding
them from proven adsb.lol data. ADSBDB route values remain transient.

Reports store aircraft observations, peak tracked aircraft, legacy emergency
observations, and new emergency transition events as separate values. The
writer coalesces rollups in memory and flushes every 15 seconds, at hour
rollover, and during shutdown. Transient SQLite busy failures retain and retry
the same batch without stopping live ingest.

## Consequences

The small per-upsert regression is accepted for accurate, upgrade-safe report
semantics. Coalescing reduces the expected daily report-upsert count from about
86,400 to about 5,760 (approximately 93% fewer writes), so aggregate database
work is substantially lower despite the wider row. A crash can lose at most
the unflushed 15-second report window. Raw snapshots and track points remain
memory-only.
