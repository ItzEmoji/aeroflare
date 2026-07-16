{
  lib,
  buildGoModule,
  makeWrapper,
  nix,
  git,
  wget,
  gnutar,
  gzip,
  bash,
}:

let
  # `init` shells out to git (to push the generated proxy repo) and to
  # `sh -c 'wget … | tar -xz'` (to fetch the worker script from a release), and
  # the package declared none of them. `nix run github:ItzEmoji/aeroflare -- init`
  # therefore failed partway through on any machine that didn't happen to have
  # them installed — exactly the fresh-machine case init exists for.
  runtimeDeps = [
    git
    wget
    gnutar
    gzip
    bash
  ];
in
buildGoModule (finalAttrs: {
  pname = "aeroflare";
  version = (lib.importJSON ./version.json).".";
  doCheck = false;

  src = ./.;

  vendorHash = "sha256-zAqJnCrNgMWPEMQkvXotLuIceap00KuXx/2F6HxYGPk=";

  nativeBuildInputs = [ makeWrapper ];

  # --suffix, so a tool already on the user's PATH still takes precedence and
  # these only fill in what's missing.
  postFixup = ''
    for prog in $out/bin/*; do
      wrapProgram "$prog" --suffix PATH : ${lib.makeBinPath runtimeDeps}
    done
  '';

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
    maintainers = with lib.maintainers; [ ];
    mainProgram = "aeroflare";
  };
})
