{
  description = "Katzenpost development flake";

  outputs = { self, nixpkgs }:
    let inherit (nixpkgs) lib;
    in {

      overlays.default = final: prev: {
        katzen = final.callPackage (import ./katzen.nix {
          src = self;
          version = "unstable-${self.lastModifiedDate}";
        }) { };
      };

      legacyPackages = lib.attrsets.mapAttrs (system: pkgs:
        pkgs.appendOverlays (builtins.attrValues self.overlays)) {
          inherit (nixpkgs.legacyPackages)
            i686-linux x86_64-linux aarch64-linux;
        };

      packages = lib.attrsets.mapAttrs (system: pkgs: rec {
        inherit (pkgs) katzen;
        default = katzen;
      }) self.legacyPackages;

      checks = self.packages;
    };

}
