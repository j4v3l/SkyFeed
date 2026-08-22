# Cloud connectivity

SkyFeed's supported first cloud pattern is a private WireGuard or managed mesh
VPN between the cloud workload and receiver LAN. Keep the normal HTTP source
adapter and set `SKYFEED_ADSB_BASE_URL` to the receiver's private tunnel
address. Permit only that receiver address plus outbound Discord WSS/HTTPS and,
when enabled, ADSBDB HTTPS. Never create a public router port-forward for the
receiver JSON.

Run one active SkyFeed process. A second process may exist only as an inactive
standby. Shared PostgreSQL, a leadership lease, and an active-passive Gateway
are intentionally not included until a multi-replica platform is selected.

`skyfeed-agent` remains a non-running stub because no cloud platform requiring
Pattern B has been selected. The project already contains the common Source
boundary and the future agent envelope's mTLS identity, timestamp, payload
limit, monotonic-sequence, and replay validation. Enabling the agent requires a
separate transport threat model, certificate lifecycle, and failure test; it
must never become a general LAN proxy.

Use the cloud platform's secret manager to mount the Discord token at
`/run/secrets/discord_token`. Do not bake credentials into an image, Compose
file, Terraform variable default, or image build argument.
