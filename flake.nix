{
  description = "Aeroflare - OCI-backed Nix binary cache";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    go-overlay.url = "github:purpleclay/go-overlay";
  };
  outputs = inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      perSystem = { pkgs, config, system, ... }:
        let
          pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = [ inputs.go-overlay.overlays.default ];
          };
          go = pkgs.go-bin.fromGoMod ./go.mod;
        in
        {
          packages.default = pkgs.buildGoApplication {
            inherit go;
            pname = "aeroflare";
            version = (builtins.fromJSON (builtins.readFile ./version.json)).".";
            src = ./.;
            modules = ./govendor.toml;
            doCheck = false;
          };
          packages.aeroflare = config.packages.default;
          devShells.default = pkgs.mkShell {
            nativeBuildInputs = with pkgs; [
              go
              inputs.go-overlay.packages.${stdenv.hostPlatform.system}.govendor
              golangci-lint
            ]; 
          };
        };
    };
}
