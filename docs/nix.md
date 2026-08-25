# Nix and NixOS

SkyFeed ships a [Nix flake](https://nixos.org/manual/nix/stable/command-ref/new-cli/nix3-flake.html) for native installs and a `services.skyfeed` NixOS module. Configuration uses the same `SKYFEED_*` environment variables as Docker Compose—only the file location changes.

## Requirements

- **Flake input:** `github:NixOS/nixpkgs/nixos-unstable` (Go **1.27** via `buildGo127Module`)
- **Stable nixpkgs channels** may ship an older default Go toolchain; pin unstable or override the builder if you fork the flake
- **Platforms:** `x86_64-linux`, `aarch64-linux`, `x86_64-darwin`, `aarch64-darwin`

## Quick install (any system)

```sh
nix run github:j4v3l/SkyFeed -- version
nix run github:j4v3l/SkyFeed -- --help
```

Build the binary locally:

```sh
nix build github:j4v3l/SkyFeed
./result/bin/skyfeed version
```

Run checks against a local `.env` (same keys as Compose):

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
sudo chown root:skyfeed /etc/skyfeed/secrets/discord_token
sudo chmod 640 /etc/skyfeed/secrets/discord_token
sudo chown root:skyfeed /etc/skyfeed/skyfeed.env
sudo chmod 640 /etc/skyfeed/skyfeed.env
```

The `skyfeed` system user must read the env file and token. Group `640` on both files (owned by `root:skyfeed`) is the recommended pattern.

Edit `/etc/skyfeed/skyfeed.env` and set at least:

```env
SKYFEED_DISCORD_TOKEN_FILE=/etc/skyfeed/secrets/discord_token
SKYFEED_DISCORD_APPLICATION_ID=YOUR_APPLICATION_ID
SKYFEED_DISCORD_GUILD_ID=YOUR_GUILD_ID
SKYFEED_ADSB_BASE_URL=http://192.168.x.y/data
SKYFEED_DATABASE_PATH=/var/lib/skyfeed/skyfeed.db
SKYFEED_HEALTH_ADDR=127.0.0.1:9090
```

The module also sets `SKYFEED_DISCORD_TOKEN_FILE` and `SKYFEED_DATABASE_PATH` in the unit; values in `environmentFile` override those when present. Prefer configuring paths once in the env file for clarity.

Apply and start:

```sh
sudo nixos-rebuild switch
sudo systemctl status skyfeed
curl --fail http://127.0.0.1:9090/livez
```

Before the first start, validate configuration:

```sh
sudo -u skyfeed env $(grep -v '^#' /etc/skyfeed/skyfeed.env | xargs) \
  SKYFEED_DISCORD_TOKEN_FILE=/etc/skyfeed/secrets/discord_token \
  /run/current-system/sw/bin/skyfeed config check
```

On boot the unit runs `skyfeed migrate` and `skyfeed config check` (both configurable via `services.skyfeed.migrateOnStart` and `configCheckOnStart`).

### Module options

| Option | Default | Purpose |
| --- | --- | --- |
| `enable` | `false` | Enable the systemd service |
| `package` | flake package | Override binary for forks |
| `environmentFile` | `/etc/skyfeed/skyfeed.env` | systemd `EnvironmentFile=` (optional `-` prefix if missing on first boot) |
| `tokenFile` | `/etc/skyfeed/secrets/discord_token` | Discord token path (also set in unit environment) |
| `dataDir` | `/var/lib/skyfeed` | SQLite and working directory |
| `user` / `group` | `skyfeed` | Service account |
| `migrateOnStart` | `true` | Run embedded migrations before start |
| `configCheckOnStart` | `true` | Fail fast on invalid env |
| `openFirewall` | `false` | Reserved; keep health on loopback |

## Environment variable reference

NixOS uses `/etc/skyfeed/skyfeed.env` instead of the repo-root `.env` used by Docker Compose. **Variable names are identical.** Canonical list: [`.env.example`](../.env.example).

| Variable | Docker (`.env`) | NixOS (`/etc/skyfeed/skyfeed.env`) | Notes |
| --- | --- | --- | --- |
| `SKYFEED_DISCORD_TOKEN_FILE` | `/run/secrets/discord_token` | `/etc/skyfeed/secrets/discord_token` | File read at runtime; never inline token |
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
| `SKYFEED_HEALTH_ADDR` | `0.0.0.0:9090` in example | **`127.0.0.1:9090` recommended** | Bind health to loopback on NixOS |
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

- GitHub Actions runs `nix flake check` on `x86_64-linux`
- `aarch64-linux` (e.g. Raspberry Pi 64-bit) builds with the same flake on native hardware; cross-compilation from other systems requires binfmt/QEMU and is not required for CI
- After changing `go.mod` dependencies, update `vendorHash` in `nix/packages.nix` (run a local `nix build` and copy the hash from the error message)

## Parity with Docker

| Concern | Docker Compose | NixOS |
| --- | --- | --- |
| Config file | `.env` + `--env-file` | `/etc/skyfeed/skyfeed.env` via `EnvironmentFile` |
| Discord token | `secrets/discord_token` mount | `/etc/skyfeed/secrets/discord_token` |
| Data volume | compose volume | `/var/lib/skyfeed` (`StateDirectory`) |
| Migrations | automatic on start | `ExecStartPre` → `skyfeed migrate` |
| Health | host `127.0.0.1:9090` | set `SKYFEED_HEALTH_ADDR=127.0.0.1:9090` |

Container images remain the primary OCI deploy path; Nix is a second supported install method.
