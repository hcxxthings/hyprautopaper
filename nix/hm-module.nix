{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.hyprautopaper;
  json = pkgs.formats.json { };
  configFile = json.generate "hyprautopaper-config.json" cfg.settings;
in
{
  options.services.hyprautopaper = {
    enable = lib.mkEnableOption "hyprautopaper, a wallpaper changer for hyprpaper";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix { }";
      description = "The hyprautopaper package.";
    };

    hyprctlPackage = lib.mkOption {
      type = lib.types.package;
      default = pkgs.hyprland;
      defaultText = lib.literalExpression "pkgs.hyprland";
      description = ''
        Package that provides `hyprctl`. Use the same Hyprland you run,
        e.g. the one from the Hyprland flake.
      '';
    };

    extraPackages = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      example = lib.literalExpression "[ pkgs.awww ]";
      description = ''
        Packages on the service's PATH besides hyprctl, e.g. the program
        that `settings.command` runs with `settings.backend = "command"`.
      '';
    };

    settings = lib.mkOption {
      type = json.type;
      default = { };
      example = lib.literalExpression ''
        {
          sources = [ "spotlight" "bing" "wallhaven" ];
          interval = "2h";
          history = 30;
          save_dir = "~/Pictures/Wallpapers";
          notify = true;
          wallhaven = {
            query = "nature";
            at_least = "3840x2160";
          };
        }
      '';
      description = ''
        Written to {file}`$XDG_CONFIG_HOME/hyprautopaper/config.json`.
        Every key is optional; see the README for the full list.
        With the default backend hyprpaper must run with `ipc = true` (its default).
      '';
    };

    systemdTarget = lib.mkOption {
      type = lib.types.str;
      default = config.wayland.systemd.target;
      defaultText = lib.literalExpression "config.wayland.systemd.target";
      description = "Systemd target that starts and stops the service.";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile."hyprautopaper/config.json".source = configFile;

    systemd.user.services.hyprautopaper = {
      Unit = {
        Description = "hyprautopaper wallpaper changer";
        PartOf = [ cfg.systemdTarget ];
        After = [
          cfg.systemdTarget
          "hyprpaper.service"
        ];
        ConditionEnvironment = "WAYLAND_DISPLAY";
        X-Restart-Triggers = [ "${configFile}" ];
      };
      Service = {
        ExecStart = "${lib.getExe cfg.package} daemon";
        Environment = [ "PATH=${lib.makeBinPath ([ cfg.hyprctlPackage ] ++ cfg.extraPackages)}" ];
        Restart = "on-failure";
        RestartSec = 10;
        # Exit status 2 is a config error; restarting would only repeat it.
        RestartPreventExitStatus = 2;
      };
      Install.WantedBy = [ cfg.systemdTarget ];
    };
  };
}
