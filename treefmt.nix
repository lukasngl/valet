{ ... }: {
  projectRootFile = ".git/config";
  programs.nixpkgs-fmt.enable = true;
  programs.gofumpt.enable = true;
}
