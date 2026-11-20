# For users without flakes: nix-build, or
#   hyprautopaper = pkgs.callPackage (fetchTarball "https://github.com/hcxxthings/hyprautopaper/archive/v0.3.0.tar.gz") { };
{
  pkgs ? import <nixpkgs> { },
}:

pkgs.callPackage ./nix/package.nix { }
