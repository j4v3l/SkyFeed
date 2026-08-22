# Performance evidence

- Host: Apple M3 Pro, darwin/arm64, Go 1.27.0
- Date: 2026-08-22
- Samples: five Go benchmark runs unless noted

| Benchmark | Observed range | Allocations |
|---|---:|---:|
| Decode synthetic aircraft JSON | 4.994–5.029 µs/op | 2,796 B, 23 allocs |
| Normalize/publish 1,000 aircraft | 158.5–171.6 µs/op | 426,106–426,109 B, 2,044 allocs |
| Distance/bearing | 68.23–68.59 ns/op | 0 B, 0 allocs |
| Evaluate 5,000 exact rules over 1,000 aircraft | 226.8–232.3 µs/op | 18,118–18,127 B, 1,002 allocs |
| Combined replay pipeline | 401.6–409.5 µs/op | 442,498–442,532 B, 3,040 allocs |
| Render aircraft card | 837.4–844.1 ns/op | 872 B, 23 allocs |
| ADSBDB cache hit | 89.56–95.05 ns/op | 0 B, 0 allocs |
| SQLite rollup write | 19.09–19.38 µs/op | 416 B, 8 allocs |

The measured means have substantial margin against the 25 ms snapshot and
10 ms rule targets, but they are not presented as p99 evidence for the intended
Raspberry Pi-class host. That release claim requires the target-host run.

The 10× HTTP fixture replay published 100 snapshots in 9.938 seconds without
queue accumulation. A 30-second, 10×, 1,000-aircraft soak completed 300
iterations with goroutines 1→1, file descriptors 5→5, and live heap
3,012,136→223,856 bytes after GC. This is a smoke soak, not a substitute for
the required 24-hour ARM64 run.

PGO remains excluded under [ADR 0007](../adr/0007-pgo-deferred.md): no
representative intended-host CPU profile exists yet.
