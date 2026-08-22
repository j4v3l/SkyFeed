# readsb fixture policy

Live fixture capture is blocked as of 2026-08-22 because the configured
receiver returns HTTP 200 with empty bodies for all three required endpoints.
The checked-in fixtures are deliberately synthetic contract fixtures so
development can continue without representing the live receiver as verified.

When the receiver is healthy, capture each response to a temporary location,
validate it as non-empty JSON, and construct committed fixtures with synthetic
values. Never commit the raw response. Replace aircraft identifiers, callsigns,
registrations, coordinates, receiver names, network addresses, timestamps, and
other operator-specific values while preserving field types and representative
missing/null cases.

Synthetic fixtures:

- `aircraft.json`
- `receiver.json`
- `stats.json`

Before production acceptance, compare these shapes to sanitized live payloads,
add any representative optional fields, and retain the synthetic identifiers.
