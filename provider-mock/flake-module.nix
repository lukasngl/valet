{ inputs, ... }:
{
  perSystem =
    {
      pkgs,
      mkGoModule,
      withPackageEnv,
      envtest-binaries,
      version,
      ...
    }:
    let
      provider-mock = mkGoModule {
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
        tag = version;
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

      checks.provider-mock-helm-lint =
        pkgs.runCommand "provider-mock-helm-lint"
          {
            nativeBuildInputs = [ pkgs.kubernetes-helm ];
          }
          ''
            helm lint ${inputs.self}/provider-mock/charts/provider-mock
            touch $out
          '';

      checks.provider-mock-lint = withPackageEnv provider-mock {
        name = "provider-mock-lint";
        extraBuildInputs = [ pkgs.golangci-lint ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          golangci-lint run --timeout 10m ./provider-mock/...
        '';
      };

      checks.provider-mock-test = withPackageEnv provider-mock {
        name = "provider-mock-test";
        extraBuildInputs = [
          pkgs.gotestsum
          pkgs.etcd
          pkgs.kubernetes
        ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          export KUBEBUILDER_ASSETS=${envtest-binaries}
          gotestsum --format short-verbose -- -short ./provider-mock/...
        '';
      };
    };
}
