{ inputs, ... }:
{
  imports = [ inputs.treefmt-nix.flakeModule ];

  perSystem =
    { pkgs, self', ... }:
    {
      treefmt = {
        projectRootFile = ".git/config";

        # Nix
        programs.nixfmt.enable = true;
        programs.nixfmt.package = pkgs.nixfmt-rfc-style;

        # Go
        programs.gofumpt.enable = true;
        programs.goimports.enable = true;
        programs.golines.enable = true;
        settings.formatter.modfmt = {
          command = pkgs.writeShellScriptBin "modfmt" ''
            for f in "$@"; do
              sh -c "cd $(dirname $f) && ${pkgs.lib.getExe self'.packages.modfmt}"
            done
          '';
          includes = [ "go.mod" ];
        };

        # Shell
        programs.shfmt.enable = true;
        programs.shellcheck.enable = true;
        settings.formatter.shellcheck.priority = -1; # run after shfmt

        # GitHub Actions
        programs.actionlint.enable = true;

        # Misc
        programs.dprint.enable = true;
        settings.formatter.dprint.options = [ "--allow-no-files" ];
        programs.dprint.settings.excludes = [
          "**/templates/**/*.yaml"
        ];
        programs.dprint.settings.plugins = (
          pkgs.dprint-plugins.getPluginList (
            plugins: with plugins; [
              dprint-plugin-markdown
              dprint-plugin-json
              g-plane-pretty_yaml
            ]
          )
        );
      };

      packages = {
        modfmt = pkgs.buildGoModule {
          pname = "modfmt";
          version = "0.1.0";
          src = pkgs.fetchFromGitHub {
            # https://github.com/abhijit-hota/modfmt/commit/
            owner = "abhijit-hota";
            repo = "modfmt";
            rev = "287023c3d9051b6d94b8267b0b48f6a8d33b1b0d";
            hash = "sha256-DBFKHmO6hmnqFseuJZ9WTRi8jglq4UZJ+hrsFU16888=";
          };
          vendorHash = "sha256-q1fWnKhWwwd7V64avX0/G167HemGthlIb3ETc3Vv/os=";
          meta.mainProgram = "modfmt";
        };
      };
    };
}
