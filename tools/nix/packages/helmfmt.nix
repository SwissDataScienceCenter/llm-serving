{
  lib,
  buildGoModule,
  helmfmt-src,
}:
buildGoModule {
  pname = "helmfmt";
  version = "0.5.0";
  src = helmfmt-src;
  vendorHash = "sha256-Q7G+Evk6XiKVPa94lSF0vvWpHGxCenyi49Wh0irsrnw="; # Update manually if bumping

  meta = {
    description = "Code formatter for Helm charts";
    homepage = "https://github.com/digitalstudium/helmfmt";
    license = lib.licenses.gpl3Only;
    mainProgram = "helmfmt";
  };
}
