# Performance evidence

- Host: Apple M3 Pro, darwin/arm64, Go 1.27.0
- Date: 2026-08-25
- Samples: five Go benchmark runs unless noted; longer one-second samples were
  used for the state pipeline and seven samples for the mixed-rule/render paths

| Benchmark | Observed range | Allocations |
|---|---:|---:|
| Decode synthetic aircraft JSON | 5.239–5.399 µs/op | 2,795–2,796 B, 23 allocs |
| Normalize/publish 1,000 aircraft | 116.6–123.5 µs/op | 410,609–410,610 B, 22 allocs |
| Metadata-only immutable snapshot reuse | 142.5–165.5 ns/op | 768 B, 1 alloc |
| Distance/bearing | 70.92–72.62 ns/op | 0 B, 0 allocs |
| Evaluate 5,000 exact rules over 1,000 aircraft | 213.2–233.8 µs/op | 3,990–4,067 B, 6 allocs |
| Evaluate representative 5,000-rule mixed set over 1,000 aircraft | 7.947–8.459 ms/op | 2,048 B, 2 allocs |
| Evaluate 5,000 best-effort enrichment rules | 216.8–221.8 µs/op | 496 B, 2 allocs |
| Unique-ICAO churn (100 new aircraft/batch) | 49.00–50.31 µs/op | 90,872–90,927 B, 158–159 allocs |
| Combined replay pipeline | 350.0–453.6 µs/op (median 368.6 µs) | 410,879–410,915 B, 18 allocs |
| Render layered aircraft card | 1.547–1.813 µs/op | 1,088 B, 51 allocs |
| ADSBDB cache hit | 67.33–69.31 ns/op | 0 B, 0 allocs |
| SQLite rollup upsert | 22.60–24.41 µs/op | 448–449 B, 8 allocs |

The measured means have substantial margin against the 25 ms snapshot and
10 ms rule targets, including the representative mixed rule set after compiled
callsign-prefix lookup replaced the linear scan. They are not presented as p99
evidence for the intended Raspberry Pi-class host. That release claim requires
the target-host run.

The 10× HTTP fixture replay published 100 snapshots in 9.942 seconds and the
50× replay published 100 in 2.110 seconds without queue accumulation. A
30-second, 10×, 1,000-aircraft soak completed 300 iterations with goroutines
1→1, file descriptors 5→5, and live heap 2,130,952→248,688 bytes after GC.
This is a smoke soak, not a substitute for the required 24-hour ARM64 run.

The wider, semantically correct report row makes one isolated SQLite upsert
slower than the earlier narrow row. This is accepted in [ADR 0009](../adr/0009-provenance-and-rollup-batching.md):
15-second coalescing reduces expected report writes by about 93%, so aggregate
database work is lower while provenance and emergency-transition semantics are
correct.

PGO remains excluded under [ADR 0007](../adr/0007-pgo-deferred.md): no
representative intended-host CPU profile exists yet.
