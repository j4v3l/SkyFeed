# Security policy

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability or include Discord
tokens, interaction payloads, private receiver addresses, guild identifiers, or
user data in a report.

Use the repository's private GitHub Security Advisory reporting flow when it is
available. Until a remote repository and private reporting channel are
published, contact the repository owner privately and provide only the minimum
sanitized reproduction information needed.

## Operational response

If a Discord token may have been exposed, reset it immediately in the Discord
Developer Portal, replace the mounted token file, restart SkyFeed, and verify
that logs and shell history do not retain it. Treat the old token as permanently
revoked.
