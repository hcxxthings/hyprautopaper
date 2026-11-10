package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// State is kept in $XDG_STATE_HOME/hyprautopaper/state.json.
type State struct {
	// When the wallpaper on screen was put there.
	LastChange time.Time `json:"last_change"`
	// Recently shown wallpapers, oldest first. Their files stay in the cache.
	History []Entry `json:"history"`
	// Index in History of the wallpaper on screen.
	Cursor int `json:"cursor"`
	// IDs of images shown before, newest last, so they are not repeated.
	Seen []string `json:"seen"`
	// Pinned keeps the wallpaper on screen: the daemon leaves it alone and
	// next and prev refuse to change it without --force.
	Pinned bool `json:"pinned,omitempty"`
}

// Entry is a shown image and its downloaded file.
type Entry struct {
	Image
	Path string `json:"path"`
}

const maxSeen = 500

var errNoWallpaper = errors.New(`no wallpaper has been set yet, run "hyprautopaper next"`)

// current returns the wallpaper on screen, or nil before the first one.
func (s *State) current() *Entry {
	if s.Cursor < 0 || s.Cursor >= len(s.History) {
		return nil
	}
	return &s.History[s.Cursor]
}

// add puts e at the end of the history and on screen, and drops the oldest
// entries beyond size.
func (s *State) add(e Entry, size int) {
	s.History = append(slices.DeleteFunc(s.History, func(h Entry) bool { return h.ID == e.ID }), e)
	if len(s.History) > size {
		s.History = s.History[len(s.History)-size:]
	}
	s.Cursor = len(s.History) - 1
	s.Seen = append(slices.DeleteFunc(s.Seen, func(id string) bool { return id == e.ID }), e.ID)
	if len(s.Seen) > maxSeen {
		s.Seen = s.Seen[len(s.Seen)-maxSeen:]
	}
}

func statePath() string {
	return filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "hyprautopaper", "state.json")
}

// loadState returns an empty state if there is no file yet. On a parse error
// it returns what it could read together with the error.
func loadState() (State, error) {
	var s State
	data, err := os.ReadFile(statePath())
	if err == nil {
		err = json.Unmarshal(data, &s)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	// Keep the cursor valid even if the file was edited by hand or is broken.
	if s.Cursor < 0 || s.Cursor >= len(s.History) {
		s.Cursor = len(s.History) - 1
	}
	return s, err
}

func saveState(s State) error {
	return writeFileAtomic(statePath(), func(f *os.File) error {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	})
}

// currentEntry returns the wallpaper on screen according to the state file.
func currentEntry() (Entry, error) {
	st, err := loadState()
	if err != nil {
		return Entry{}, err
	}
	cur := st.current()
	if cur == nil {
		return Entry{}, errNoWallpaper
	}
	return *cur, nil
}

// lock serialises changes from the CLI and the daemon. It gives up when ctx ends.
func lock(ctx context.Context) (func(), error) {
	p := filepath.Join(filepath.Dir(statePath()), "lock")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("waiting for another hyprautopaper: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// writeFileAtomic writes through a hidden temp file in the same directory and renames it.
func writeFileAtomic(path string, write func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
