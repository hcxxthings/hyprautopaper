package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is read from $XDG_CONFIG_HOME/hyprautopaper/config.json. Every field is optional.
type Config struct {
	// Sources to pick new images from; one is chosen at random each time.
	Sources []string `json:"sources"`
	// Time between automatic changes, e.g. "30m", "2h", "1d", or "off".
	Interval Duration `json:"interval"`
	// Fetch a new image whenever the daemon starts, instead of showing the last one.
	ChangeOnStart bool `json:"change_on_start"`
	// How many recent wallpapers stay on disk and can be reached with "prev".
	History int `json:"history"`
	// Directory that "hyprautopaper save" copies the current wallpaper to. It must exist.
	SaveDir string `json:"save_dir"`
	// Show a desktop notification for next, prev and save, errors included.
	Notify bool `json:"notify"`

	// What puts the image on screen: "hyprpaper" or "command".
	Backend string `json:"backend"`
	// hyprpaper fit_mode: contain, cover, tile or fill.
	FitMode string `json:"fit_mode"`
	// For backend "command": a program and its arguments, with "{path}"
	// replaced by the image file, e.g. ["awww", "img", "{path}"]. It must exit.
	Command []string `json:"command"`

	// Market/locale for Bing and Spotlight, e.g. "en-US", "de-DE".
	Market string `json:"market"`
	// Where downloaded images are kept.
	CacheDir string `json:"cache_dir"`

	Wallhaven WallhavenConfig `json:"wallhaven"`
}

type WallhavenConfig struct {
	// Search query; empty means the toplist.
	Query string `json:"query"`
	// Three bits: general, anime, people. "100" is general only.
	Categories string `json:"categories"`
	// Toplist range: 1d, 3d, 1w, 1M, 3M, 6M, 1y.
	TopRange string `json:"top_range"`
	// Minimum resolution, e.g. "2560x1440".
	AtLeast string `json:"at_least"`
	// Comma separated aspect ratios, e.g. "16x9,16x10".
	Ratios string `json:"ratios"`
	// File that contains a wallhaven API key (optional, not needed for SFW images).
	APIKeyFile string `json:"api_key_file"`
}

var validSources = []string{"spotlight", "bing", "wallhaven"}

func defaultConfig() Config {
	return Config{
		Sources:  []string{"spotlight", "bing"},
		Interval: Duration(time.Hour),
		History:  30,
		Backend:  "hyprpaper",
		FitMode:  "cover",
		Market:   "en-US",
		CacheDir: filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), "hyprautopaper"),
		Wallhaven: WallhavenConfig{
			Categories: "100",
			TopRange:   "1M",
			AtLeast:    "2560x1440",
			Ratios:     "16x9,16x10",
		},
	}
}

func defaultConfigPath() string {
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "hyprautopaper", "config.json")
}

// loadConfig reads path on top of the defaults. A missing file is not an error.
func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}
	// hyprctl resolves relative paths against its own working directory.
	if cfg.CacheDir, err = filepath.Abs(expandHome(cfg.CacheDir)); err != nil {
		return cfg, err
	}
	if cfg.SaveDir != "" {
		if cfg.SaveDir, err = filepath.Abs(expandHome(cfg.SaveDir)); err != nil {
			return cfg, err
		}
	}
	cfg.Wallhaven.APIKeyFile = expandHome(cfg.Wallhaven.APIKeyFile)
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if len(c.Sources) == 0 {
		return errors.New("sources must not be empty")
	}
	for _, s := range c.Sources {
		if !slices.Contains(validSources, s) {
			return fmt.Errorf("unknown source %q (valid: %s)", s, strings.Join(validSources, ", "))
		}
	}
	// 0 means "off".
	if c.Interval != 0 && c.Interval < Duration(time.Minute) {
		return errors.New(`interval must be at least 1m, or "off"`)
	}
	if c.History < 1 {
		return errors.New("history must be at least 1")
	}
	switch c.Backend {
	case "hyprpaper":
		if !slices.Contains([]string{"contain", "cover", "tile", "fill"}, c.FitMode) {
			return fmt.Errorf("unknown fit_mode %q (valid: contain, cover, tile, fill)", c.FitMode)
		}
		// hyprctl splits its argument on commas.
		if strings.Contains(c.CacheDir, ",") {
			return errors.New("cache_dir must not contain a comma with the hyprpaper backend")
		}
	case "command":
		if !slices.ContainsFunc(c.Command, func(arg string) bool { return strings.Contains(arg, "{path}") }) {
			return errors.New(`backend "command" needs a "command" with "{path}" in one of its arguments`)
		}
	default:
		return fmt.Errorf("unknown backend %q (valid: hyprpaper, command)", c.Backend)
	}
	return nil
}

// Duration accepts Go durations ("90m", "2h"), whole days ("1d") and "off" (0).
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf(`interval must be a string like "1h": %w`, err)
	}
	v, err := parseDuration(s)
	*d = Duration(v)
	return err
}

func (d Duration) String() string {
	if d == 0 {
		return "off"
	}
	return time.Duration(d).String()
}

func parseDuration(s string) (time.Duration, error) {
	if s == "off" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid interval %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

func xdgDir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return filepath.Join(homeDir(), fallback)
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(homeDir(), rest)
	}
	return p
}
