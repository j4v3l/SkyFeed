# ADR 0007: Defer PGO until a representative production-like profile exists

Status: accepted

Synthetic decode, 1,000-aircraft snapshot, 5,000-rule, render, cache, SQLite,
and replay benchmarks establish the current baseline. They do not reproduce a
real ARM64 feeder's command mix, Gateway work, or day-long alert distribution.
Using one synthetic benchmark as `default.pgo` could overfit the binary and
would not satisfy the specification's representative-profile requirement.

SkyFeed therefore ships without PGO initially. Capture a sanitized CPU profile
from the replay harness or a controlled ARM64 deployment after live receiver
validation, compare at least five before/after benchmark runs with benchstat,
and retain `default.pgo` only when the mixed workload improves without a
regression. This decision is revisited after the required 24-hour ARM64 soak.
