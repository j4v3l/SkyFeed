{ config, lib, pkgs, defaultPackage, ... }:

let
  cfg = config.services.skyfeed-agent;
in
{
  config = lib.mkIf cfg.enable {
    services.skyfeed-agent.package = lib.mkDefault defaultPackage;

    systemd.services.skyfeed-agent = {
      description = "SkyFeed outbound LAN feeder agent";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      environment = {
        SKYFEED_AGENT_SERVER_URL = cfg.serverURL;
        SKYFEED_AGENT_STATE_DIR = cfg.stateDir;
        SKYFEED_ADSB_BASE_URL = cfg.receiverBaseURL;
      } // lib.optionalAttrs (cfg.enrollmentFile != null) {
        SKYFEED_AGENT_ENROLLMENT_FILE = "%d/enrollment-code";
      };

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = cfg.stateDir;
        ReadWritePaths = [ cfg.stateDir ];
        ExecStart = "${lib.getExe' cfg.package "skyfeed-agent"} run";
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
        EnvironmentFile = lib.mkIf (cfg.environmentFile != null) "-${cfg.environmentFile}";
        LoadCredential = lib.optional (cfg.enrollmentFile != null) "enrollment-code:${cfg.enrollmentFile}";
      } // lib.optionalAttrs (cfg.stateDir == "/var/lib/skyfeed-agent") { StateDirectory = "skyfeed-agent"; };
    };

    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.stateDir;
      createHome = false;
    };
    users.groups.${cfg.group} = { };
    systemd.tmpfiles.rules = [ "d ${cfg.stateDir} 0700 ${cfg.user} ${cfg.group} -" ];
  };
}
