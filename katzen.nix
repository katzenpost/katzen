{ src ? ./., version ? "unstable" }:
{ lib, buildGoModule, fetchFromGitHub, copyDesktopItems, makeDesktopItem
, pkg-config, librsvg, libGL, libX11, libXfixes, libxkbcommon, vulkan-headers
, wayland, xorg }:

buildGoModule rec {
  pname = "katzen";
  inherit src version;

  vendorHash = "sha256-bUap2v9fXlrVWbzTe2BRQ1T7K3OhfoMKo6iC7+qliF0=";
  # This hash is may drift from the actual vendoring and break the build,
  # see https://nixos.org/manual/nixpkgs/unstable/#ssec-language-go.

  nativeBuildInputs = [ pkg-config copyDesktopItems librsvg ];
  buildInputs = [
    libGL
    libX11
    libXfixes
    libxkbcommon
    vulkan-headers
    wayland
    xorg.libXcursor
  ];

  CGO_CFLAGS_ALLOW = "-DPARAMS=sphincs-shake-256f";

  postInstall = ''
    ## Both of the configs are embedded in the binary; "without_tor" being the default.
    ## Another config may be specified by using '-f' flag.
    install -D -t $out/share/ ./default_config_without_tor.toml
    install -D -t $out/share/ ./default_config_with_tor.toml

    install -D \
      ./assets/katzenpost_logo.svg \
      $out/share/icons/hicolor/scalable/apps/${pname}.svg

    mkdir -p $out/share/icons/hicolor/48x48/apps
    rsvg-convert \
      --output $out/share/icons/hicolor/48x48/apps/${pname}.png \
      --width 48 --height 48 \
      ./assets/katzenpost_logo.svg
  '';

  desktopItems = [
    (makeDesktopItem {
      name = pname;
      exec = pname;
      icon = pname;
      desktopName = "Katzen";
      genericName = meta.description;
      categories = [ "Chat" "Network" ];
    })
  ];

  meta = with lib; {
    description = "Traffic analysis resistant messaging";
    homepage = "https://katzenpost.mixnetworks.org/";
    license = licenses.agpl3;
    maintainers = with maintainers; [ ehmry ];
  };
}
