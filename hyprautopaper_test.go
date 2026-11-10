package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30m": 30 * time.Minute,
		"2h":  2 * time.Hour,
		"1d":  24 * time.Hour,
		"off": 0,
	} {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseDuration("1.5d"); err == nil {
		t.Error(`parseDuration("1.5d") should fail`)
	}
}

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"interval": "off", "save_dir": "~/Saved", "wallhaven": {"query": "sea"}}`), 0o644)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 0 || cfg.SaveDir != filepath.Join(home, "Saved") {
		t.Errorf("interval %v, save_dir %q", cfg.Interval, cfg.SaveDir)
	}
	// Keys that are not set keep their defaults, also inside objects.
	if cfg.History != 30 || cfg.Wallhaven.Query != "sea" || cfg.Wallhaven.Categories != "100" {
		t.Errorf("defaults lost: %+v", cfg)
	}
	os.WriteFile(path, []byte(`{"keep": 3}`), 0o644)
	if _, err := loadConfig(path); err == nil {
		t.Error("unknown key should fail")
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]func(c *Config){
		"short interval":       func(c *Config) { c.Interval = Duration(30 * time.Second) },
		"unknown source":       func(c *Config) { c.Sources = []string{"flickr"} },
		"unknown backend":      func(c *Config) { c.Backend = "swaybg" },
		"unknown fit_mode":     func(c *Config) { c.FitMode = "stretch" },
		"command without path": func(c *Config) { c.Backend, c.Command = "command", []string{"awww", "img"} },
		"empty history":        func(c *Config) { c.History = 0 },
	}
	for name, change := range bad {
		c := defaultConfig()
		change(&c)
		if c.validate() == nil {
			t.Errorf("%s: no error", name)
		}
	}
	good := defaultConfig()
	good.Interval = 0
	good.Backend, good.Command = "command", []string{"awww", "img", "{path}"}
	if err := good.validate(); err != nil {
		t.Error(err)
	}
}

func TestImageExt(t *testing.T) {
	for want, head := range map[string][]byte{
		".jpg":  {0xff, 0xd8, 0xff, 0xe0},
		".png":  []byte("\x89PNG\r\n\x1a\n"),
		".webp": []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
		".jxl":  {0xff, 0x0a},
	} {
		if got, err := imageExt(head); err != nil || got != want {
			t.Errorf("imageExt(%s) = %q, %v", want, got, err)
		}
	}
	if _, err := imageExt([]byte("<html>")); err == nil {
		t.Error("imageExt should reject HTML")
	}
}

func TestStateAdd(t *testing.T) {
	var s State
	for _, id := range []string{"a", "b", "c", "b"} {
		s.add(Entry{Image: Image{ID: id}}, 2)
	}
	var ids []string
	for _, e := range s.History {
		ids = append(ids, e.ID)
	}
	// "a" fell out of the history of 2, "b" moved to the end instead of repeating.
	if !slices.Equal(ids, []string{"c", "b"}) || s.Cursor != 1 || !slices.Equal(s.Seen, []string{"a", "c", "b"}) {
		t.Errorf("history %v, cursor %d, seen %v", ids, s.Cursor, s.Seen)
	}
}

func TestCleanCacheKeepsHistoryAndForeignFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"bing_A.jpg", "bing_B.jpg", "holiday.jpg", "bing_notes.txt"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	cleanCache(dir, []Entry{{Path: filepath.Join(dir, "bing_A.jpg")}})
	for _, n := range []string{"bing_A.jpg", "holiday.jpg", "bing_notes.txt"} {
		if !fileExists(filepath.Join(dir, n)) {
			t.Errorf("%s was removed", n)
		}
	}
	if fileExists(filepath.Join(dir, "bing_B.jpg")) {
		t.Error("bing_B.jpg is not in the history but was kept")
	}
}

func TestIsDue(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bing_A.jpg")
	os.WriteFile(f, nil, 0o644)
	hist := []Entry{{Path: f}}
	hour := Duration(time.Hour)
	cases := []struct {
		name     string
		st       State
		interval Duration
		want     bool
	}{
		{"no wallpaper yet", State{Cursor: -1}, hour, true},
		{"fresh", State{LastChange: time.Now().Add(-time.Minute), History: hist}, hour, false},
		{"old", State{LastChange: time.Now().Add(-2 * time.Hour), History: hist}, hour, true},
		{"clock went back", State{LastChange: time.Now().Add(time.Hour), History: hist}, hour, true},
		{"file gone", State{LastChange: time.Now(), History: []Entry{{Path: f + ".gone"}}}, hour, true},
		{"off", State{LastChange: time.Now().Add(-48 * time.Hour), History: hist}, 0, false},
		{"off, nothing shown yet", State{Cursor: -1}, 0, true},
	}
	for _, c := range cases {
		if got := isDue(c.st, c.interval); got != c.want {
			t.Errorf("%s: isDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIPCFitMode(t *testing.T) {
	if ipcFitMode("fill") != "stretch" || ipcFitMode("cover") != "cover" {
		t.Error("fill must map to stretch, others unchanged")
	}
}

// commandConfig shows images with a shell command that writes the path to out.
func commandConfig(t *testing.T, cacheDir, out string) Config {
	t.Helper()
	cfg := defaultConfig()
	cfg.CacheDir = cacheDir
	cfg.Backend = "command"
	cfg.Command = []string{"/bin/sh", "-c", `printf %s "$1" > "$2"`, "sh", "{path}", out}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPrevNext(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	out := filepath.Join(dir, "shown")
	cfg := commandConfig(t, dir, out)
	var hist []Entry
	for _, id := range []string{"bing:A", "bing:B", "bing:C"} {
		p := filepath.Join(dir, cacheName(id)+".jpg")
		os.WriteFile(p, nil, 0o644)
		hist = append(hist, Entry{Image: Image{ID: id, Source: "bing"}, Path: p})
	}
	if err := saveState(State{History: hist, Cursor: 2}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	expect := func(to step, id string) {
		t.Helper()
		e, err := change(ctx, cfg, to, "", false)
		if err != nil {
			t.Fatal(err)
		}
		shown, _ := os.ReadFile(out)
		if e.ID != id || string(shown) != e.Path {
			t.Fatalf("got %s (command saw %q), want %s", e.ID, shown, id)
		}
	}
	expect(stepPrev, "bing:B")
	expect(stepPrev, "bing:A")
	if _, err := change(ctx, cfg, stepPrev, "", false); !errors.Is(err, errNoOlder) {
		t.Fatalf("prev at the oldest: %v", err)
	}
	expect(stepNext, "bing:B")
	expect(stepNext, "bing:C")
	// A manual change restarts the interval, so the daemon leaves it alone.
	if e, err := change(ctx, cfg, stepAuto, "", false); e != nil || err != nil {
		t.Errorf("auto change right after next: %v, %v", e, err)
	}
	for _, e := range hist {
		if !fileExists(e.Path) {
			t.Errorf("%s is in the history but was removed", e.Path)
		}
	}
}

func TestPin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	out := filepath.Join(dir, "shown")
	cfg := commandConfig(t, dir, out)
	var hist []Entry
	for _, id := range []string{"bing:A", "bing:B"} {
		p := filepath.Join(dir, cacheName(id)+".jpg")
		os.WriteFile(p, nil, 0o644)
		hist = append(hist, Entry{Image: Image{ID: id, Source: "bing"}, Path: p})
	}
	// LastChange is long ago, so the daemon would change it if it were not pinned.
	if err := saveState(State{History: hist, Cursor: 1}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if changed, err := setPin(ctx, true); !changed || err != nil {
		t.Fatalf("pin: %v, %v", changed, err)
	}
	if changed, err := setPin(ctx, true); changed || err != nil {
		t.Fatalf("pin again: %v, %v", changed, err)
	}
	for _, to := range []step{stepAuto, stepNew, stepNext, stepPrev} {
		if e, err := change(ctx, cfg, to, "", false); e != nil || !errors.Is(err, errPinned) {
			t.Errorf("step %d while pinned: %v, %v", to, e, err)
		}
	}
	e, err := change(ctx, cfg, stepPrev, "", true)
	if err != nil || e.ID != "bing:A" {
		t.Fatalf("prev --force: %v, %v", e, err)
	}
	if st, _ := loadState(); st.Pinned {
		t.Error("--force should unpin")
	}

	setPin(ctx, true)
	if changed, err := setPin(ctx, false); !changed || err != nil {
		t.Fatalf("unpin: %v, %v", changed, err)
	}
	// Unpinning restarts the interval.
	if e, err := change(ctx, cfg, stepAuto, "", false); e != nil || err != nil {
		t.Errorf("auto change right after unpin: %v, %v", e, err)
	}
	if e, err := change(ctx, cfg, stepNext, "", false); err != nil || e.ID != "bing:B" {
		t.Errorf("next after unpin: %v, %v", e, err)
	}
}

func TestRunSetter(t *testing.T) {
	ctx := context.Background()
	// A background child that keeps the output pipe open must not count as a failure.
	start := time.Now()
	if err := runSetter(ctx, []string{"/bin/sh", "-c", "sleep 3 & echo started", "sh", "{path}"}, "/x.jpg"); err != nil {
		t.Errorf("background child: %v", err)
	}
	if time.Since(start) > 2500*time.Millisecond {
		t.Error("runSetter waited for the background child")
	}
	err := runSetter(ctx, []string{"/bin/sh", "-c", "echo boom >&2; exit 3", "sh", "{path}"}, "/x.jpg")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failing setter: %v", err)
	}
}

func TestSave(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := defaultConfig()
	if _, _, err := save(cfg, "config.json"); err == nil || !strings.Contains(err.Error(), "save_dir is not set") {
		t.Fatalf("unset save_dir: %v", err)
	}
	cfg.SaveDir = filepath.Join(t.TempDir(), "missing")
	if _, _, err := save(cfg, "config.json"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing save_dir: %v", err)
	}
	if fileExists(cfg.SaveDir) {
		t.Fatal("save created save_dir")
	}
	cfg.SaveDir = t.TempDir()
	if _, _, err := save(cfg, "config.json"); !errors.Is(err, errNoWallpaper) {
		t.Fatalf("nothing shown yet: %v", err)
	}
	src := filepath.Join(t.TempDir(), "bing_A.jpg")
	os.WriteFile(src, []byte("image"), 0o644)
	saveState(State{History: []Entry{{Image: Image{ID: "bing:A"}, Path: src}}})
	dst, existed, err := save(cfg, "config.json")
	if data, _ := os.ReadFile(dst); err != nil || existed || string(data) != "image" {
		t.Fatalf("save: %v, existed %v, data %q", err, existed, data)
	}
	if _, existed, err := save(cfg, "config.json"); err != nil || !existed {
		t.Fatalf("second save: %v, existed %v", err, existed)
	}
}
