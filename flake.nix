{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    godogen = {
      url = "github:lukasngl/godogen";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      treefmt-nix,
      godogen,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        self' = builtins.mapAttrs (name: value: value.${system} or value) self;
        treefmt = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;
        withPackageEnv =
          {
            name,
            buildPhase,
            extraBuildInputs ? [ ],
          }:
          self'.packages.secret-manager.overrideAttrs (old: {
            inherit name buildPhase;
            nativeBuildInputs = old.nativeBuildInputs ++ extraBuildInputs;
            doCheck = false;
            installPhase = "touch $out";
          });
      in
      {
        packages = rec {
          default = secret-manager;
          secret-manager = pkgs.buildGoModule {
            pname = "secret-manager";
            version = "0.1.0";
            src = self;
            vendorHash = "sha256-bbJlyAm1kVb534ewlTflDCo46w0zsgFubzByURZGaR0=";
            subPackages = [ "cmd" ];
            ldflags = [
              "-s"
              "-w"
            ];
          };
        };

        devShells.default = pkgs.mkShell {
          hardeningDisable = [ "fortify" ];
          name = "secret-manager";
          buildInputs =
            (with pkgs; [
              go
              just
              operator-sdk
              golangci-lint
              kubernetes-controller-tools
              kustomize
            ])
            ++ [
              godogen.packages.${system}.default
            ];
        };

        # run with `nix fmt`
        formatter = treefmt.config.build.wrapper;

        # run with `nix flake check`
        checks = {

          # run with `nix build .#checks.formatting`
          formatting = treefmt.config.build.check self;

          # run with `nix build .#checks.generated`
          generated = withPackageEnv {
            name = "check-generated";
            extraBuildInputs = self'.devShells.default.buildInputs ++ [ pkgs.git ];
            buildPhase = ''
              export HOME=$(mktemp -d)

              # Initialize git repo to track changes
              git init && git add .

              # Run generation
              just gen

              # Check for changes
              if ! git diff --exit-code; then
                echo "Generated files out of date. Run 'just gen' and commit."
                exit 1
              fi
            '';
          };

          # run with `nix build .#checks.golangci-lint`
          golangci-lint = withPackageEnv {
            name = "golangci-lint-check";
            extraBuildInputs = [ pkgs.golangci-lint ];
            buildPhase = ''
              export HOME=$(mktemp -d)
              golangci-lint run --timeout 10m ./...
            '';
          };

          # run with `nix build .#checks.test`
          test = withPackageEnv {
            name = "go-test";
            extraBuildInputs = [ pkgs.gotestsum ];
            buildPhase = ''
              export HOME=$(mktemp -d)
              gotestsum --format short-verbose -- -short ./...
            '';
          };

          # run with `nix build .#checks.helm-lint`
          helm-lint = pkgs.runCommand "helm-lint" { nativeBuildInputs = [ pkgs.kubernetes-helm ]; } ''
            helm lint ${self}/charts/secret-manager
            touch $out
          '';

        };
      }
    );
}
