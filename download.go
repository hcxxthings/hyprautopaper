package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const maxImageSize = 64 << 20

// download saves img into dir and returns the file. The name comes from the
// image ID, so the same image always lands in the same file.
func download(ctx context.Context, img Image, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, img.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", img.URL, res.Status)
	}
	// Some CDNs (Spotlight's) send "text/plain", so the format is sniffed from the data.
	body := bufio.NewReader(io.LimitReader(res.Body, maxImageSize+1))
	head, _ := body.Peek(512)
	ext, err := imageExt(head)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", img.URL, err)
	}
	path := filepath.Join(dir, cacheName(img.ID)+ext)
	err = writeFileAtomic(path, func(f *os.File) error {
		n, err := io.Copy(f, body)
		if err == nil && n > maxImageSize {
			err = errors.New("image is larger than 64 MiB")
		}
		return err
	})
	return path, err
}

// imageExt returns the file extension for the formats hyprpaper can load.
func imageExt(head []byte) (string, error) {
	switch {
	case bytes.HasPrefix(head, []byte{0xff, 0x0a}),
		bytes.HasPrefix(head, []byte("\x00\x00\x00\x0cJXL \r\n\x87\n")):
		return ".jxl", nil
	}
	switch ct := http.DetectContentType(head); ct {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", fmt.Errorf("not a supported image (%s)", ct)
	}
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// cacheName turns "bing:Name" into "bing_Name".
func cacheName(id string) string {
	return strings.Trim(unsafeChars.ReplaceAllString(id, "_"), "_")
}

var imageExts = []string{".jpg", ".png", ".webp", ".jxl"}

// isCacheFile matches only names that download creates, so a cache_dir that
// is shared with other pictures never loses anything else.
func isCacheFile(name string) bool {
	src, _, ok := strings.Cut(name, "_")
	return ok && slices.Contains(validSources, src) && slices.Contains(imageExts, filepath.Ext(name))
}

// cleanCache removes downloaded images in dir that are no longer in the history.
func cleanCache(dir string, history []Entry) {
	keep := map[string]bool{}
	for _, e := range history {
		keep[e.Path] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.Type().IsRegular() && isCacheFile(e.Name()) && !keep[p] {
			os.Remove(p)
		}
	}
}
