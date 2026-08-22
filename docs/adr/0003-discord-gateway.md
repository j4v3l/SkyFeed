# 0003: disgo Gateway integration

- Status: Accepted
- Date: 2026-08-22

## Decision

Use github.com/disgoorg/disgo v0.19.6 for Gateway, REST, application commands,
interactions, autocomplete, components, modals, rate-limit handling, and
graceful close. Use only outbound Gateway WebSocket and HTTPS connections and
request no Message Content intent.

Only `internal/discord` may import disgo. Domain and storage code use
project-owned values. The adapter owns a narrow fakeable transport boundary for
tests and a bounded, prioritized outbound scheduler.

## Consequences

No second Discord SDK or inbound interaction server is introduced. Guild-scoped
commands are the local-development default; global registration is a deliberate
release operation. Voice remains out of scope.
