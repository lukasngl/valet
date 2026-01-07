{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };
  outputs =
    { self
    , nixpkgs
    , flake-utils
    , treefmt-nix
    ,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        treefmt = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;
      in
      {
        devShells.default = pkgs.mkShell {
          hardeningDisable = [ "fortify" ];
          name = "client-secret-operator";
          buildInputs = with pkgs; [
            go
            operator-sdk
            golangci-lint
          ];
        };
        # run with `nix fmt`
        formatter = treefmt.config.build.wrapper;
        # run with `nix flake check`
        checks = {
          formatting = treefmt.config.build.check self;
          lints =
            pkgs.runCommand "golangci-lint"
              {
                buildInputs = [ pkgs.golangci-lint ];
              }
              ''
                golangci-lint run ./... || exit 1
                echo OK > $out
              '';
          # TODO: write tests and execute them here
        };
      }
    );
}
