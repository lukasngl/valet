{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    godogen = {
      url = "github:lukasngl/godogen";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } (
      { lib, flake-parts-lib, ... }:
      {

        # SRI hash for the Go workspace vendor (go work vendor).
        # Update with `nix build` after changing any go.mod / go.sum.
        config.valet.lib.vendorHash = "sha256-VhLiEUn1bE90Iu5DYLjooNkiNGKQZHQaCADd2XnAWs8=";

        imports = [
          ./nix/package.nix
          ./nix/helm.nix
          ./nix/devShells.nix
          ./nix/treefmt.nix
          ./framework/flake-module.nix
          ./provider-azure/flake-module.nix
          ./provider-mock/flake-module.nix
        ];

        config.systems = [
          "x86_64-linux"
          "aarch64-linux"
        ];

        # Shared library functions for all modules. Each nix module contributes
        # its own keys (lazyAttrsOf merges at the attribute level).
        options.valet.lib = lib.mkOption {
          type = lib.types.lazyAttrsOf lib.types.raw;
          default = { };
          description = "Shared library functions for provider modules.";
        };

        options.perSystem = flake-parts-lib.mkPerSystemOption (
          { lib, ... }:
          {
            options.valet.lib = lib.mkOption {
              type = lib.types.lazyAttrsOf lib.types.raw;
              default = { };
            };
          }
        );
      }
    );
}
