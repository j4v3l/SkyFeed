---
layout: default
title: Roadmap
---

# Roadmap

SkyFeed `v0.1.1` is a public preview. The local receiver path, invited community
agents, Discord interface, moderation, weather, movement inference, reporting,
Docker packaging, and Nix/NixOS support are implemented. Preview status reflects
the operational validation still required before a stable release.

## Before v1.0

- Complete a 24-hour soak and a 15-minute CPU/RSS measurement on the intended
  ARM64 host.
- Run the x86_64 NixOS VM service test on an x86_64 Linux builder.
- Rehearse live upgrade, rollback, backup/restore, token rotation, and disaster
  recovery from the published container image.
- Exercise a real community-feeder invitation through the chosen private mesh
  or authenticated HTTPS reverse proxy.
- Complete development-guild acceptance on desktop and mobile for aircraft,
  weather/activity, alerts, reports, feeder administration, and moderation.
- Reconfirm data-use terms and attribution for every optional provider.

## Later, when justified

- Add PostgreSQL, shared leases, or Redis only if multiple active replicas are
  introduced.
- Add a LAN agent emergency path only if outage testing demonstrates a clear
  resilience need.
- Consider PGO or a different JSON decoder only after representative profiles
  show a stable bottleneck and benchmarks demonstrate a material improvement.
- Consider longer historical retention only as an opt-in subsystem with
  explicit disk and privacy limits.

New work must preserve private receiver networking, bounded queues and caches,
one active Discord/alert leader, domain independence from adapters, and the rule
that optional enrichment never determines live or safety-sensitive state.
