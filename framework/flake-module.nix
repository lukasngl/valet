{
  perSystem =
    {
      pkgs,
      self',
      withPackageEnv,
      ...
    }:
    {
      checks.framework-test = withPackageEnv self'.packages.provider-mock {
        name = "framework-test";
        extraBuildInputs = [ pkgs.gotestsum ];
        buildPhase = ''
          export HOME=$(mktemp -d)
          gotestsum --format short-verbose -- -short ./framework/...
        '';
      };
    };
}
