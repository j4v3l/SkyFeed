{ lib, pkgs, defaultPackage, ... }:

{
  imports = [
    ./options.nix
    ./service.nix
  ];

  options.services.skyfeed.package = lib.mkOption {
    type = lib.types.package;
    default = defaultPackage;
    description = "SkyFeed package to run.";
  };
}
