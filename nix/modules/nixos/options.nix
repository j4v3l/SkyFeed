{ lib, ... }:

{
  options.services.skyfeed = {
    enable = lib.mkEnableOption "SkyFeed ADS-B Discord bot";

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = "/etc/skyfeed/skyfeed.env";
      example = "/etc/skyfeed/skyfeed.env";
      description = ''
        Environment file in systemd EnvironmentFile format. Use the same
        SKYFEED_* keys as [.env.example](https://github.com/j4v3l/SkyFeed/blob/main/.env.example).
        Keep secrets out of the Nix store; this path should live on the host.
      '';
    };

    tokenFile = lib.mkOption {
      type = lib.types.str;
      default = "/etc/skyfeed/secrets/discord_token";
      example = "/etc/skyfeed/secrets/discord_token";
      description = ''
        Discord bot token source. systemd copies it into the service credential
        directory, so the skyfeed user never needs permission to read this path.
        Do not also set SKYFEED_DISCORD_TOKEN_FILE in environmentFile.
      '';
    };

    dataDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/skyfeed";
      example = "/var/lib/skyfeed";
      description = "Directory for the SQLite database and persistent state.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "skyfeed";
      description = "User account under which SkyFeed runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "skyfeed";
      description = "Group account under which SkyFeed runs.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Open the firewall for SKYFEED_HEALTH_ADDR when it binds to a public
        interface. Prefer 127.0.0.1:9090 in the environment file.
      '';
    };

    healthAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1";
      description = "Address for health and metrics. Keep loopback unless protected.";
    };

    healthPort = lib.mkOption {
      type = lib.types.port;
      default = 9090;
      description = "TCP port for health and metrics.";
    };

    agentIngress = {
      enable = lib.mkEnableOption "the central community feeder ingress";
      address = lib.mkOption {
        type = lib.types.str;
        default = "127.0.0.1";
        description = "Central agent ingress bind address.";
      };
      port = lib.mkOption {
        type = lib.types.port;
        default = 9091;
        description = "Central agent ingress TCP port.";
      };
      publicURL = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        example = "https://skyfeed-agent.example.net";
        description = "Private-mesh or authenticated HTTPS URL given to invited agents.";
      };
      openFirewall = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Open the central ingress TCP port. Prefer a private reverse proxy.";
      };
    };

    migrateOnStart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Run skyfeed migrate before starting the service.";
    };

    configCheckOnStart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Run skyfeed config check before starting the service.";
    };
  };
}
