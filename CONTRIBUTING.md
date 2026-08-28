# Contributing to SkyFeed

Thanks for helping improve SkyFeed. This project is a **local-first** ADS-B
Discord bot for one directly connected readsb/tar1090 feeder and optional
administrator-invited community feeders. Contributions should preserve privacy
defaults, avoid the Message Content intent, and keep secrets out of the
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

- Base normal feature, fix, documentation, and dependency pull requests on
  `dev`. The default branch is `dev`.
- `main` is the release branch. Only the repository's `dev` branch may open a
  promotion pull request to `main`.
- Keep PRs focused; prefer small, reviewable diffs.
- Match existing Go style (`gofmt`), package layout, and test patterns.
- Add or update tests for behavioral changes.
- Update `.env.example`, `docs/`, and the README when configuration or ops change.
- Bump Discord command schema version when slash-command contracts change.
- Use the PR template checklist.

CI runs format, tests, race, vet, staticcheck, govulncheck, license checks,
container smoke tests, and (on push/PR) `nix flake check`.

## Releases

Before promoting `dev` to `main`, update `VERSION`, add a dated changelog
section, and add `docs/releases/v<version>.md`. The promotion check rejects an
existing tag or a pull request from any branch other than `dev`.

After the promotion merge, successful `main` CI publishes the version exactly
once, signs and attests the multi-platform image, creates an immutable release
tag, and fast-forwards `dev` to the released main commit. Do not create release
tags manually.

## Security

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/j4v3l/SkyFeed/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## Out of scope (default)

- Publicly exposing receivers or accepting arbitrary receiver URLs
- Multiple active Discord Gateway or alert-processing leaders
- Features that require Discord Message Content intent
- Publishing private home or receiver coordinates in Discord, logs, metrics, or health JSON
