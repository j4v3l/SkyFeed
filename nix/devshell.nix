{ pkgs, packages }:

pkgs.mkShell {
  packages = with pkgs; [
    go_1_27
    gopls
    packages.skyfeed
  ];

  shellHook = ''
    echo "SkyFeed dev shell (Go 1.27)"
    echo "  Docker/local: cp .env.example .env and edit secrets/discord_token"
    echo "  NixOS: see docs/nix.md for /etc/skyfeed/skyfeed.env"
    echo "  skyfeed version  — flake-built binary"
    echo "  go test ./...    — run tests with host Go"
  '';
}
