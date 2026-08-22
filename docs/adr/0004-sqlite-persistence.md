# 0004: modernc SQLite persistence

- Status: Accepted
- Date: 2026-08-22

## Decision

Use modernc.org/sqlite v1.57.0 with its required modernc.org/libc v1.74.4.
Configure WAL, foreign keys, a busy timeout, explicit transactions, embedded
forward-only migrations, and one controlled writer path.

Expose feature-specific repositories for settings, watch rules, alert state,
events, reports, and message bindings rather than SQL or one unbounded generic
repository. Persistence is never called by the one-second ingest path.

## Consequences

The binary remains CGO-free and supports Linux amd64 and arm64. The driver is
retained only if cross-architecture smoke tests and later benchmarks pass.
CGO-backed SQLite and initial PostgreSQL deployment were rejected for the local
single-node edition.
