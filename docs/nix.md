# Nix and NixOS

SkyFeed ships a [Nix flake](https://nixos.org/manual/nix/stable/command-ref/new-cli/nix3-flake.html) for native installs and hardened `services.skyfeed` and `services.skyfeed-agent` NixOS modules. Configuration uses the same non-secret `SKYFEED_*` environment variables as Docker Compose; secrets are passed through systemd credentials.

## Requirements

- **Flake input:** `github:NixOS/nixpkgs/nixos-unstable` (Go **1.27** via `buildGo127Module`)
- **Stable nixpkgs channels** may ship an older default Go toolchain; pin unstable or override the builder if you fork the flake
- **Platforms:** `x86_64-linux`, `aarch64-linux`, `aarch64-darwin`

## Quick install (any system)

```sh
nix run github:j4v3l/SkyFeed -- version
nix run github:j4v3l/SkyFeed -- --help
nix run github:j4v3l/SkyFeed#skyfeed-agent -- version
```

Build the binary locally:

```sh
nix build github:j4v3l/SkyFeed
./result/bin/skyfeed version
```

Run checks against a local `.env` (same keys as Compose). Keep token paths local and never commit this file:

```sh
set -a; source .env; set +a
nix run . -- config check
nix run . -- source check
```

The flake installs a reference copy of the env template at `$out/share/doc/skyfeed/env.example`.

## NixOS module

Add the flake input and import the module:

```nix
{
  inputs.skyfeed.url = "github:j4v3l/SkyFeed";

  # configuration.nix
  imports = [ inputs.skyfeed.nixosModules.default ];

  services.skyfeed = {
    enable = true;
    environmentFile = "/etc/skyfeed/skyfeed.env";
    tokenFile = "/etc/skyfeed/secrets/discord_token";
  };
}
```

See [`nix/templates/nixos-configuration-snippet.nix`](../nix/templates/nixos-configuration-snippet.nix) for a minimal example.

### First-time host setup

Create directories, copy the template, and install secrets **outside** the Nix store:

```sh
sudo install -d -m 750 /etc/skyfeed /var/lib/skyfeed
sudo install -d -m 700 /etc/skyfeed/secrets
cp .env.example /etc/skyfeed/skyfeed.env   # edit values
sudo install -m 600 secrets/discord_token /etc/skyfeed/secrets/discord_token
sudo chown root:root /etc/skyfeed/secrets/discord_token
sudo chown root:skyfeed /etc/skyfeed/skyfeed.env
sudo chmod 640 /etc/skyfeed/skyfeed.env
```

The `skyfeed` user reads the non-secret environment file. It does **not** need access to the source token: `LoadCredential` gives the service a private, read-only copy at runtime.

Edit `/etc/skyfeed/skyfeed.env` and set at least:

```env
SKYFEED_DISCORD_APPLICATION_ID=YOUR_APPLICATION_ID
SKYFEED_DISCORD_GUILD_ID=YOUR_GUILD_ID
SKYFEED_ADSB_BASE_URL=http://192.168.x.y/data
SKYFEED_DATABASE_PATH=/var/lib/skyfeed/skyfeed.db
```

The module owns `SKYFEED_DISCORD_TOKEN_FILE`, `SKYFEED_DATABASE_PATH`, and the health bind. Configure those with `tokenFile`, `dataDir`, `healthAddress`, and `healthPort`, not in `environmentFile`.

Apply and start:

```sh
sudo nixos-rebuild switch
sudo systemctl status skyfeed
curl --fail http://127.0.0.1:9090/livez
```

Before the first start, validate configuration:

```sh
sudo systemctl start skyfeed
journalctl -u skyfeed -n 30 --no-pager
```

On boot the unit runs `skyfeed migrate` and `skyfeed config check` (both configurable via `services.skyfeed.migrateOnStart` and `configCheckOnStart`).

### Module options

| Option | Default | Purpose |
| --- | --- | --- |
| `enable` | `false` | Enable the systemd service |
| `package` | flake package | Override binary for forks |
| `environmentFile` | `/etc/skyfeed/skyfeed.env` | systemd `EnvironmentFile=` (optional `-` prefix if missing on first boot) |
| `tokenFile` | `/etc/skyfeed/secrets/discord_token` | Discord token copied through `LoadCredential` |
| `dataDir` | `/var/lib/skyfeed` | SQLite and working directory |
| `user` / `group` | `skyfeed` | Service account |
| `healthAddress` / `healthPort` | `127.0.0.1` / `9090` | Typed health and metrics bind |
| `agentIngress.enable` | `false` | Enable invited-feeder ingress only when deliberately configured |
| `agentIngress.address` / `port` | `127.0.0.1` / `9091` | Loopback central ingress bind |
| `agentIngress.publicURL` | `null` | Required private-mesh or HTTPS reverse-proxy URL when enabled |
| `agentIngress.openFirewall` | `false` | Open the ingress port; normally leave disabled |
| `migrateOnStart` | `true` | Run embedded migrations before start |
| `configCheckOnStart` | `true` | Fail fast on invalid env |
| `openFirewall` | `false` | Open the configured health port |

### Community feeder agent

The LAN agent initiates every connection outbound and never exposes readsb. Put the 15-minute one-time enrollment code in a root-owned `0600` file, then enable the separate service:

```nix
services.skyfeed-agent = {
  enable = true;
  serverURL = "https://skyfeed-agent.example.net";
  receiverBaseURL = "http://127.0.0.1:8080/data";
  environmentFile = "/etc/skyfeed-agent/agent.env";
  enrollmentFile = "/etc/skyfeed-agent/enrollment-code";
};
```

`LoadCredential` presents the enrollment code privately. The agent creates its Ed25519 key in `/var/lib/skyfeed-agent`; after enrollment the code is no longer used and can be removed. Use a private mesh or authenticated HTTPS reverse proxy for the central URL.

## Environment variable reference

NixOS uses `/etc/skyfeed/skyfeed.env` instead of the repo-root `.env` used by Docker Compose. **Variable names are identical.** Canonical list: [`.env.example`](../.env.example).

| Variable | Docker (`.env`) | NixOS (`/etc/skyfeed/skyfeed.env`) | Notes |
| --- | --- | --- | --- |
| `SKYFEED_DISCORD_TOKEN_FILE` | `/run/secrets/discord_token` | managed by module | Docker mount or systemd credential; never inline token |
| `SKYFEED_DISCORD_APPLICATION_ID` | same | same | Required |
| `SKYFEED_DISCORD_GUILD_ID` | same | same | Development guild |
| `SKYFEED_DISCORD_GLOBAL_COMMANDS` | same | same | Default `false` |
| `SKYFEED_DISCORD_*_ROLE_ID` | same | same | Optional access-tier bindings |
| `SKYFEED_ADSB_BASE_URL` | same | same | LAN receiver URL ending in `/data` |
| `SKYFEED_AIRCRAFT_POLL` | same | same | Default `1s` |
| `SKYFEED_METADATA_POLL` | same | same | Default `30s` |
| `SKYFEED_AIRCRAFT_PROVIDER_ORDER` | same | same | e.g. `readsb,airplanes-live` |
| `SKYFEED_PUBLIC_CENTER_*` | same | same | Public airport reference for fallback |
| `SKYFEED_DOMESTIC_COUNTRY_ISO` | same | same | Default derived from airport prefix |
| `SKYFEED_AIRPLANES_LIVE_*` | same | same | Radius, timeout, poll |
| `SKYFEED_DATABASE_PATH` | `/var/lib/skyfeed/skyfeed.db` | `/var/lib/skyfeed/skyfeed.db` | Module sets default if omitted |
| `SKYFEED_ADSBDB_*` | same | same | Opt-in enrichment |
| `SKYFEED_ADSBLOL_*` | same | same | Route/airport enrichment |
| `SKYFEED_PLANE_ALERT_*` | same | same | Interesting aircraft matching |
| `SKYFEED_DASHBOARD_INTERVAL` | same | same | Default `15s` |
| `SKYFEED_ADMIN_DIGEST_INTERVAL` | same | same | Default `6h`; `0` disables |
| `SKYFEED_HEALTH_ADDR` | `0.0.0.0:9090` in example | managed by module | Configure with typed NixOS options |
| `SKYFEED_AGENT_*` | same | module or agent env | Central ingress is disabled by default |
| `SKYFEED_PPROF_ADDR` | same | same | Optional profiling |
| `SKYFEED_LOG_LEVEL` | same | same | Default `info` |
| `SKYFEED_LOG_FORMAT` | same | same | Default `json` |
| `SKYFEED_TIMEZONE` | same | same | Default `UTC` |

Docker Compose may omit newer keys; treat `.env.example` as authoritative when migrating.

## Developer shell

```sh
nix develop
go test ./...
skyfeed version   # flake-built binary from devShell
```

## CI and platform notes

- Flake evaluation covers Linux amd64/ARM64 and Apple Silicon; the x86_64 Linux check includes a NixOS VM service test
- `aarch64-linux` (for example Raspberry Pi 64-bit) uses the same package and test-enabled Go build
- After changing `go.mod` dependencies, update `vendorHash` in `nix/packages.nix` (run a local `nix build` and copy the hash from the error message)

## Parity with Docker

| Concern | Docker Compose | NixOS |
| --- | --- | --- |
| Config file | `.env` + `--env-file` | `/etc/skyfeed/skyfeed.env` via `EnvironmentFile` |
| Discord token | `secrets/discord_token` mount | systemd `LoadCredential` from a root-owned file |
| Data volume | compose volume | `/var/lib/skyfeed` (`StateDirectory`) |
| Migrations | automatic on start | `ExecStartPre` → `skyfeed migrate` |
| Health | host `127.0.0.1:9090` | typed `healthAddress` / `healthPort` options |

Container images remain the primary OCI deploy path; Nix is a second supported install method.
