{ self, lib, buildGoModule }:

let
  versionInfo = self.shortRev or self.dirtyShortRev or "unknown";
  buildDate = self.lastModifiedDate or "1970-01-01";
in
buildGoModule {
  pname = "skyfeed";
  version = "0-unstable-${versionInfo}";
  src = self;

  vendorHash = "sha256-DP9ISSsrhnRNE2GZJt5+oiYDKzICyTbXId/x4Si0j9k=";

  doCheck = false;

  tags = [ "timetzdata" ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/j4v3l/SkyFeed/internal/app.Version=${versionInfo}"
    "-X github.com/j4v3l/SkyFeed/internal/app.Commit=${self.rev or "unknown"}"
    "-X github.com/j4v3l/SkyFeed/internal/app.BuildDate=${buildDate}"
  ];

  subPackages = [ "cmd/skyfeed" ];

  postInstall = ''
    mkdir -p $out/share/doc/skyfeed
    cp ${self}/.env.example $out/share/doc/skyfeed/env.example
  '';

  meta = with lib; {
    description = "Local-first ADS-B Discord bot for a readsb/tar1090 feeder";
    homepage = "https://github.com/j4v3l/SkyFeed";
    license = licenses.asl20;
    mainProgram = "skyfeed";
    platforms = platforms.linux ++ platforms.darwin;
  };
}
