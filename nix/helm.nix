# Helm chart packaging and validation.
#
# Contributes to perSystem valet.lib: packageChart.
#
# The derivation packages the chart (.tgz) in the build phase and runs
# helm lint + kubeconform validation in the check phase.
{ ... }:
{
  perSystem =
    { pkgs, lib, ... }:
    {
      valet.lib =
        let
          packageChart =
            {
              name,
              src,
              valuesFile ? "values.kubeconform.yaml",
            }:
            pkgs.stdenvNoCC.mkDerivation {
              name = "${name}-chart";
              inherit src;
              dontUnpack = true;
              nativeBuildInputs = with pkgs; [
                kubernetes-helm
                kubeconform
              ];
              doCheck = true;
              buildPhase = ''
                helm package ${src} -d $TMPDIR
              '';
              checkPhase = ''
                helm lint ${src}
                helm template test ${src} -f ${src}/${valuesFile} \
                  | kubeconform -strict -summary \
                      -schema-location '${kubernetesSchemas}/{{.ResourceKind}}{{.KindSuffix}}.json'
              '';
              installPhase = ''
                mkdir -p $out
                cp $TMPDIR/*.tgz $out/
              '';
            };

          # Fixed-output derivation: pre-fetches K8s resource schemas for offline
          # validation in the Nix sandbox. Pinned to a released K8s version so the
          # hash stays stable.
          kubernetesSchemas = pkgs.stdenvNoCC.mkDerivation {
            name = "kubernetes-json-schemas";
            dontUnpack = true;
            outputHashAlgo = "sha256";
            outputHashMode = "recursive";
            outputHash = "sha256-4Ds3c2yO5MIA5lX0DCecFljilyXpsWZoh6gdyc7R/7I=";
            nativeBuildInputs = [ pkgs.cacert ];
            buildPhase =
              let
                base = "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/v1.32.0-standalone-strict";
                schemas = [
                  "deployment-apps-v1"
                  "serviceaccount-v1"
                  "clusterrole-rbac-v1"
                  "clusterrolebinding-rbac-v1"
                  "role-rbac-v1"
                  "rolebinding-rbac-v1"
                ];
              in
              ''
                mkdir -p $out
                ${lib.concatMapStringsSep "\n" (
                  s: "${lib.getExe pkgs.curl} -sLo $out/${s}.json ${base}/${s}.json"
                ) schemas}
              '';
            installPhase = "true";
          };
        in
        {
          inherit packageChart;
        };
    };
}
