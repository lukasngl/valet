# Go build helpers for the workspace.
#
# Contributes to perSystem valet.lib: version, vendorHash, mkGoModule,
# workspaceVendor, withPackageEnv, envtestBinaries.
{ config, inputs, ... }:
let
  # Project version, read from version.txt at the repo root.
  version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ../version.txt);

  # Override a Go package derivation to run a check instead of producing a binary.
  # Reuses the package's build environment (vendor, source, nativeBuildInputs)
  # but replaces the build and install phases.
  #
  # basePackage -> { name, buildPhase, extraBuildInputs? } -> derivation
  withPackageEnv =
    basePackage:
    {
      name,
      buildPhase,
      extraBuildInputs ? [ ],
      installPhase ? "touch $out",
    }:
    basePackage.overrideAttrs (old: {
      inherit name buildPhase installPhase;
      nativeBuildInputs = old.nativeBuildInputs ++ extraBuildInputs;
      doCheck = false;
    });
in
{
  perSystem =
    { pkgs, ... }:
    let
      # Shared workspace vendor directory built from go.work.
      # All mkGoModule derivations share this single vendor.
      workspaceVendor =
        (pkgs.buildGoModule {
          pname = "workspace";
          inherit version;
          inherit (config.valet.lib) vendorHash;
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

      # Build a Go binary from the workspace using the shared vendor.
      #
      # { pname, subPackages, tags?, ldflags?, ... } -> derivation
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

      # etcd + kube-apiserver for envtest. Set KUBEBUILDER_ASSETS to this.
      envtestBinaries = pkgs.linkFarm "envtest-binaries" {
        etcd = "${pkgs.etcd}/bin/etcd";
        kube-apiserver = "${pkgs.kubernetes}/bin/kube-apiserver";
      };
    in
    {
      valet.lib = {
        inherit
          version
          withPackageEnv
          mkGoModule
          workspaceVendor
          envtestBinaries
          ;
      };
    };
}
