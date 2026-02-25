{ inputs, ... }:
{
  perSystem =
    { config, pkgs, ... }:
    let
      valet = config.valet.lib;

      provider-mock = valet.mkGoModule {
        pname = "provider-mock";
        subPackages = [ "provider-mock/cmd" ];
        postInstall = ''
          mv $out/bin/cmd $out/bin/provider-mock
        '';
        meta.mainProgram = "provider-mock";
      };

      provider-mock-compressed = pkgs.stdenvNoCC.mkDerivation {
        inherit (provider-mock) pname version meta;
        dontUnpack = true;
        nativeBuildInputs = [ pkgs.upx ];
        buildPhase = ''
          mkdir -p $out/bin
          upx -o $out/bin/provider-mock ${provider-mock}/bin/provider-mock
        '';
      };

      image = pkgs.dockerTools.streamLayeredImage {
        name = "provider-mock";
        tag = valet.version;
        contents = [ pkgs.dockerTools.caCertificates ];
        config = {
          Entrypoint = [ "${provider-mock-compressed}/bin/provider-mock" ];
          User = "65532:65532";
          WorkingDir = "/";
        };
      };
    in
    {
      packages = {
        inherit provider-mock provider-mock-compressed;
        provider-mock-image = image;
      };

      checks.provider-mock-helm = valet.packageChart {
        name = "provider-mock";
        src = "${inputs.self}/provider-mock/charts/provider-mock";
      };

      checks.provider-mock-lint = valet.withPackageEnv provider-mock {
        name = "provider-mock-lint";
        extraBuildInputs = [ pkgs.golangci-lint ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          golangci-lint run --timeout 10m ./provider-mock/...
        '';
      };

      checks.provider-mock-test = valet.withPackageEnv provider-mock {
        name = "provider-mock-test";
        extraBuildInputs = [
          pkgs.gotestsum
          pkgs.etcd
          pkgs.kubernetes
        ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          export KUBEBUILDER_ASSETS=${valet.envtestBinaries}
          gotestsum --format short-verbose -- -coverpkg=github.com/lukasngl/valet/framework/...,./... -coverprofile=coverage.txt ./provider-mock/...
        '';
        installPhase = ''
          mkdir -p $out
          cp coverage.txt $out/
        '';
      };
    };
}
