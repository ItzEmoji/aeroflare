{
  description = "Aeroflare - OCI-backed Nix binary cache";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };
  outputs = inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      perSystem = { pkgs, config, system, ... }:
        {
          packages.default = pkgs.callPackage ./default.nix { };
          packages.aeroflare = config.packages.default;
          devShells.default = pkgs.mkShell {
            nativeBuildInputs = with pkgs; [
              go
              golangci-lint
            ]; 
          };
        };
    };
}
