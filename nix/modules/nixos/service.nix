{ config, lib, pkgs, ... }:

let
  cfg = config.services.skyfeed;
  package = cfg.package;
  databasePath = "${cfg.dataDir}/skyfeed.db";
in
{
  config = lib.mkIf cfg.enable {
    systemd.services.skyfeed = {
      description = "SkyFeed ADS-B Discord bot";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      environment = {
        SKYFEED_DISCORD_TOKEN_FILE = "%d/discord-token";
        SKYFEED_DATABASE_PATH = databasePath;
        SKYFEED_HEALTH_ADDR = "${cfg.healthAddress}:${toString cfg.healthPort}";
        SKYFEED_AGENT_ENABLED = if cfg.agentIngress.enable then "true" else "false";
        SKYFEED_AGENT_ADDR = "${cfg.agentIngress.address}:${toString cfg.agentIngress.port}";
      };

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = cfg.dataDir;
        ReadWritePaths = [ cfg.dataDir ];
        LoadCredential = [ "discord-token:${cfg.tokenFile}" ];
        ExecStart = lib.getExe package + " run";
        Restart = "on-failure";
        RestartSec = 5;
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        AmbientCapabilities = "";
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        RestrictSUIDSGID = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
        RestrictRealtime = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [ "@system-service" "~@privileged" "~@resources" ];
        SystemCallErrorNumber = "EPERM";
        EnvironmentFile =
          lib.mkIf (cfg.environmentFile != null) "-${cfg.environmentFile}";
      } // lib.optionalAttrs (cfg.dataDir == "/var/lib/skyfeed") { StateDirectory = "skyfeed"; };

      preStart =
        lib.optionalString cfg.migrateOnStart "${lib.getExe package} migrate\n"
        + lib.optionalString cfg.configCheckOnStart "${lib.getExe package} config check\n";
    };

    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      createHome = false;
    };

    users.groups.${cfg.group} = { };

    systemd.tmpfiles.rules = [
      "d ${cfg.dataDir} 0750 ${cfg.user} ${cfg.group} -"
      "d /etc/skyfeed 0750 root root -"
    ];

    networking.firewall.allowedTCPPorts =
      lib.optional cfg.openFirewall cfg.healthPort
      ++ lib.optional (cfg.agentIngress.enable && cfg.agentIngress.openFirewall) cfg.agentIngress.port;

    assertions = [
      {
        assertion = !cfg.agentIngress.enable || cfg.agentIngress.publicURL != null;
        message = "services.skyfeed.agentIngress.publicURL is required when agent ingress is enabled";
      }
    ];

    systemd.services.skyfeed.environment.SKYFEED_AGENT_PUBLIC_URL =
      lib.mkIf cfg.agentIngress.enable cfg.agentIngress.publicURL;
  };
}
