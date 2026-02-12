{ ... }:
{
  perSystem =
    {
      config,
      pkgs,
      self',
      ...
    }:
    let
      valet = config.valet.lib;
    in
    {
      checks.framework-test = valet.withPackageEnv self'.packages.provider-mock {
        name = "framework-test";
        extraBuildInputs = [ pkgs.gotestsum ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          gotestsum --format short-verbose -- -short ./framework/...
        '';
      };
    };
}
