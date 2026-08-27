# Changelog

Notable user-facing changes are recorded here. SkyFeed follows semantic
versioning after the first public preview.

## Unreleased

- No unreleased changes.

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
