# readsb fixture policy

The configured receiver contract was compared with readsb 3.16.15 on
2026-08-22. Current releases expose one-minute statistics through `last1min`
with `max_distance`; older releases may expose `latest` with
`max_distance_in_metres`. The checked-in fixtures are deliberately synthetic
and cover both shapes without retaining receiver-specific observations.

When the receiver is healthy, capture each response to a temporary location,
validate it as non-empty JSON, and construct committed fixtures with synthetic
values. Never commit the raw response. Replace aircraft identifiers, callsigns,
registrations, coordinates, receiver names, network addresses, timestamps, and
other operator-specific values while preserving field types and representative
missing/null cases.

Synthetic fixtures:

- `aircraft.json`
- `receiver.json`
- `stats.json` (legacy statistics shape)
- `stats-current.json` (current statistics shape)

Before production acceptance, compare these shapes to sanitized live payloads,
add any representative optional fields, and retain the synthetic identifiers.

Related fixture policies: [airplanes.live](../airplaneslive/README.md) and
[adsb.lol](../adsblol/README.md).
