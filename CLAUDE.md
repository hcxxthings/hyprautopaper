# hyprautopaper

Go CLI and daemon that downloads wallpapers (Windows Spotlight, Bing image of the
day, wallhaven) and shows them through hyprpaper's IPC or a user-supplied command.
Users install it mostly through the flake's home-manager module.

## Layout
- `main.go`: CLI commands and the daemon loop.
- `hyprautopaper.go`: changing wallpapers (history with prev/next, new images, save).
- `state.go`: state file, history, lock, atomic writes.
- `sources.go`: the image sources. `download.go`: downloads and the cache.
- `backend.go`: hyprpaper or command backend. `hyprland.go`: hyprctl and the event socket.
- `notify.go`: desktop notifications through notify-send.
- `nix/package.nix`, `nix/hm-module.nix`, `flake.nix`, `default.nix` (non-flake users).

## Build and test
- `nix develop -c go test ./...`, `nix build`, `nix flake check`.
- Standard library only (`vendorHash = null`); keep it that way.
- Keep `README.md` and `nix/hm-module.nix` in sync with `Config` in `config.go`.

## Facts checked against upstream sources (do not re-guess)
- `hyprctl hyprpaper wallpaper 'MON,PATH,FIT'` (Hyprland `hyprctl/src/hyprpaper/Hyprpaper.cpp`):
  prints nothing on success, `error: ...` and exit status 1 on failure;
  waits for hyprpaper's answer with no timeout; splits on commas;
  FIT is `contain`, `tile`, `fit`/`stretch`, anything else silently becomes cover
  (so the config's `fill` is sent as `stretch`); empty MON is the fallback for
  monitors that never had a specific target; an unknown MON fails with "Invalid monitor".
- Hyprland event socket: `$XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock`,
  line `monitoradded>>NAME` (wiki.hypr.land/ipc).
- Spotlight: `fd.api.iris.microsoft.com/v4/api/selection?placement=88000820`, undocumented;
  `items[].item` is JSON in a string; the CDN serves JPEGs as `text/plain`, so formats are sniffed.
- Bing: `HPImageArchive.aspx` returns at most 8 images per request, idx up to 7 (~15 days);
  append `_UHD.jpg` to `urlbase`.
- wallhaven: SFW needs no API key; narrow searches can have fewer pages than requested.

## Conventions
- Code comments, docs and commit messages in English.
- Prefer small changes; ask before refactoring.
