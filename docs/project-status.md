---
layout: default
title: Project status
---

# Project status

SkyFeed `v0.1.1` is the current public preview. The application is usable for a
local receiver today, but the preview label is intentional: long-running ARM64
validation and the first production community-agent deployment remain before a
stable release.

## Available in the preview

- Bounded polling for readsb aircraft, receiver, and statistics JSON with
  immutable last-known-good snapshots and independent source health.
- Native Discord commands, autocomplete, mobile-first cards, pagination,
  weather, inferred airport movement, memory-only tracks, dashboard updates,
  reports, and privacy-aware help.
- Exact and best-effort watch rules, emergency squawk handling, cooldowns,
  hysteresis, feeder-health recovery, and separate alert priorities.
- Existing-role access tiers and moderation with Discord permission and role
  hierarchy enforcement, private confirmation, durable cases, and bounded log
  retries.
- SQLite WAL persistence with forward-only migrations, coalesced report writes,
  backup/restore commands, and no one-second raw snapshot retention.
- Optional ADSBDB, adsb.lol, airplanes.live, AviationWeather.gov, and
  plane-alert-db adapters with bounded clients, caches, attribution, and typed
  provenance.
- One local feeder plus administrator-invited outbound agents. Community
  snapshots are signed, sequence checked, coordinate stripped, size limited,
  and accepted only through opt-in HTTPS ingress.
- Distroless non-root Docker images for Linux AMD64 and ARM64, Docker Compose,
  Nix packages, and hardened NixOS services for the bot and agent.

## Deliberate boundaries

- One active Discord Gateway and alert-processing leader.
- No public receiver endpoint, arbitrary receiver URL, or general LAN proxy.
- No Message Content intent or default allowed mentions.
- No Redis, PostgreSQL, raw flight-history database, or durable ADSBDB routes.
- No claim that inferred movement or community aircraft classifications are
  authoritative.

## Stable-release gates

- Complete the 24-hour soak and 15-minute CPU/RSS measurement on the intended
  ARM64 host.
- Run the x86_64 NixOS VM service test on an x86_64 Linux builder.
- Rehearse upgrade, rollback, backup/restore, token rotation, and disaster
  recovery using the published image.
- Exercise a community feeder over the selected private mesh or authenticated
  HTTPS reverse proxy.
- Complete desktop and mobile acceptance in a development guild.

See [performance.md](performance.md) for current measurements and
[roadmap.md](roadmap.md) for planned validation.
