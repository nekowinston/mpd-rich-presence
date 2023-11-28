{pkgs ? import <nixpkgs> {}}: rec {
  mpd-rich-presence = pkgs.buildGoModule rec {
    name = "mpd-rich-presence";
    version = "0.8.0";
    src = ./.;
    vendorHash = "sha256-36t7SnTB4zrlwphNcdtuzzQ7TUxUGtxoMmntbEENXmc=";
    ldflags = ["-s" "-w" "-X=main.version=${version}" "-X=main.builtBy=nixpkgs"];
    meta = {
      description = "Discord Rich Presence for MPD";
      homepage = "https://github.com/nekowinston/mpd-rich-presence";
      license = pkgs.lib.licenses.mit;
      platforms = pkgs.lib.platforms.unix;
    };
  };
  default = mpd-rich-presence;
}
