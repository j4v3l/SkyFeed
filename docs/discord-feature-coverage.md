# Discord feature coverage

| disgo / Discord capability | SkyFeed use | Test state |
|---|---|---|
| Gateway lifecycle | Minimal-intent outbound connection, reconnect/resume hooks, bounded shutdown | SDK compile/lifecycle tests; live credential gate open |
| REST | Command CRUD, dashboard create/edit, alerts, reports, destination tests, member timeouts/kicks/bans, ban removal, and warning DMs | Adapter, hierarchy, and scheduler tests; live credential gate open |
| Slash commands/subcommands | Twenty slash commands, including `/top live`, `/top traffic`, `/preferences units`, aircraft/airport/airline exploration, alerts, reports, moderation, and administration | Schema and router tests |
| Autocomplete | Live aircraft, routes, airport/airline codes, and saved rules, bounded to 25 choices | Router tests |
| Deferred/ephemeral responses | All storage-backed commands defer first; settings and moderation flows are private | Deferral-policy and router latency tests |
| Embeds | Branded status, layered aircraft, weather, local track summaries, nearby, alerts, reports, moderation cases, and tests with composite provider attribution | Aggregate/per-field limit and provenance tests |
| Buttons | Previous, next, details, track, route/weather, refresh, watch, close, raw weather detail, and 60-second kick/ban confirmation | Session ownership, authorization, and expiry tests |
| String select | Nearby sort control | Component tests |
| Modals | Save personal aircraft watch | Modal validation/persistence tests |
| Allowed mentions | Explicit empty policy on every rendered message | Presentation tests |
| Attachments | On-demand locally rendered PNG radar plot from the bounded memory-only recent track | PNG, cache, capacity, and session tests; CSV/export remains disabled |
| Voice | No ADS-B product use | Intentionally out of scope |
