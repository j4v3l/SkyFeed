# Contributing to SkyFeed

Thanks for helping improve SkyFeed. This project is a **local-first** ADS-B
Discord bot for a single readsb/tar1090 feeder. Contributions should preserve
privacy defaults, avoid the Message Content intent, and keep secrets out of the
repository.

Please follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

1. Search [existing issues](https://github.com/j4v3l/SkyFeed/issues) and
   [discussions](https://github.com/j4v3l/SkyFeed/discussions).
2. Open an issue for larger changes so scope can be confirmed early.
3. Never commit `.env`, `secrets/`, tokens, private IPs, guild IDs, or receiver
   coordinates.

## Development setup

Requirements: Go **1.27.0** (see `go.mod`), Docker optional for Compose smoke
tests.

```sh
cp .env.example .env
# edit .env; place token in secrets/discord_token (gitignored)
make check
go test ./...
```

Nix users:

```sh
nix develop
nix flake check
```

## Pull requests

- Keep PRs focused; prefer small, reviewable diffs.
- Match existing Go style (`gofmt`), package layout, and test patterns.
- Add or update tests for behavioral changes.
- Update `.env.example`, `docs/`, and the README when configuration or ops change.
- Bump Discord command schema version when slash-command contracts change.
- Use the PR template checklist.

CI runs format, tests, race, vet, staticcheck, govulncheck, license checks,
container smoke tests, and (on push/PR) `nix flake check`.

## Security

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/j4v3l/SkyFeed/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## Out of scope (default)

- Multi-tenant / cloud-hosted tracking of private receivers
- Features that require Discord Message Content intent
- Publishing private home or receiver coordinates in Discord, logs, metrics, or health JSON
