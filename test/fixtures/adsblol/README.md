# adsb.lol fixtures

Route and airport enrichment tests currently use in-process HTTP test servers.
When checked-in JSON fixtures are added, they must be hand-authored synthetic
data—not live API captures.

Policy:

- Never commit live adsb.lol responses, private coordinates, or receiver/home
  positions. Route requests contain only normalized callsigns and already-public
  aircraft positions.
- Replace airport names, route strings, and operator-specific text while
  preserving field types and representative missing/null cases.
- Keep ODbL attribution strings stable and aligned with
  `internal/enrichment/adsblol.Attribution`.
- Do not embed raw URLs in committed fixtures; attribution is validated through
  the renderer and privacy disclosure paths instead.
