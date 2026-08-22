# 0005: Internal ADSBDB enrichment adapter

- Status: Accepted
- Date: 2026-08-22

## Decision

Implement the narrow ADSBDB v0 combined aircraft/callsign lookup with
`net/http` and `encoding/json` behind the project `Enricher` interface. Keep
provider DTOs private and make every presentation field optional.

The reviewed go-adsbdb head (`37bb0556911d06b05daa239c1c96d143c448427f`)
is not selected because it is untagged and reads response and error bodies
without SkyFeed's required bounds, retry budget, limiter, or circuit policy.

## Consequences

Enrichment is asynchronous, bounded, cacheable, and unable to alter live state
or emergency decisions. It defaults off until the operator accepts the data-use
notice. Route enrichment and durable route storage default off; route data is
never exported in bulk and uses synthetic fixtures only.
