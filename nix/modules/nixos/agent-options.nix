{ lib, ... }:

{
  options.services.skyfeed-agent = {
    enable = lib.mkEnableOption "the outbound SkyFeed LAN feeder agent";

    package = lib.mkOption {
      type = lib.types.package;
      description = "SkyFeed package containing skyfeed-agent.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = "/etc/skyfeed-agent/agent.env";
      description = "Non-secret agent environment file outside the Nix store.";
    };

    enrollmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/run/keys/skyfeed-agent-enrollment";
      description = "One-time enrollment code loaded with systemd credentials.";
    };

    serverURL = lib.mkOption {
      type = lib.types.str;
      example = "https://skyfeed-agent.example.net";
      description = "Private HTTPS URL of the central SkyFeed service.";
    };

    receiverBaseURL = lib.mkOption {
      type = lib.types.str;
      default = "http://127.0.0.1:8080/data";
      description = "Local readsb/tar1090 data URL. This is never sent to Discord.";
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/skyfeed-agent";
      description = "Private directory for the Ed25519 key and sequence state.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "skyfeed-agent";
      description = "System user for the LAN agent.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "skyfeed-agent";
      description = "System group for the LAN agent.";
    };
  };
}
