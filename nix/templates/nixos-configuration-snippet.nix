# Example NixOS configuration snippet for SkyFeed.
#
# inputs.skyfeed.url = "github:j4v3l/SkyFeed";
# imports = [ inputs.skyfeed.nixosModules.default ];
#
# services.skyfeed = {
#   enable = true;
#   environmentFile = "/etc/skyfeed/skyfeed.env";
#   tokenFile = "/etc/skyfeed/secrets/discord_token";
#   healthAddress = "127.0.0.1";
#   healthPort = 9090;
# };

{
  services.skyfeed = {
    enable = true;
    environmentFile = "/etc/skyfeed/skyfeed.env";
    tokenFile = "/etc/skyfeed/secrets/discord_token";
    healthAddress = "127.0.0.1";
    healthPort = 9090;
  };

  # Optional contributor-side service. Obtain the one-time enrollment code
  # from an administrator, store it root-owned with mode 0600, and remove it
  # after enrollment succeeds.
  # services.skyfeed-agent = {
  #   enable = true;
  #   serverURL = "https://skyfeed-agent.example.net";
  #   receiverBaseURL = "http://127.0.0.1:8080/data";
  #   enrollmentFile = "/etc/skyfeed-agent/enrollment-code";
  # };
}
