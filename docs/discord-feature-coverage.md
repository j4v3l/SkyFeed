# Discord feature coverage

| disgo / Discord capability | SkyFeed use | Test state |
|---|---|---|
| Gateway lifecycle | Minimal-intent outbound connection, reconnect/resume hooks, bounded shutdown | SDK compile/lifecycle tests; live credential gate open |
| REST | Command CRUD, dashboard create/edit, alerts, reports, destination tests, member timeouts/kicks/bans, ban removal, and warning DMs | Adapter, hierarchy, and scheduler tests; live credential gate open |
| Slash commands/subcommands | `/status`, `/nearby`, `/aircraft`, `/watch`, `/alerts`, `/reports`, `/feeder`, `/settings`, `/moderation`, `/help` | Schema and router tests |
| Autocomplete | Live aircraft and saved rules, bounded to 25 choices | Router tests |
| Deferred/ephemeral responses | All storage-backed commands defer first; settings and moderation flows are private | Deferral-policy and router latency tests |
| Embeds | Branded status, aircraft, nearby, alerts, reports, moderation cases, and tests | Aggregate/per-field limit tests |
| Buttons | Previous, next, refresh, watch, close, and 60-second kick/ban confirmation | Session ownership, authorization, and expiry tests |
| String select | Nearby sort control | Component tests |
| Modals | Save personal aircraft watch | Modal validation/persistence tests |
| Allowed mentions | Explicit empty policy on every rendered message | Presentation tests |
| Attachments | Disabled pending export/data-use review | Intentionally not enabled |
| Voice | No ADS-B product use | Intentionally out of scope |
