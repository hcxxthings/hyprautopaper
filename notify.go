package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// notifySend is set to an absolute path by the Nix package.
var notifySend = "notify-send"

// Notification servers may read the body as markup.
var markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// notify shows a desktop notification. Repeated ones replace each other on
// servers that know one of the two hints (dunst, swaync and others). A
// failure is only logged: the command itself has already done its work.
func notify(summary, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, notifySend,
		"--app-name=hyprautopaper",
		"--hint=string:x-dunst-stack-tag:hyprautopaper",
		"--hint=string:x-canonical-private-synchronous:hyprautopaper",
		summary, markupEscaper.Replace(body)).CombinedOutput()
	if err != nil {
		logf("notify-send: %v: %s", err, strings.TrimSpace(string(out)))
	}
}
