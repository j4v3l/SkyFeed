# Implementation checkpoint

- Updated: 2026-08-25
- Scope: Trust/stability, presentation, information, and performance roadmap

SkyFeed's complete local-first code path is implemented. No production Discord
request, public receiver exposure, application release, or v1.0 tag was
performed. The required Terms and Privacy pages were published separately with
no cookies, analytics, authentication, or private application data.

## Implemented evidence

| Area | Evidence |
|---|---|
| Lifecycle and config | Fail-fast validated environment/token-file config, command tree, structured logs, fixed-service cancellation, healthcheck, and SIGTERM test |
| Receiver and state | Three fixed bounded endpoints, last-good retention, independent health, timestamp sanity, immutable atomic snapshots, metadata-only index reuse, direct search comparison, 10×/50× replay, and malformed-input tests |
| Discord | disgo-only Gateway/REST adapter, schema v14 with twenty slash commands plus context lookup, `/top live`/`traffic`, personal/guild units, layered cards, local track plots, composite provenance, permission-aware help, bounded sessions, and isolated outbound priority lanes |
| Rules and storage | Central emergency semantics, indexed prefix/exact/geometric/best-effort rules, bounded churn state, three-sample inferred movement, restart dedupe, WAL SQLite, ten migrations, provenance-gated routes, coalesced/retried rollups, reports, schedules, and backup/restore |
| Access and moderation | Existing-role Operator/Moderator/Admin bindings, native permission plus hierarchy enforcement, private confirmation, warning DMs, durable cases, bounded retry outbox, and 365-day retention |
| ADSBDB and providers | Typed provenance, bounded TTL/LRU caches, redirect/body/timeout/malformed-response controls, retry-after, limiter, singleflight, worker bounds, circuit breaker, fair route prefetch, weather stale/negative caching, and bounded Plane Alert streaming |
| Information integrity | Report observations/peak/emergency events have distinct semantics; only adsb.lol routes may enter durable rankings; migration 010 purges older derived route rows and collapses legacy emergency fingerprints without reinterpreting historical observations |
| Memory bounds | Rule and enrichment state is pruned; recent tracks are memory-only for 15 minutes at 180 points each and 2,000 aircraft; plot cache is capped; weather/enrichment/session queues and caches expose fixed limits |
| Operations | Low-cardinality metrics, loopback-only optional pprof, private tunnel docs, agent replay/identity validation, CI/release automation, SBOM/provenance/signing/scanning |
| Containers | Final distroless images passed `version` and synthetic `source check` on linux/arm64 and linux/amd64; user is `nonroot:nonroot`; `/bin/sh` is absent |
| Legal verification | Public Terms and Privacy routes return HTTP 200 over HTTPS; policy source remains version controlled in `docs/legal` |

Final locally loaded image evidence after the roadmap source changes:

| Platform | Image ID | Size |
|---|---|---:|
| linux/arm64 | `sha256:df0627c4db6e0ab62dc9646cc841d57699b32fa0d7dc0266be8fb7bfafe5064a` | 17,973,850 bytes |
| linux/amd64 | `sha256:6076777ad8c68d9b30c30047df8bf513f44ba3a8e0b2a0221147735c082987c5` | 19,010,138 bytes |

## Verification summary

- `go test ./...`, `go test -race ./...`, `go vet ./...`, Go-1.27-pinned
  Staticcheck, govulncheck, formatting, diff checks, and dependency-license
  policy: passed.
- Adapter redirect, oversize, malformed, additive-field, timeout, negative
  cache, stale-cache, and in-flight coalescing tests pass without live calls.
- Plane Alert statement coverage is 36.8% (up from the audited 18.7%); the
  changed Discord render package is 62.0%.
- Sanitized receiver replay passed at 10× and 50×. The 30-second 10× leak smoke
  held goroutines 1→1 and file descriptors 5→5 with heap falling after GC.
- Five fuzz targets passed; component-ID fuzzing discovered and retained a
  whitespace-parser regression case.
- Local, host-network, and cloud Compose overlays render successfully.
- Current ARM64 and AMD64 images run as non-root; the ARM64 final image passed
  the fixed three-endpoint receiver contract against an isolated fake.
- See [performance.md](performance.md) for refreshed benchmark and soak values.

## Open release gates

1. Run a real development-guild Discord smoke test—including role bootstrap,
   hierarchy failures, warning DM, timeout, kick/ban confirmation, unban, and
   moderation-log recovery—with the operator's local token.
2. Run the required 24-hour soak and 15-minute CPU/RSS measurement on the
   intended ARM64 host. Revisit PGO only from that representative profile.
3. Exercise the selected private VPN path before a cloud deployment. The LAN
   agent and shared PostgreSQL/leader lease remain conditional, not missing
   local-v1 dependencies.
4. Rehearse live upgrade, rollback, backup/restore, token rotation, and disaster
   recovery before creating a signed v1.0 release.
