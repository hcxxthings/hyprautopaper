// hyprautopaper changes the wallpaper using photos from Windows Spotlight, Bing's
// image of the day or wallhaven, shown through hyprpaper or any other setter.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

const usage = `Usage: hyprautopaper [-config FILE] COMMAND

Commands:
  next [--force] [SOURCE]
                 next wallpaper: forward in the history, or a new image at
                 its end; SOURCE (spotlight, bing, wallhaven) forces a new one
  prev [--force] previous wallpaper from the history
  pin            keep the current wallpaper: the daemon stops changing it and
                 next and prev fail unless given --force, which also unpins
  unpin          let the wallpaper change again
  save           copy the current wallpaper to "save_dir"
  info [-json]   title, copyright, link and file of the current wallpaper
  reapply        show the current wallpaper again
  daemon         change the wallpaper every "interval" (for a user service)
  version        print the version

The config file defaults to $XDG_CONFIG_HOME/hyprautopaper/config.json.
`

func main() {
	configPath := flag.String("config", defaultConfigPath(), "config file")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*configPath, flag.Arg(0), flag.Args()[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hyprautopaper:", err)
		var ue usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// usageError is a mistake in the command line or config that a restart cannot
// fix. It exits with status 2, which the systemd unit does not restart on.
type usageError struct{ error }

// Commands that usually run from key bindings, where nobody sees stderr.
var notifyCommands = []string{"next", "prev", "save", "pin", "unpin"}

func run(configPath, cmd string, args []string) error {
	if cmd == "version" {
		fmt.Println(version)
		return nil
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return usageError{err}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = dispatch(ctx, cfg, configPath, cmd, args)
	if err != nil && cfg.Notify && slices.Contains(notifyCommands, cmd) {
		notify("hyprautopaper "+cmd+" failed", err.Error())
	}
	return err
}

func dispatch(ctx context.Context, cfg Config, configPath, cmd string, args []string) error {
	switch cmd {
	case "daemon":
		return daemon(ctx, cfg)
	case "next", "prev":
		force := false
		args = slices.DeleteFunc(slices.Clone(args), func(a string) bool {
			if a == "--force" || a == "-force" || a == "-f" {
				force = true
				return true
			}
			return false
		})
		to, source := stepNext, ""
		if cmd == "prev" {
			to = stepPrev
			if len(args) > 0 {
				return usageError{errors.New("prev takes no arguments")}
			}
		}
		if len(args) > 0 {
			if !slices.Contains(validSources, args[0]) {
				return usageError{fmt.Errorf("unknown source %q (valid: %s)", args[0], strings.Join(validSources, ", "))}
			}
			to, source = stepNew, args[0]
		}
		e, err := change(ctx, cfg, to, source, force)
		if err != nil {
			return err
		}
		printInfo(os.Stdout, *e)
		if cfg.Notify {
			notify(e.Title, e.credit())
		}
		return nil
	case "save":
		path, existed, err := save(cfg, configPath)
		if err != nil {
			return err
		}
		fmt.Println(path)
		if cfg.Notify {
			summary := "Wallpaper saved"
			if existed {
				summary = "Wallpaper was saved before"
			}
			notify(summary, path)
		}
		return nil
	case "pin", "unpin":
		pinned := cmd == "pin"
		changed, err := setPin(ctx, pinned)
		if err != nil {
			return err
		}
		var msg string
		switch {
		case pinned && changed:
			msg = "Wallpaper pinned"
		case pinned:
			msg = "Wallpaper was already pinned"
		case changed:
			msg = "Wallpaper unpinned"
		default:
			msg = "Wallpaper was not pinned"
		}
		fmt.Println(msg)
		if cfg.Notify {
			notify(msg, "")
		}
		return nil
	case "reapply":
		cur, err := currentEntry()
		if err != nil {
			return err
		}
		return show(ctx, cfg, cur.Path)
	case "info":
		st, err := loadState()
		if err != nil {
			return err
		}
		cur := st.current()
		if cur == nil {
			return errNoWallpaper
		}
		if len(args) > 0 && args[0] == "-json" {
			return json.NewEncoder(os.Stdout).Encode(cur)
		}
		printInfo(os.Stdout, *cur)
		fmt.Printf("history: %d of %d\n", st.Cursor+1, len(st.History))
		if st.Pinned {
			fmt.Println("pinned:  yes")
		}
		return nil
	}
	flag.Usage()
	return usageError{fmt.Errorf("unknown command %q", cmd)}
}

// After a failed change the daemon waits 1m, 2m, 4m ... up to maxRetryDelay,
// so it recovers quickly when the network comes up late after boot or resume.
const maxRetryDelay = 15 * time.Minute

func daemon(ctx context.Context, cfg Config) error {
	logf("starting: interval %s, sources %v, backend %s", cfg.Interval, cfg.Sources, cfg.Backend)

	monitorAdded := make(chan struct{}, 1)
	go watchMonitors(ctx, monitorAdded)

	// The backend starts with its own image (hyprpaper's config); put ours back.
	if cur, err := currentEntry(); err == nil && fileExists(cur.Path) {
		reapplyWithRetry(ctx, cfg, cur.Path)
	}

	// With change_on_start the first change fetches a new image whatever the
	// age of the current one, and is retried like any other until it works.
	pending := stepAuto
	if cfg.ChangeOnStart {
		pending = stepNew
	}
	var retryAt time.Time
	var retryDelay time.Duration
	// change reads the state itself, so "hyprautopaper next" and "prev" count too.
	tick := func() {
		if time.Now().Before(retryAt) {
			return
		}
		e, err := change(ctx, cfg, pending, "", false)
		switch {
		case errors.Is(err, errPinned):
			// Pinned: not a failure, and change_on_start does not fire after unpin.
			pending, retryAt, retryDelay = stepAuto, time.Time{}, 0
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			retryDelay = min(max(2*retryDelay, time.Minute), maxRetryDelay)
			retryAt = time.Now().Add(retryDelay)
			logf("change failed, retrying in %s: %v", retryDelay, err)
		case e != nil:
			pending, retryAt, retryDelay = stepAuto, time.Time{}, 0
			logf("now showing %s: %s", e.Source, e.Title)
		}
	}

	// A short tick against the wall clock also handles suspend and reboots.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	tick()
	for {
		select {
		case <-ctx.Done():
			logf("stopping")
			return nil
		case <-ticker.C:
			tick()
		case <-monitorAdded:
			// Give the backend a moment to see the new output first.
			sleepCtx(ctx, 2*time.Second)
			if cur, err := currentEntry(); err == nil {
				if err := show(ctx, cfg, cur.Path); err != nil {
					logf("reapply after monitor added: %v", err)
				}
			}
		}
	}
}

// reapplyWithRetry waits up to a minute for the backend (e.g. hyprpaper) to come up.
func reapplyWithRetry(ctx context.Context, cfg Config, path string) {
	var err error
	for i := 0; i < 30 && ctx.Err() == nil; i++ {
		if err = show(ctx, cfg, path); err == nil {
			return
		}
		sleepCtx(ctx, 2*time.Second)
	}
	logf("could not show the current wallpaper: %v", err)
}

// credit is the line under the title: the photo credit, or the page about it.
func (e Entry) credit() string {
	if e.Copyright != "" {
		return e.Copyright
	}
	return e.Link
}

func printInfo(w io.Writer, e Entry) {
	fmt.Fprintf(w, "%s\n", e.Title)
	if e.Copyright != "" {
		fmt.Fprintf(w, "%s\n", e.Copyright)
	}
	fmt.Fprintf(w, "source:  %s\n", e.Source)
	if e.Link != "" {
		fmt.Fprintf(w, "link:    %s\n", e.Link)
	}
	fmt.Fprintf(w, "file:    %s\n", e.Path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeFileAtomic(dst, func(f *os.File) error {
		_, err := io.Copy(f, in)
		return err
	})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// journald adds timestamps, so log lines stay plain.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
