{ inputs, ... }:
{
  perSystem =
    { config, pkgs, ... }:
    let
      valet = config.valet.lib;

      provider-azure = valet.mkGoModule {
        pname = "provider-azure";
        subPackages = [ "provider-azure/cmd" ];
        postInstall = ''
          mv $out/bin/cmd $out/bin/provider-azure
        '';
        meta.mainProgram = "provider-azure";
      };

      provider-azure-compressed = pkgs.stdenvNoCC.mkDerivation {
        inherit (provider-azure) pname version meta;
        dontUnpack = true;
        nativeBuildInputs = [ pkgs.upx ];
        buildPhase = ''
          mkdir -p $out/bin
          upx -o $out/bin/provider-azure ${provider-azure}/bin/provider-azure
        '';
      };

      image = pkgs.dockerTools.streamLayeredImage {
        name = "provider-azure";
        tag = valet.version;
        contents = [ pkgs.dockerTools.caCertificates ];
        config = {
          Entrypoint = [ "${provider-azure-compressed}/bin/provider-azure" ];
          User = "65532:65532";
          WorkingDir = "/";
        };
      };
    in
    {
      packages = {
        inherit provider-azure provider-azure-compressed;
        provider-azure-image = image;
      };

      checks.provider-azure-helm = valet.packageChart {
        name = "provider-azure";
        src = "${inputs.self}/provider-azure/charts/provider-azure";
      };

      checks.provider-azure-lint = valet.withPackageEnv provider-azure {
        name = "provider-azure-lint";
        extraBuildInputs = [ pkgs.golangci-lint ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          golangci-lint run --timeout 10m ./provider-azure/...
        '';
      };

      checks.provider-azure-test = valet.withPackageEnv provider-azure {
        name = "provider-azure-test";
        extraBuildInputs = [
          pkgs.gotestsum
          pkgs.etcd
          pkgs.kubernetes
        ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          export KUBEBUILDER_ASSETS=${valet.envtestBinaries}
          gotestsum --format short-verbose -- -short -coverpkg=github.com/lukasngl/valet/framework/...,./... -coverprofile=coverage.txt ./provider-azure/...
        '';
        installPhase = ''
          mkdir -p $out
          cp coverage.txt $out/
        '';
      };
    };
}
