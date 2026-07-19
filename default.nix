{
  lib,
  buildGoModule,
  nix,
}:

buildGoModule (finalAttrs: {
  pname = "aeroflare";
  version = (lib.importJSON ./version.json).".";
  doCheck = true;

  src = ./.;

  vendorHash = "sha256-H4jgc08mklolpHQNlcQx5JzpCDBYpujgoKFR2Ct8xR8=";
  # No runtime PATH wrapping: the only tools aeroflare shells out to are `nix`
  # and `nix-store`, and those must come from the user's own installation
  # rather than a version pinned by this package.

  # internal/prepare shells out to `nix-store --dump` to serialize NARs, so the
  # checkPhase needs the binary on PATH. Dumping a path reads no store state,
  # which is why this works inside the build sandbox.
  nativeCheckInputs = [ nix ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/itzemoji/aeroflare/internal/build.Version=${finalAttrs.version}"
  ];

  subPackages = [
    "cmd/aeroflare"
    "cmd/aeroflare-ci"
  ];

  meta = {
    description = "The OCI-based Nix-Binary-Cache written in Go";
    homepage = "https://github.com/itzemoji/aeroflare";
    changelog = "https://github.com/itzemoji/aeroflare/blob/v${finalAttrs.version}/CHANGELOG.md";
    license = lib.licenses.gpl3Only;
    maintainers = with lib.maintainers; [ itzemoji ];
    mainProgram = "aeroflare";
  };
})
