{
  lib,
  buildGoModule,
  fetchFromGitHub,
}:

buildGoModule (finalAttrs: {
  pname = "aeroflare";
  version = "1.10.0";

  src = fetchFromGitHub {
    owner = "itzemoji";
    repo = "aeroflare";
    tag = "v${finalAttrs.version}";
    hash = "sha256-4l0QVJAgKqS/tnMD6GncQdbefWPncmS4QDIz7QEiPrk=";
  };

  vendorHash = "sha256-H4jgc08mklolpHQNlcQx5JzpCDBYpujgoKFR2Ct8xR8";

  ldflags = [ "-s" ];

  meta = {
    description = "The OCI-based Nix-Binary-Cache written in Go";
    homepage = "https://github.com/itzemoji/aeroflare";
    changelog = "https://github.com/itzemoji/aeroflare/blob/${finalAttrs.src.rev}/CHANGELOG.md";
    license = lib.licenses.gpl3Only;
    maintainers = with lib.maintainers; [ ];
    mainProgram = "aeroflare";
  };
})
