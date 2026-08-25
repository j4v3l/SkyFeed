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
        SKYFEED_DISCORD_TOKEN_FILE = cfg.tokenFile;
        SKYFEED_DATABASE_PATH = databasePath;
      };

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = "skyfeed";
        WorkingDirectory = cfg.dataDir;
        ReadWritePaths = [ cfg.dataDir ];
        ExecStart = lib.getExe package + " run";
        Restart = "on-failure";
        RestartSec = 5;
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
        RestrictRealtime = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        SystemCallArchitectures = "native";
        EnvironmentFile =
          lib.mkIf (cfg.environmentFile != null) "-${cfg.environmentFile}";
      };

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
      "d /etc/skyfeed/secrets 0700 root root -"
    ];
  };
}
