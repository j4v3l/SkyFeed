# Implementation checkpoint

- Updated: 2026-08-27
- Scope: Trust/stability, multi-feeder, weather, Nix/NixOS, presentation,
  information, and performance roadmap

SkyFeed's complete local-first code path is implemented. No production Discord
request, public receiver exposure, application release, or v1.0 tag was
performed. The required Terms and Privacy pages were published separately with
no cookies, analytics, authentication, or private application data.

## Implemented evidence

| Area | Evidence |
|---|---|
| Lifecycle and config | Fail-fast validated environment/token-file config, command tree, structured logs, fixed-service cancellation, healthcheck, and SIGTERM test |
| Receiver and state | Three fixed bounded endpoints, last-good retention, independent health, timestamp sanity, immutable atomic snapshots, metadata-only index reuse, direct search comparison, 10×/50× replay, and malformed-input tests |
| Discord | disgo-only Gateway/REST adapter, schema v15 with 22 slash commands plus context lookup, `/feeders` administration, feeder selectors, `/top live`/`traffic`, personal/guild units, layered cards, local track plots, composite provenance, permission-aware help, bounded sessions, and isolated outbound priority lanes |
| Rules and storage | Central emergency semantics, feeder-scoped indexed rules, bounded churn and movement registries, three-sample inferred movement, restart dedupe, WAL SQLite, eleven forward migrations, provenance-gated routes, coalesced/retried rollups, scoped reports, schedules, and backup/restore |
| Access and moderation | Existing-role Operator/Moderator/Admin bindings, native permission plus hierarchy enforcement, private confirmation, warning DMs, durable cases, bounded retry outbox, and 365-day retention |
| ADSBDB and providers | Typed provenance, bounded TTL/LRU caches, redirect/body/timeout/malformed-response controls, retry-after, limiter, singleflight, worker bounds, circuit breaker, fair route prefetch, weather stale/negative caching, and bounded Plane Alert streaming |
| Information integrity | Report observations/peak/emergency events have distinct semantics; only adsb.lol routes may enter durable rankings; migration 010 purges older derived route rows and collapses legacy emergency fingerprints without reinterpreting historical observations |
| Weather and movement | AviationWeather typed fields are authoritative with raw fallback, exact/alias/nearby reporting-station resolution, bounded stale/negative caches, cancellation-isolated coalescing, per-feeder weather overrides, and separately bounded movement inference |
| Multi-feeder | One direct `local` source plus up to 100 invited outbound agents; Ed25519 identity, zstd payloads, replay persistence, timestamp/body/aircraft limits, four-worker ingress, latest-value agent buffering, privacy stripping, and aggregate ICAO deduplication are implemented |
| Memory bounds | Rule and enrichment state is pruned; recent aggregate tracks are memory-only for 15 minutes at 180 points each and 5,000 aircraft; movement is capped at 1,000 entries per feeder and 25,000 globally; plot/weather/enrichment/session state is bounded |
| Operations | Low-cardinality histograms distinguish local interaction handling from Discord acknowledgement latency; loopback-only pprof and agent ingress, private tunnel/reverse-proxy docs, CI/release automation, SBOM/provenance/signing/scanning |
| Containers and Nix | Hardened distroless image includes `skyfeed` and `skyfeed-agent`; Compose keeps ingress disabled by default. The Nix package builds and tests both binaries, exposes both apps, and hardened NixOS bot/agent modules use systemd credentials and typed firewall options |
| Legal verification | Public Terms and Privacy routes return HTTP 200 over HTTPS; policy source remains version controlled in `docs/legal` |

Previous loaded-image evidence remains below. After migration 011, the current
source built successfully through both final distroless stages for linux/amd64
and linux/arm64. The locally loaded arm64 image
`sha256:214992ccd5fd676a75ccd6376ce91474aa24ba0ebcb6ac245d5424f74cd68c7f`
runs both binaries, reached the real three-endpoint feeder, and became healthy.

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
- Baseline, central-ingress, and outbound-agent Compose configurations render;
  community ingress remains disabled until an administrator creates an invite.
- `nix flake check path:. --no-build --all-systems` and the native Nix package
  build with tests pass. The x86_64 NixOS VM derivation evaluates on Apple
  Silicon but cannot execute locally without an x86_64 Linux builder.
- Current ARM64 and AMD64 images run as non-root; the ARM64 final image passed
  the fixed three-endpoint receiver contract against an isolated fake.
- See [performance.md](performance.md) for refreshed benchmark and soak values.

## Open release gates

1. Run the full real-guild moderation and first community-feeder invitation
   acceptance flow. Local Gateway readiness and schema-v15 registration are
   healthy, but community ingress deliberately remains disabled.
2. Run the required 24-hour soak and 15-minute CPU/RSS measurement on the
   intended ARM64 host. Revisit PGO only from that representative profile.
3. Execute the x86_64 NixOS VM service test on an x86_64 Linux builder. Both
   final Docker architectures compile locally, but a release image is not yet
   published or signed.
4. Exercise the selected private mesh or authenticated HTTPS reverse proxy
   before enabling agent ingress. PostgreSQL/Redis/leadership remain
   intentionally absent for the single active process.
5. Rehearse live upgrade, rollback, backup/restore, token rotation, and disaster
   recovery before creating a signed v1.0 release.
