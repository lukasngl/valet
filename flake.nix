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
    let
      vendorHash = "sha256-gmhUUJBWAcMik8baCXdvtdPyvVthOU/XXo9msA0V2Rc=";
    in
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      imports = [
        ./treefmt.nix
        ./framework/flake-module.nix
        ./provider-azure/flake-module.nix
        ./provider-mock/flake-module.nix
      ];

      perSystem =
        {
          pkgs,
          inputs',
          lib,
          ...
        }:
        let
          version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ./version.txt);

          # Single workspace-level vendor. All packages and checks share this.
          workspaceVendor =
            (pkgs.buildGoModule {
              pname = "workspace";
              inherit version vendorHash;
              src = inputs.self;
              overrideModAttrs = _: _: {
                buildPhase = ''
                  runHook preBuild
                  export GIT_SSL_CAINFO=$NIX_SSL_CERT_FILE
                  go work vendor
                  mkdir -p vendor
                  runHook postBuild
                '';
              };
              subPackages = [ ];
              buildPhase = "true";
              installPhase = "mkdir -p $out";
            }).goModules;

          envtest-binaries = pkgs.linkFarm "envtest-binaries" {
            etcd = "${pkgs.etcd}/bin/etcd";
            kube-apiserver = "${pkgs.kubernetes}/bin/kube-apiserver";
          };

          # Build a Go binary from the workspace using the shared vendor.
          mkGoModule =
            {
              pname,
              subPackages,
              tags ? [ "netgo" ],
              ldflags ? [
                "-s"
                "-w"
                "-X main.version=${version}"
              ],
              ...
            }@args:
            pkgs.buildGoModule (
              {
                inherit
                  pname
                  version
                  subPackages
                  tags
                  ldflags
                  ;
                src = inputs.self;
                vendorHash = null;
                preConfigure = ''
                  cp -r --reflink=auto ${workspaceVendor} vendor
                '';
              }
              // builtins.removeAttrs args [
                "pname"
                "subPackages"
                "tags"
                "ldflags"
              ]
            );

          # Override a package to run a check instead of producing a binary.
          withPackageEnv =
            basePackage:
            {
              name,
              buildPhase,
              extraBuildInputs ? [ ],
            }:
            basePackage.overrideAttrs (old: {
              inherit name buildPhase;
              nativeBuildInputs = old.nativeBuildInputs ++ extraBuildInputs;
              doCheck = false;
              installPhase = "touch $out";
            });
        in
        {
          _module.args = {
            inherit
              mkGoModule
              withPackageEnv
              workspaceVendor
              envtest-binaries
              version
              ;
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
                kubernetes-helm
                kustomize
                skopeo
              ])
              ++ [
                inputs'.godogen.packages.default
              ];
            KUBEBUILDER_ASSETS = "${envtest-binaries}";
          };

          devShells.ci = pkgs.mkShell {
            name = "secret-manager-ci";
            buildInputs = with pkgs; [
              go
              gotestsum
            ];
            GOFLAGS = "-mod=vendor";
            shellHook = ''
              ln -sfn ${workspaceVendor} vendor
            '';
          };
        };
    };
}
