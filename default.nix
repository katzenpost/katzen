{ pkgs ? import <nixpkgs> { } }: pkgs.callPackage (import ./katzen.nix { }) { }
