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

For invited community receivers, `skyfeed-agent` is the supported outbound-only
pattern. An administrator creates an ephemeral one-time invitation with
`/feeders invite`; the agent generates an Ed25519 key locally and enrolls over
HTTPS. Each compressed normalized snapshot is signed and sequence-checked.
The central ingress accepts only enrollment and snapshots and can never act as
a general LAN proxy.

Bind ingress to loopback (the default) behind a private mesh or authenticated
HTTPS reverse proxy. `deploy/compose.ingress.yaml` enables the central listener
without publishing it beyond `127.0.0.1:9091`; `deploy/compose.agent.yaml` runs
the contributor-side process with a private persistent key directory. Community
ingress remains disabled until an administrator explicitly enables it and
creates the first invitation.

Use the cloud platform's secret manager to mount the Discord token at
`/run/secrets/discord_token`. Do not bake credentials into an image, Compose
file, Terraform variable default, or image build argument.
