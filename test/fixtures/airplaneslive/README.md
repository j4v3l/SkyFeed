# airplanes.live fixtures

These point responses are hand-authored synthetic data. They are not live API
captures and contain no receiver, home, or private location.

Policy:

- Never commit live airplanes.live captures or private center coordinates.
- Use generic coordinates (for example `1.5`, `-2.5`) and synthetic identifiers
  in JSON fixtures; public airport reference values such as KPBI belong only in
  deployment examples and configuration documentation.
- Preserve representative field types, missing/null cases, millisecond
  timestamps, emergency states, and empty `ac` arrays.
- Sanitize callsigns, registrations, URLs, and operator-specific text before
  commit.

Synthetic fixtures:

- `point.json`
- `point-empty.json`

Before production acceptance, compare these shapes to sanitized live payloads,
add any representative optional fields, and retain the synthetic identifiers.
