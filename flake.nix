{
  description = "SkyFeed — local-first ADS-B Discord bot";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    {
      self,
      nixpkgs,
      ...
    }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      overlays.default = final: prev: {
        skyfeed = self.packages.${prev.stdenv.hostPlatform.system}.skyfeed;
      };

      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        let
          skyfeed = pkgs.callPackage ./nix/packages.nix {
            inherit self;
            buildGoModule = pkgs.buildGo127Module;
          };
        in
        {
          inherit skyfeed;
          default = skyfeed;
        }
      );

      apps = forAllSystems (
        system:
        let
          package = self.packages.${system}.skyfeed;
        in
        {
          default = {
            type = "app";
            program = "${package}/bin/skyfeed";
            meta.description = "Run the SkyFeed Discord bot";
          };
          skyfeed = {
            type = "app";
            program = "${package}/bin/skyfeed";
            meta.description = "Run the SkyFeed Discord bot";
          };
          skyfeed-agent = {
            type = "app";
            program = "${package}/bin/skyfeed-agent";
            meta.description = "Run the outbound SkyFeed LAN feeder agent";
          };
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = import ./nix/devshell.nix {
            inherit pkgs;
            packages = self.packages.${system};
          };
        }
      );

      nixosModules.default =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        import ./nix/modules/nixos {
          inherit lib pkgs;
          defaultPackage = self.packages.${pkgs.stdenv.hostPlatform.system}.skyfeed;
        };

      checks = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          skyfeed = self.packages.${system}.skyfeed;
        in
        {
          skyfeed-build = skyfeed;
        }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          nixos-service = import ./nix/tests/service.nix { inherit self pkgs; };
        }
      );
    };
}
