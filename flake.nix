{
  description = "Wallpaper changer with Windows Spotlight, Bing and wallhaven images, for hyprpaper and other setters";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
    in
    {
      packages = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.callPackage ./nix/package.nix { };
      });

      overlays.default = final: _prev: {
        hyprautopaper = final.callPackage ./nix/package.nix { };
      };

      # Builds the package with the user's own nixpkgs, so this flake's nixpkgs
      # input is never evaluated by consumers.
      homeManagerModules.default = ./nix/hm-module.nix;

      devShells = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.mkShell {
          packages = [ nixpkgs.legacyPackages.${system}.go ];
        };
      });
    };
}
