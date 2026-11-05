package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// show puts the image at path on screen with the configured backend.
func show(ctx context.Context, cfg Config, path string) error {
	if cfg.Backend == "command" {
		return runSetter(ctx, cfg.Command, path)
	}
	return applyHyprpaper(ctx, path, cfg.FitMode)
}

// A setter must exit. One that keeps running (like swaybg) needs a wrapper
// script that starts it in the background with its output redirected.
const setterTimeout = 30 * time.Second

// runSetter runs the user's command with "{path}" replaced in every argument.
// No shell is involved, so the path needs no quoting.
func runSetter(ctx context.Context, command []string, path string) error {
	ctx, cancel := context.WithTimeout(ctx, setterTimeout)
	defer cancel()
	args := make([]string, len(command))
	for i, a := range command {
		args[i] = strings.ReplaceAll(a, "{path}", path)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	// A wrapper may leave a background child that holds the output pipe open.
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
