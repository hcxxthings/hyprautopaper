package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// hyprctl waits for hyprpaper's answer without a timeout of its own.
const hyprctlTimeout = 15 * time.Second

// applyHyprpaper sets path as the fallback (used by monitors connected later)
// and shows it on every connected monitor.
func applyHyprpaper(ctx context.Context, path, fitMode string) error {
	// hyprctl splits the argument on commas.
	if strings.Contains(path, ",") {
		return fmt.Errorf("path %q contains a comma, hyprpaper cannot take it over IPC", path)
	}
	fit := ipcFitMode(fitMode)
	if _, err := hyprctl(ctx, "hyprpaper", "wallpaper", ","+path+","+fit); err != nil {
		return err
	}
	monitors, err := monitorNames(ctx)
	if err != nil {
		return err
	}
	for _, m := range monitors {
		// A monitor can disappear between listing and setting (DPMS, Thunderbolt);
		// it gets the wallpaper again when Hyprland reports it back.
		if _, err := hyprctl(ctx, "hyprpaper", "wallpaper", m+","+path+","+fit); err != nil {
			logf("monitor %s: %v", m, err)
		}
	}
	return nil
}

// hyprpaper's config calls it "fill", hyprctl only knows "stretch" and
// silently uses "cover" for anything it does not recognise.
func ipcFitMode(mode string) string {
	if mode == "fill" {
		return "stretch"
	}
	return mode
}

func monitorNames(ctx context.Context) ([]string, error) {
	out, err := hyprctl(ctx, "-j", "monitors")
	if err != nil {
		return nil, err
	}
	var mons []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &mons); err != nil {
		return nil, fmt.Errorf("hyprctl monitors: %w", err)
	}
	names := make([]string, 0, len(mons))
	for _, m := range mons {
		names = append(names, m.Name)
	}
	return names, nil
}

// hyprctl prints nothing on success and "error: ..." with exit status 1 on failure.
func hyprctl(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, hyprctlTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "hyprctl", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(string(out) + " " + stderr.String())
		return nil, fmt.Errorf("hyprctl %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return out, nil
}

// watchMonitors sends on added whenever Hyprland reports a new monitor
// (e.g. an output that came back after DPMS or a Thunderbolt reconnect).
// It reconnects to the event socket until ctx is cancelled.
func watchMonitors(ctx context.Context, added chan<- struct{}) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		logf("HYPRLAND_INSTANCE_SIGNATURE is not set, not watching for new monitors")
		return
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	sock := filepath.Join(runtime, "hypr", sig, ".socket2.sock")
	for ctx.Err() == nil {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", sock)
		if err != nil {
			sleepCtx(ctx, 10*time.Second)
			continue
		}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				conn.Close()
			case <-done:
			}
		}()
		sc := bufio.NewScanner(conn)
		// Window title events can be long.
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "monitoradded>>") {
				select {
				case added <- struct{}{}:
				default:
				}
			}
		}
		close(done)
		conn.Close()
		sleepCtx(ctx, 2*time.Second)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
