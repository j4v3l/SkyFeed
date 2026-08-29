# Changelog

Notable user-facing changes are recorded here. SkyFeed follows semantic
versioning after the first public preview.

## Unreleased

### Changed

- Added secure Admin-only message deletion by message link, ID, or Discord
  context action, with native Manage Messages enforcement, private confirmation,
  durable moderation audit records, and no stored message content.
- Added one persistent live flight-leaders card in the configured reports
  channel, refreshed in place with fastest, slowest, highest, and lowest fresh
  airborne aircraft from the deduplicated all-feeder view.

- Reduced community aggregate allocation volume while preserving immutable
  snapshots and deterministic feeder attribution.
- Partitioned watch-rule indexes and state by feeder, keeping emergency
  deduplication independently protected across the community view.
- Moved enrichment discovery and route prefetch to the deduplicated aggregate,
  with bounded tracking, rotating scans, and once-per-second telemetry.
- Made track sampling skip off-cycle aggregate updates and use bounded LRU
  eviction without temporary visibility maps or eviction sorts.
- Reused agent compression state, separated Discord emergency and interaction
  workers, and classified transient versus permanent SQLite writer failures.

### Performance

- Added realistic 1,000-aircraft readsb, agent codec/ingress, concurrent-rule,
  track-store, and full 100-feeder pipeline benchmarks.
- Recorded statistically significant improvements in aggregate, ingest,
  replay, and heterogeneous rule workloads in `docs/performance.md`.

## 0.1.1 - 2026-08-28

### Changed

- Added a protected `dev` integration branch and a version-gated `dev` to
  `main` promotion workflow.
- Successful `main` CI now publishes the checked-in version once, including
  AMD64/ARM64 images, SBOM, provenance, vulnerability scanning, signing, and
  an immutable GitHub prerelease.
- Removed the hosted Nix cache upload that delayed completed checks; Nix still
  runs the full flake validation on every development and promotion build.
- Updated `github.com/klauspost/compress` from 1.18.4 to 1.19.2.

## 0.1.0 - 2026-08-27

### Added

- Local readsb ingestion, immutable snapshots, receiver health, rules, alerts,
  reports, emergency squawk handling, and a responsive Discord interface.
- Weather summaries, inferred approach/landing/departure trends, memory-only
  tracks, route and airport enrichment, and provider-aware attribution.
- Role-based administration and moderation with confirmation, audit history,
  durable delivery, and Discord hierarchy enforcement.
- Invited community feeders using outbound agents, Ed25519 identity, replay
  protection, privacy stripping, and bounded aggregate views.
- Interesting-aircraft and independently configurable high-interest metadata
  alerts. Community classifications are explicitly labeled as leads to verify.
- Docker Compose, signed multi-architecture release automation, Nix/NixOS
  packaging, health endpoints, metrics, backups, and recovery documentation.

### Security and privacy

- Discord tokens are file-mounted and excluded from source and images.
- Containers run non-root with a read-only root filesystem and no Linux
  capabilities.
- Receiver coordinates, private URLs, and high-cardinality identifiers are
  excluded from public presentation and telemetry.
- External clients use bounded bodies, strict redirects, timeouts, rate limits,
  and bounded caches or queues.

### Preview limitations

- The required 24-hour ARM64 soak is not yet complete.
- Community ingress remains disabled until an administrator configures a
  private mesh or authenticated HTTPS endpoint and creates an invitation.
- SkyFeed is informational and must not be used for navigation, flight safety,
  or authoritative claims about an aircraft's activity.
