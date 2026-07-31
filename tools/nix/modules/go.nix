# This function returns a list of `devenv` modules
# which are passed to `mkShell`.
#
# Search for package at:
# https://search.nixos.org/packages
{ pkgs, ... }:
[
  {
    name = "go";
    packages = [ pkgs.gopls ];
    languages.go.enable = true;
  }
]
