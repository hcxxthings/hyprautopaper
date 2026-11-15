package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"text/tabwriter"
	"time"
)

// Catalog is every wallpaper that was ever shown, by image ID. It is kept in
// catalog.json next to the state file and, unlike the history, never shrinks.
// A nil Catalog (the file could not be read) ignores all updates, so a broken
// file is never overwritten.
type Catalog map[string]CatalogEntry

type CatalogEntry struct {
	Image
	// Name of the image file in the cache, and in save_dir if it was saved.
	File       string    `json:"file"`
	FirstShown time.Time `json:"first_shown"`
	LastShown  time.Time `json:"last_shown"`
	// How often it was put on screen, moves with prev and next included.
	Times int `json:"times"`
	// Total time on screen.
	Seconds int64 `json:"shown_seconds"`
	Saved   bool  `json:"saved"`
	// Whether it was ever pinned; unpinning does not clear it.
	Pinned bool `json:"pinned"`
}

func catalogPath() string {
	return filepath.Join(filepath.Dir(statePath()), "catalog.json")
}

// readCatalog returns an empty catalog if there is no file yet.
func readCatalog() (Catalog, error) {
	c := Catalog{}
	data, err := os.ReadFile(catalogPath())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &c)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", catalogPath(), err)
	}
	if c == nil { // the file holds "null"
		c = Catalog{}
	}
	// Entries from before the wallpaper found on screen was counted as shown.
	for id, ce := range c {
		if ce.Times == 0 {
			ce.Times = 1
			c[id] = ce
		}
	}
	return c, nil
}

// loadCatalog is readCatalog for updates: an unreadable catalog is left alone.
func loadCatalog() Catalog {
	c, err := readCatalog()
	if err != nil {
		logf("catalog is not updated: %v", err)
	}
	return c
}

// save writes the catalog. Losing an update is not worth failing a wallpaper
// change that is already on screen, so errors are only logged.
func (c Catalog) save() {
	if c == nil {
		return
	}
	err := writeFileAtomic(catalogPath(), func(f *os.File) error {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(c)
	})
	if err != nil {
		logf("saving the catalog: %v", err)
	}
}

// update changes the catalog entry of e, creating it first if needed.
func (c Catalog) update(e Entry, f func(*CatalogEntry)) {
	if c == nil {
		return
	}
	ce := c[e.ID]
	ce.Image = e.Image
	ce.File = filepath.Base(e.Path)
	f(&ce)
	// A wallpaper that was on screen before the catalog existed is first seen
	// here when it is settled, saved or pinned; it was shown once.
	if ce.Times == 0 {
		ce.Times = 1
		ce.FirstShown = time.Now().Round(0)
		ce.LastShown = ce.FirstShown
	}
	c[e.ID] = ce
}

// settle adds the time from st.CountedTo to upTo to the wallpaper on screen.
// The caller then moves or clears st.CountedTo.
func (c Catalog) settle(st *State, upTo time.Time) {
	cur := st.current()
	if cur == nil || st.CountedTo.IsZero() {
		return
	}
	// Not positive when the clock went back, or when upTo is before a change.
	if d := upTo.Sub(st.CountedTo); d > 0 {
		c.update(*cur, func(ce *CatalogEntry) { ce.Seconds += int64(d.Round(time.Second) / time.Second) })
	}
}

// shown records that e was put on screen at now.
func (c Catalog) shown(e Entry, now time.Time) {
	c.update(e, func(ce *CatalogEntry) {
		if ce.FirstShown.IsZero() {
			ce.FirstShown = now
		}
		ce.LastShown = now
		ce.Times++
	})
}

// countScreenTime is how the daemon keeps time it was not running, or the
// machine was suspended, out of the catalog: it adds the time up to upTo to
// the wallpaper on screen and then restarts the count from now, or stops it
// when running is false. A zero upTo adds nothing.
func countScreenTime(ctx context.Context, upTo time.Time, running bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		logf("counting screen time: %v", err)
		return
	}
	defer unlock()
	st, err := loadState()
	if err != nil || st.current() == nil {
		return
	}
	cat := loadCatalog()
	cat.settle(&st, upTo)
	st.CountedTo = time.Time{}
	if running {
		st.CountedTo = time.Now().Round(0)
	}
	cat.save()
	if err := saveState(st); err != nil {
		logf("counting screen time: %v", err)
	}
}

// markSaved records in the catalog that e was copied to save_dir.
func markSaved(e Entry) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		logf("marking the wallpaper as saved: %v", err)
		return
	}
	defer unlock()
	cat := loadCatalog()
	if cat != nil && !cat[e.ID].Saved {
		cat.update(e, func(ce *CatalogEntry) { ce.Saved = true })
		cat.save()
	}
}

// applyCatalog shows a wallpaper from the catalog again and puts it at the
// end of the history. ref is its number in "hyprautopaper catalog" or its
// image ID. A pin is handled as in change.
func applyCatalog(ctx context.Context, cfg Config, ref string, force bool) (*Entry, error) {
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
	if st.Pinned {
		if !force {
			return nil, errPinned
		}
		st.Pinned = false
	}
	cat, err := readCatalog()
	if err != nil {
		return nil, err
	}
	now := time.Now().Round(0)
	cat.settle(&st, now)
	st.CountedTo = now
	ce, err := findCatalog(catalogRows(cat, st, now), ref)
	if err != nil {
		return nil, err
	}
	path, err := restore(ctx, cfg, ce)
	if err != nil {
		return nil, err
	}
	if err := show(ctx, cfg, path); err != nil {
		return nil, err
	}
	st.add(Entry{Image: ce.Image, Path: path}, cfg.History)
	return record(cfg, &st, cat)
}

// findCatalog returns the row with rank or image ID ref.
func findCatalog(rows []CatalogRow, ref string) (CatalogEntry, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 1 || n > len(rows) {
			return CatalogEntry{}, usageError{fmt.Errorf("no wallpaper number %d, the catalog has %d", n, len(rows))}
		}
		return rows[n-1].CatalogEntry, nil
	}
	for _, r := range rows {
		if r.ID == ref {
			return r.CatalogEntry, nil
		}
	}
	return CatalogEntry{}, usageError{fmt.Errorf("no wallpaper %q in the catalog", ref)}
}

// restore returns the image file of ce: the one still in the cache, a copy
// of the one in save_dir, or a new download.
func restore(ctx context.Context, cfg Config, ce CatalogEntry) (string, error) {
	if name := filepath.Base(ce.File); ce.File != "" {
		path := filepath.Join(cfg.CacheDir, name)
		if fileExists(path) {
			return path, nil
		}
		if saved := filepath.Join(cfg.SaveDir, name); cfg.SaveDir != "" && fileExists(saved) && copyFile(saved, path) == nil {
			return path, nil
		}
	}
	path, err := download(ctx, ce.Image, cfg.CacheDir)
	if err != nil {
		return "", fmt.Errorf("the image is no longer on disk and could not be downloaded again: %w", err)
	}
	return path, nil
}

// CatalogRow is one line of "hyprautopaper catalog".
type CatalogRow struct {
	Rank int `json:"rank"`
	CatalogEntry
	Current bool `json:"current"`
}

// catalogRows returns the catalog ranked by time on screen, longest first.
// The wallpaper on screen includes the time that is not settled yet.
func catalogRows(c Catalog, st State, now time.Time) []CatalogRow {
	cur := st.current()
	if cur != nil {
		c.settle(&st, now)
	}
	rows := make([]CatalogRow, 0, len(c))
	for _, ce := range c {
		rows = append(rows, CatalogRow{CatalogEntry: ce, Current: cur != nil && cur.ID == ce.ID})
	}
	slices.SortFunc(rows, func(a, b CatalogRow) int {
		return cmp.Or(
			cmp.Compare(b.Seconds, a.Seconds),
			b.LastShown.Compare(a.LastShown),
			cmp.Compare(a.ID, b.ID),
		)
	})
	for i := range rows {
		rows[i].Rank = i + 1
	}
	return rows
}

func catalogCommand(args []string) error {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	savedOnly := fs.Bool("saved", false, "")
	limit := fs.Int("n", 10, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *limit < 0 {
		return usageError{errors.New("usage: catalog [-json] [-saved] [-n COUNT]")}
	}
	cat, err := readCatalog()
	if err != nil {
		return err
	}
	st, _ := loadState()
	rows := catalogRows(cat, st, time.Now().Round(0))
	// The rank stays the one in the whole catalog.
	if *savedOnly {
		rows = slices.DeleteFunc(rows, func(r CatalogRow) bool { return !r.Saved })
	}
	more := 0
	if *limit > 0 && len(rows) > *limit { // 0 shows all
		more = len(rows) - *limit
		rows = rows[:*limit]
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	printCatalog(os.Stdout, rows)
	if more > 0 {
		fmt.Printf("%d more, see them all with -n 0\n", more)
	}
	return nil
}

func printCatalog(w io.Writer, rows []CatalogRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "the catalog is empty")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tON SCREEN\tTIMES\tLAST SHOWN\tSAVED\tPINNED\tSOURCE\tTITLE")
	yesNo := map[bool]string{true: "yes", false: "-"}
	for _, r := range rows {
		title := r.Title
		if title == "" {
			title = r.ID
		}
		if r.Current {
			title += " (current)"
		}
		last := "-"
		if !r.LastShown.IsZero() {
			last = r.LastShown.Local().Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", r.Rank,
			formatScreenTime(time.Duration(r.Seconds)*time.Second), r.Times,
			last, yesNo[r.Saved], yesNo[r.Pinned], r.Source, title)
	}
	tw.Flush()
}

// formatScreenTime prints the two largest units: "3d 4h", "5h 12m", "12m".
func formatScreenTime(d time.Duration) string {
	days, hours, minutes := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}
