{ lib, ... }:

{
  options.services.skyfeed = {
    enable = lib.mkEnableOption "SkyFeed ADS-B Discord bot";

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = "/etc/skyfeed/skyfeed.env";
      example = "/etc/skyfeed/skyfeed.env";
      description = ''
        Environment file in systemd EnvironmentFile format. Use the same
        SKYFEED_* keys as [.env.example](https://github.com/j4v3l/SkyFeed/blob/main/.env.example).
        Keep secrets out of the Nix store; this path should live on the host.
      '';
    };

    tokenFile = lib.mkOption {
      type = lib.types.path;
      default = "/etc/skyfeed/secrets/discord_token";
      example = "/etc/skyfeed/secrets/discord_token";
      description = ''
        Discord bot token file. Sets SKYFEED_DISCORD_TOKEN_FILE unless that
        variable is already defined in environmentFile.
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
