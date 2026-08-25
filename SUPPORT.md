# Support

## How to get help

| Need | Where |
| --- | --- |
| Bug | [Bug report](https://github.com/j4v3l/SkyFeed/issues/new?template=bug_report.yml) |
| Feature idea | [Feature request](https://github.com/j4v3l/SkyFeed/issues/new?template=feature_request.yml) |
| Usage question | [Discussions](https://github.com/j4v3l/SkyFeed/discussions) |
| Security issue | [Private advisory](https://github.com/j4v3l/SkyFeed/security/advisories/new) |
| Ops / recovery | [docs/operations.md](docs/operations.md) |
| Nix / NixOS | [docs/nix.md](docs/nix.md) |

## Before opening an issue

1. Run `skyfeed config check` and `skyfeed source check` (or the Compose equivalents).
2. Confirm Discord bot permissions and role hierarchy.
3. Redact tokens, private IPs, guild IDs, user IDs, and coordinates from logs.

## What we cannot help with in public issues

- Recovering or accepting pasted Discord tokens
- Debugging private receiver coordinates that appear in screenshots or logs
- Third-party provider outages (airplanes.live, ADSBDB, adsb.lol) beyond SkyFeed’s client behavior
