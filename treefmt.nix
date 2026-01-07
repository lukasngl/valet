{ pkgs, ... }:
{
  projectRootFile = ".git/config";
  programs.nixfmt.enable = true;
  programs.nixfmt.package = pkgs.nixfmt-rfc-style;
  programs.gofumpt.enable = true;
}
