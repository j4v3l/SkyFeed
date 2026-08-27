{ self, pkgs }:

pkgs.testers.nixosTest {
  name = "skyfeed-services";

  nodes.machine = { lib, ... }: {
    imports = [ self.nixosModules.default ];

    services.skyfeed = {
      enable = true;
      package = self.packages.x86_64-linux.skyfeed;
      tokenFile = toString (pkgs.writeText "synthetic-discord-token" "synthetic.test.token");
      environmentFile = null;
      migrateOnStart = false;
      configCheckOnStart = false;
    };
    services.skyfeed-agent = {
      enable = true;
      package = self.packages.x86_64-linux.skyfeed;
      environmentFile = null;
      serverURL = "https://agent.example.invalid";
    };

    systemd.services.skyfeed.serviceConfig = {
      Type = lib.mkForce "oneshot";
      ExecStart = lib.mkForce "${self.packages.x86_64-linux.skyfeed}/bin/skyfeed version";
      Restart = lib.mkForce "no";
      RemainAfterExit = true;
    };
    systemd.services.skyfeed-agent.serviceConfig = {
      Type = lib.mkForce "oneshot";
      ExecStart = lib.mkForce "${self.packages.x86_64-linux.skyfeed}/bin/skyfeed-agent config-check";
      Restart = lib.mkForce "no";
      RemainAfterExit = true;
    };
  };

  testScript = ''
    machine.start()
    machine.wait_for_unit("skyfeed.service")
    machine.wait_for_unit("skyfeed-agent.service")
    machine.succeed("systemctl is-active skyfeed.service")
    machine.succeed("systemctl is-active skyfeed-agent.service")
    machine.succeed("test $(systemctl show skyfeed.service -p NoNewPrivileges --value) = yes")
    machine.succeed("test $(systemctl show skyfeed-agent.service -p PrivateDevices --value) = yes")
    machine.succeed("test -x ${self.packages.x86_64-linux.skyfeed}/bin/skyfeed-agent")
  '';
}
