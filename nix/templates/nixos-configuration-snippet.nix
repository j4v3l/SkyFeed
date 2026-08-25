# Example NixOS configuration snippet for SkyFeed.
#
# inputs.skyfeed.url = "github:j4v3l/SkyFeed";
# imports = [ inputs.skyfeed.nixosModules.default ];
#
# services.skyfeed = {
#   enable = true;
#   environmentFile = "/etc/skyfeed/skyfeed.env";
#   tokenFile = "/etc/skyfeed/secrets/discord_token";
# };

{
  services.skyfeed = {
    enable = true;
    environmentFile = "/etc/skyfeed/skyfeed.env";
    tokenFile = "/etc/skyfeed/secrets/discord_token";
  };
}
