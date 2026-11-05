# hyprautopaper

Fresh wallpapers from the same curated photo feeds as Windows and Bing,
with a history you can step through.

| source      | what                                     | resolution   |
|-------------|------------------------------------------|--------------|
| `spotlight` | Windows 11 desktop Spotlight images      | 3840x2160    |
| `bing`      | Bing image of the day (last ~15 days)    | UHD          |
| `wallhaven` | wallhaven.cc toplist or search, SFW only | configurable |

No API keys needed. Recently shown images are not repeated.
The images are shown by [hyprpaper](https://wiki.hypr.land/hypr-ecosystem/user/hyprpaper/)
over its IPC, or by any other wallpaper setter (see [Backends](#backends)).

## Commands

```
hyprautopaper next [SOURCE]   next wallpaper: forward in the history, or a new image
hyprautopaper prev            previous wallpaper
hyprautopaper save            copy the current wallpaper to save_dir
hyprautopaper info [-json]    title, copyright, link and file of the current one
hyprautopaper reapply         show the current wallpaper again
hyprautopaper daemon          change every "interval" (run as a user service)
```

`prev` and `next` move through the last `history` wallpapers like the back and
forward buttons of a browser; `next` at the newest one fetches a new image.
`next SOURCE` always fetches a new image from that source.
A manual change restarts the interval.

The daemon checks the wall clock every minute, so suspend and reboots count
towards the interval. Under Hyprland it also puts the wallpaper back when a
monitor is connected again.

### Key bindings

Hyprland (Lua config):

```lua
hl.bind("SUPER + ALT + P", hl.dsp.exec_cmd("hyprautopaper prev"), { description = "Previous wallpaper" })
hl.bind("SUPER + ALT + N", hl.dsp.exec_cmd("hyprautopaper next"), { description = "Next wallpaper" })
hl.bind("SUPER + ALT + S", hl.dsp.exec_cmd("hyprautopaper save"), { description = "Save wallpaper" })
```

Hyprland (hyprlang config):

```
bind = SUPER ALT, P, exec, hyprautopaper prev
bind = SUPER ALT, N, exec, hyprautopaper next
bind = SUPER ALT, S, exec, hyprautopaper save
```

Set `"notify": true` to see what happened (or why it failed) when a command runs from a key binding.

## Configuration

`$XDG_CONFIG_HOME/hyprautopaper/config.json`. Every key is optional, these are the defaults:

```json
{
  "sources": ["spotlight", "bing"],
  "interval": "1h",
  "change_on_start": false,
  "history": 30,
  "save_dir": "",
  "notify": false,
  "backend": "hyprpaper",
  "fit_mode": "cover",
  "command": [],
  "market": "en-US",
  "cache_dir": "~/.cache/hyprautopaper",
  "wallhaven": {
    "query": "",
    "categories": "100",
    "top_range": "1M",
    "at_least": "2560x1440",
    "ratios": "16x9,16x10",
    "api_key_file": ""
  }
}
```

| key               | meaning |
|-------------------|---------|
| `sources`         | where new images come from; one is picked at random each time |
| `interval`        | time between automatic changes: `30m`, `2h`, `1d`, or `off` for manual changes only |
| `change_on_start` | fetch a new image every time the daemon starts, instead of showing the last one |
| `history`         | how many recent wallpapers stay on disk for `prev` |
| `save_dir`        | where `save` copies the current wallpaper. Must exist: `save` fails with the reason when it is not set or missing, instead of creating a directory from a typo |
| `notify`          | desktop notification (`notify-send`) for `next`, `prev` and `save`, errors included |
| `backend`         | `hyprpaper` or `command` |
| `fit_mode`        | hyprpaper only: `contain`, `cover`, `tile` or `fill` |
| `command`         | for `backend: "command"`: the setter, `{path}` is replaced with the image |
| `market`          | language and region of Bing and Spotlight, e.g. `de-DE` |
| `wallhaven`       | search options; `categories` is general/anime/people as three bits |

## Backends

`hyprpaper` (default) needs `ipc = true` in `hyprpaper.conf` (its default).

`command` runs any program that sets a wallpaper and exits. No shell is
involved, so `{path}` needs no quoting:

```json
{ "backend": "command", "command": ["awww", "img", "--transition-type", "fade", "{path}"] }
{ "backend": "command", "command": ["plasma-apply-wallpaperimage", "{path}"] }
{ "backend": "command", "command": ["sh", "-c", "gsettings set org.gnome.desktop.background picture-uri \"file://$1\" && gsettings set org.gnome.desktop.background picture-uri-dark \"file://$1\"", "sh", "{path}"] }
```

[awww](https://codeberg.org/LGFae/awww) (formerly swww) adds animated
transitions. A setter that keeps running, like swaybg, needs a small
wrapper that replaces the old process and starts it in the background with
its output redirected.

## Installation

### NixOS / home-manager with flakes

```nix
# flake.nix
inputs.hyprautopaper = {
  url = "github:hcxxthings/hyprautopaper";
  inputs.nixpkgs.follows = "nixpkgs";
};

# home-manager
imports = [ inputs.hyprautopaper.homeManagerModules.default ];
services.hyprautopaper = {
  enable = true;
  # hyprctl from the Hyprland you actually run
  hyprctlPackage = inputs.hyprland.packages.${pkgs.stdenv.hostPlatform.system}.hyprland;
  settings = {
    interval = "2h";
    save_dir = "~/Pictures/Wallpapers";
    notify = true;
  };
};
```

The module builds hyprautopaper with your own nixpkgs and runs
`hyprautopaper daemon` as a user service. With `backend = "command"`, put the
setter's package in `services.hyprautopaper.extraPackages`.

### Without flakes (channels, npins, niv)

```nix
let
  src = fetchTarball "https://github.com/hcxxthings/hyprautopaper/archive/v0.1.0.tar.gz";
in
{
  imports = [ "${src}/nix/hm-module.nix" ];
  services.hyprautopaper.enable = true;
}
```

### Other distributions

```sh
go install github.com/hcxxthings/hyprautopaper@latest
```

Run `hyprautopaper daemon` from a systemd user service or `exec-once`. It
needs `hyprctl` (or your setter) and, for `notify`, `notify-send` in `PATH`.

## Notes

The Spotlight and Bing feeds are not official public APIs; they are the ones
Windows and bing.com use and may change. The photos belong to their authors
(see `hyprautopaper info`); hyprautopaper only downloads them for your own
desktop.
