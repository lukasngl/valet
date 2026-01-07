{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };
  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      treefmt-nix,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        self' = builtins.mapAttrs (name: value: value.${system} or value) self;
        treefmt = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;
      in
      {
        packages = rec {
          default = secret-manager;
          secret-manager = pkgs.buildGoModule {
            pname = "secret-manager";
            version = "0.1.0";
            src = self;
            vendorHash = "sha256-VoGrQbgmGZgo1EI2ddoLyv0+nJtYPaC1NMO/OncLt00=";
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
          buildInputs = with pkgs; [
            go
            just
            operator-sdk
            golangci-lint
            kubernetes-controller-tools
            kustomize
          ];
        };

        # run with `nix fmt`
        formatter = treefmt.config.build.wrapper;

        # run with `nix flake check`
        checks = {

          # run with `nix build .#checks.formatting`
          formatting = treefmt.config.build.check self;

          # run with `nix build .#checks.generated`
          generated = self'.packages.secret-manager.overrideAttrs (old: {
            name = "check-generated";
            nativeBuildInputs = old.nativeBuildInputs ++ self'.devShells.default.buildInputs ++ [ pkgs.git ];
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
            installPhase = ''
              touch $out
            '';
          });

          # run with `nix build .#checks.golangci-lint`
          golangci-lint = self'.packages.secret-manager.overrideAttrs (old: {
            name = "golangci-lint-check";
            nativeBuildInputs = old.nativeBuildInputs ++ [ pkgs.golangci-lint ];
            buildPhase = ''
              export HOME=$(mktemp -d)
              golangci-lint run --timeout 5m ./...
            '';
            installPhase = ''
              touch $out
            '';
          });

          # run with `nix build .#checks.helm-lint`
          helm-lint = pkgs.runCommand "helm-lint" { nativeBuildInputs = [ pkgs.kubernetes-helm ]; } ''
            helm lint ${self}/charts/secret-manager
            touch $out
          '';

          # Tests run via buildGoModule's doCheck (default true)
          # The package build itself runs `go test`
        };
      }
    );
}
