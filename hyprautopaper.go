package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// step says where change goes.
type step int

const (
	// stepAuto fetches a new image once the interval has passed (the daemon).
	stepAuto step = iota
	// stepNew always fetches a new image.
	stepNew
	// stepNext moves forward in the history, or fetches a new image at its end.
	stepNext
	// stepPrev moves back in the history.
	stepPrev
)

// Upper bound for one change: waiting for the lock, fetching, downloading, showing.
const changeTimeout = 3 * time.Minute

// How many candidates to try when downloads fail (e.g. a missing UHD version).
const maxAttempts = 3

var errNoOlder = errors.New("no older wallpaper in the history")

// change shows another wallpaper and records it in the state. source limits
// new images to one source; empty means any of cfg.Sources. With stepAuto it
// returns nil while the wallpaper on screen is newer than cfg.Interval.
// It runs under the lock, so the daemon and the CLI never change the
// wallpaper twice in a row or overwrite each other's history.
func change(ctx context.Context, cfg Config, to step, source string) (*Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, changeTimeout)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	st, err := loadState()
	if err != nil {
		logf("ignoring broken state: %v", err)
	}
	switch to {
	case stepAuto:
		if !isDue(st, cfg.Interval) {
			return nil, nil
		}
	case stepNext:
		if st.Cursor < len(st.History)-1 {
			return showHistory(ctx, cfg, &st, st.Cursor+1)
		}
	case stepPrev:
		if st.Cursor <= 0 {
			return nil, errNoOlder
		}
		return showHistory(ctx, cfg, &st, st.Cursor-1)
	}
	sources := cfg.Sources
	if source != "" {
		sources = []string{source}
	}
	return showNew(ctx, cfg, &st, sources)
}

func isDue(st State, interval Duration) bool {
	cur := st.current()
	if cur == nil || !fileExists(cur.Path) {
		return true
	}
	if interval == 0 { // "off"
		return false
	}
	age := time.Since(st.LastChange)
	// A negative age means the clock went back; do not wait for it to catch up.
	return age < 0 || age >= time.Duration(interval)
}

// showHistory shows History[i], downloading it again if its file is gone.
func showHistory(ctx context.Context, cfg Config, st *State, i int) (*Entry, error) {
	e := &st.History[i]
	if !fileExists(e.Path) {
		path, err := download(ctx, e.Image, cfg.CacheDir)
		if err != nil {
			return nil, fmt.Errorf("%s is gone and could not be downloaded again: %w", e.Path, err)
		}
		e.Path = path
	}
	if err := show(ctx, cfg, e.Path); err != nil {
		return nil, err
	}
	st.Cursor = i
	return record(cfg, st)
}

// showNew downloads and shows a new image from one of sources.
func showNew(ctx context.Context, cfg Config, st *State, sources []string) (*Entry, error) {
	imgs, err := candidates(ctx, cfg, sources, st.Seen)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, img := range imgs[:min(len(imgs), maxAttempts)] {
		path, err := download(ctx, img, cfg.CacheDir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := show(ctx, cfg, path); err != nil {
			return nil, err
		}
		st.add(Entry{Image: img, Path: path}, cfg.History)
		return record(cfg, st)
	}
	return nil, errors.Join(errs...)
}

// record saves the state with st.Cursor on screen since now and removes
// cached images that fell out of the history.
func record(cfg Config, st *State) (*Entry, error) {
	// Round(0) drops the monotonic reading so that time spent suspended counts.
	st.LastChange = time.Now().Round(0)
	if err := saveState(*st); err != nil {
		return nil, err
	}
	cleanCache(cfg.CacheDir, st.History)
	e := *st.current()
	return &e, nil
}

// candidates returns the images to try, best first: the not recently shown
// images of the first source (in random order) that has any, otherwise all
// images, least recently shown first.
func candidates(ctx context.Context, cfg Config, sources []string, seen []string) ([]Image, error) {
	order := slices.Clone(sources)
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	var all []Image
	var errs []error
	for _, name := range order {
		imgs, err := fetchSource(ctx, name, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		fresh := slices.DeleteFunc(slices.Clone(imgs), func(im Image) bool { return slices.Contains(seen, im.ID) })
		if len(fresh) > 0 {
			rand.Shuffle(len(fresh), func(i, j int) { fresh[i], fresh[j] = fresh[j], fresh[i] })
			return fresh, nil
		}
		all = append(all, imgs...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no images: %w", errors.Join(errs...))
	}
	slices.SortStableFunc(all, func(a, b Image) int {
		return slices.Index(seen, a.ID) - slices.Index(seen, b.ID)
	})
	return all, nil
}

// save copies the wallpaper on screen into cfg.SaveDir. It returns the copy
// and whether it was there already. save_dir is never created, so that a typo
// in the path fails instead of filling a new directory nobody looks at.
func save(cfg Config, configPath string) (string, bool, error) {
	if cfg.SaveDir == "" {
		return "", false, usageError{fmt.Errorf(`save_dir is not set; add "save_dir" to %s`, configPath)}
	}
	info, err := os.Stat(cfg.SaveDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", false, usageError{fmt.Errorf("save_dir %s does not exist; create it or fix save_dir in %s", cfg.SaveDir, configPath)}
	case err != nil:
		return "", false, err
	case !info.IsDir():
		return "", false, usageError{fmt.Errorf("save_dir %s is not a directory", cfg.SaveDir)}
	}
	cur, err := currentEntry()
	if err != nil {
		return "", false, err
	}
	dst := filepath.Join(cfg.SaveDir, filepath.Base(cur.Path))
	if fileExists(dst) {
		return dst, true, nil
	}
	return dst, false, copyFile(cur.Path, dst)
}
