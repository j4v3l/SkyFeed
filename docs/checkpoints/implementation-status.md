# Implementation checkpoint

- Updated: 2026-08-22
- Scope: Phases 1–9 after the Phase 0 development exception

SkyFeed's complete local-first code path is implemented. No production Discord
request, public receiver exposure, application release, or v1.0 tag was
performed. The required Terms and Privacy pages were published separately with
no cookies, analytics, authentication, or private application data.

## Implemented evidence

| Area | Evidence |
|---|---|
| Lifecycle and config | Fail-fast validated environment/token-file config, command tree, structured logs, fixed-service cancellation, healthcheck, and SIGTERM test |
| Receiver and state | Three fixed bounded endpoints, last-good retention, independent health, timestamp sanity, immutable atomic snapshots, replay and malformed-input tests |
| Discord | disgo-only Gateway/REST adapter, guild/global idempotent registration, ten command schemas, autocomplete, buttons, select, modal, deferral, bound sessions, dashboard, alert/test/report delivery |
| Rules and storage | Indexed live and best-effort enrichment rules, emergency/feeder dedupe across restart, bounded priority queues, WAL SQLite, five migrations, async batches, reports, schedules, backup/restore |
| Access and moderation | Existing-role Operator/Moderator/Admin bindings, native permission plus hierarchy enforcement, private confirmation, warning DMs, durable cases, bounded retry outbox, and 365-day retention |
| ADSBDB | Internal typed client, body/timeout bounds, retry-after, limiter, singleflight, configurable TTLs, stale cache, worker bounds, circuit breaker, route switch, attribution |
| Operations | Low-cardinality metrics, loopback-only optional pprof, private tunnel docs, agent replay/identity validation, CI/release automation, SBOM/provenance/signing/scanning |
| Containers | Final distroless images passed `version` and synthetic `source check` on linux/arm64 and linux/amd64; user is `nonroot:nonroot`; `/bin/sh` is absent |
| Legal verification | Public Terms and Privacy routes return HTTP 200 over HTTPS; policy source remains version controlled in `docs/legal` |

Final locally loaded image evidence after the last source change:

| Platform | Image ID | Size |
|---|---|---:|
| linux/arm64 | `sha256:2508a89e974c3ab28efb87175398a819585233a1f829f8ed78374cd5ee8a7e01` | 17,121,882 bytes |
| linux/amd64 | `sha256:70891d940dd16d559741e84c1694900c6029b0b31e4d417cec660d8a7bbca1ad` | 17,982,042 bytes |

## Verification summary

- `go test ./...`, `go test -race ./...`, `go vet ./...`, Staticcheck,
  govulncheck, and dependency-license policy: passed.
- Five fuzz targets passed; component-ID fuzzing discovered and retained a
  whitespace-parser regression case.
- Local, host-network, and cloud Compose overlays render successfully.
- See [performance.md](performance.md) for measured benchmark and soak values.

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
