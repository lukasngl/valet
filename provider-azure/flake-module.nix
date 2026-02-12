{ inputs, ... }:
{
  perSystem =
    {
      pkgs,
      mkGoModule,
      withPackageEnv,
      version,
      ...
    }:
    let
      provider-azure = mkGoModule {
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
        tag = version;
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

      checks.provider-azure-helm-lint =
        pkgs.runCommand "provider-azure-helm-lint"
          {
            nativeBuildInputs = with pkgs; [
              kubernetes-helm
              kubeconform
            ];
          }
          ''
            chart=${inputs.self}/provider-azure/charts/provider-azure
            helm lint "$chart"
            helm template test "$chart" -f "$chart/values.kubeconform.yaml" \
              | kubeconform -strict -summary
            touch $out
          '';

      checks.provider-azure-lint = withPackageEnv provider-azure {
        name = "provider-azure-lint";
        extraBuildInputs = [ pkgs.golangci-lint ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          golangci-lint run --timeout 10m ./provider-azure/...
        '';
      };
    };
}
