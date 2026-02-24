# Development shells for local work and CI.
#
# default: full toolchain for local development (go, just, helm, etc.)
# ci:      minimal shell for CI pipelines with pre-linked workspace vendor
{ inputs, ... }:
{
  perSystem =
    {
      config,
      pkgs,
      inputs',
      ...
    }:
    let
      valet = config.valet.lib;
    in
    {
      devShells.default = pkgs.mkShell {
        hardeningDisable = [ "fortify" ];
        name = "valet";
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
        KUBEBUILDER_ASSETS = "${valet.envtestBinaries}";
      };

      devShells.ci = pkgs.mkShell {
        name = "valet-ci";
        buildInputs = with pkgs; [
          go
          gotestsum
        ];
        GOFLAGS = "-mod=vendor";
        KUBEBUILDER_ASSETS = "${valet.envtestBinaries}";
        shellHook = ''
          if [ -n "''${CI:-}" ]; then
            ln -sfn ${valet.workspaceVendor} vendor
          fi
        '';
      };
    };
}
