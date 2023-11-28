{
  description = "mpd-rich-presence";
  outputs = {
    self,
    nixpkgs,
  }: let
    systems = ["x86_64-linux" "x86_64-darwin" "aarch64-darwin" "aarch64-linux"];
    eachSystem = fn: nixpkgs.lib.genAttrs systems (system: fn nixpkgs.legacyPackages.${system});
  in rec {
    packages = eachSystem (pkgs: import ./nix {inherit pkgs;});

    homeManagerModules.mpd-rich-presence = {
      config,
      pkgs,
      ...
    }: let
      cfg = config.services.mpd-rich-presence;
      inherit (pkgs) lib;
    in {
      options.services.mpd-rich-presence = {
        enable = lib.mkEnableOption "mpd-rich-presence";
        package = lib.mkOption {
          type = lib.types.package;
          default = pkgs.callPackage ./nix {};
          description = "The mpd-rich-presence package to use.";
        };
      };
      config = lib.mkIf cfg.enable {
        systemd.user.services.mpd-rich-presence = {
          Unit = {
            Description = "Discord Rich Presence for MPD";
            Requires = ["mpd.service"];
            After = ["mpd.service"];
          };
          Service = {
            ExecStart = "${lib.getExe pkgs.hello}";
            Restart = "on-failure";
          };
          Install = {
            WantedBy = ["default.target"];
          };
        };
      };
    };

    devShells = eachSystem (pkgs: {
      default = pkgs.mkShell {
        buildInputs = with pkgs; [go gofumpt goreleaser];
      };
    });
  };
}
