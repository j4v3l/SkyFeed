# Security policy

## Supported versions

| Version | Supported |
| --- | --- |
| Latest `main` | Yes |
| Latest release tag (`v*.*.*`) | Yes |
| Older tags | Best effort only |

## Reporting a vulnerability

**Do not open a public issue** for a suspected vulnerability. Do not include
Discord tokens, interaction payloads, private receiver addresses, guild
identifiers, user data, or coordinates in any report.

Use GitHub’s private reporting flow:

→ [Report a vulnerability](https://github.com/j4v3l/SkyFeed/security/advisories/new)

Provide only the minimum sanitized information needed to reproduce and assess
impact. We aim to acknowledge reports within a few business days.

## Operational response

If a Discord token may have been exposed:

1. Reset it immediately in the Discord Developer Portal.
2. Replace the mounted token file (`secrets/discord_token` or
   `/etc/skyfeed/secrets/discord_token`).
3. Restart SkyFeed.
4. Verify logs and shell history do not retain the old value.

Treat the old token as permanently revoked.

## Secure defaults (expectations)

- Token is read from a file path (`SKYFEED_DISCORD_TOKEN_FILE`), never inlined in env examples committed to git.
- Health and metrics should bind to loopback in production (`127.0.0.1:9090`).
- Private receiver coordinates must not appear in Discord, metrics labels, or health JSON.
