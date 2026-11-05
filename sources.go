package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Image is one candidate wallpaper returned by a source.
type Image struct {
	ID        string `json:"id"` // unique across sources, used to avoid repeats
	Source    string `json:"source"`
	URL       string `json:"url"` // direct link to the image file
	Title     string `json:"title,omitempty"`
	Copyright string `json:"copyright,omitempty"`
	Link      string `json:"link,omitempty"` // page about the image
}

const userAgent = "hyprautopaper (+https://github.com/hcxxthings/hyprautopaper)"

// The whole change has a deadline; this only catches servers that accept
// the connection and then never answer.
var httpClient = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: t}
}()

func fetchSource(ctx context.Context, name string, cfg Config) ([]Image, error) {
	switch name {
	case "bing":
		return fetchBing(ctx, cfg)
	case "spotlight":
		return fetchSpotlight(ctx, cfg)
	case "wallhaven":
		return fetchWallhaven(ctx, cfg)
	}
	return nil, fmt.Errorf("unknown source %q", name)
}

// fetchBing returns the Bing images of the day for the last ~15 days, in UHD.
func fetchBing(ctx context.Context, cfg Config) ([]Image, error) {
	var out []Image
	// The archive endpoint returns at most 8 images per request and goes back to idx 7.
	for _, idx := range []int{0, 7} {
		q := url.Values{"format": {"js"}, "idx": {fmt.Sprint(idx)}, "n": {"8"}, "mkt": {cfg.Market}}
		var resp struct {
			Images []struct {
				URLBase       string `json:"urlbase"`
				Title         string `json:"title"`
				Copyright     string `json:"copyright"`
				CopyrightLink string `json:"copyrightlink"`
			} `json:"images"`
		}
		if err := getJSON(ctx, "https://www.bing.com/HPImageArchive.aspx?"+q.Encode(), nil, &resp); err != nil {
			return out, err
		}
		for _, im := range resp.Images {
			// urlbase looks like "/th?id=OHR.StileCrossing_EN-US9052435517".
			u, err := url.Parse(im.URLBase)
			if err != nil || u.Query().Get("id") == "" {
				continue
			}
			id := u.Query().Get("id")
			// The market suffix differs per locale for the same picture; drop it.
			name, _, _ := strings.Cut(strings.TrimPrefix(id, "OHR."), "_")
			out = append(out, Image{
				ID:        "bing:" + name,
				Source:    "bing",
				URL:       "https://www.bing.com" + im.URLBase + "_UHD.jpg",
				Title:     im.Title,
				Copyright: im.Copyright,
				Link:      im.CopyrightLink,
			})
		}
	}
	return dedupe(out), nil
}

// fetchSpotlight returns a few random Windows Spotlight desktop images (3840x2160).
func fetchSpotlight(ctx context.Context, cfg Config) ([]Image, error) {
	country := "US"
	if _, c, ok := strings.Cut(cfg.Market, "-"); ok {
		country = strings.ToUpper(c)
	}
	q := url.Values{
		"placement": {"88000820"}, // Windows 11 desktop Spotlight
		"bcnt":      {"4"},
		"country":   {country},
		"locale":    {cfg.Market},
		"fmt":       {"json"},
	}
	var resp struct {
		BatchRsp struct {
			Items []struct {
				Item string `json:"item"` // JSON encoded as a string
			} `json:"items"`
		} `json:"batchrsp"`
	}
	if err := getJSON(ctx, "https://fd.api.iris.microsoft.com/v4/api/selection?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	var out []Image
	for _, it := range resp.BatchRsp.Items {
		var item struct {
			Ad struct {
				Title          string `json:"title"`
				Copyright      string `json:"copyright"`
				EntityID       string `json:"entityId"`
				CtaURI         string `json:"ctaUri"`
				LandscapeImage struct {
					Asset string `json:"asset"`
				} `json:"landscapeImage"`
			} `json:"ad"`
		}
		if err := json.Unmarshal([]byte(it.Item), &item); err != nil || item.Ad.LandscapeImage.Asset == "" {
			continue
		}
		id := item.Ad.EntityID
		if id == "" {
			id = item.Ad.LandscapeImage.Asset
		}
		out = append(out, Image{
			ID:        "spotlight:" + id,
			Source:    "spotlight",
			URL:       item.Ad.LandscapeImage.Asset,
			Title:     item.Ad.Title,
			Copyright: item.Ad.Copyright,
			Link:      bingSearchLink(item.Ad.CtaURI),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("spotlight: no images in response")
	}
	return out, nil
}

// Spotlight's ctaUri is "microsoft-edge:https://www.bing.com/search?..."; keep the web part.
func bingSearchLink(cta string) string {
	if i := strings.Index(cta, "https://"); i >= 0 {
		return cta[i:]
	}
	return ""
}

// fetchWallhaven returns SFW images from a random page of the wallhaven toplist or search.
func fetchWallhaven(ctx context.Context, cfg Config) ([]Image, error) {
	w := cfg.Wallhaven
	q := url.Values{
		"categories": {w.Categories},
		"purity":     {"100"}, // SFW only
	}
	if w.Query != "" {
		q.Set("q", w.Query)
		q.Set("sorting", "favorites")
	} else {
		q.Set("sorting", "toplist")
		q.Set("topRange", w.TopRange)
	}
	if w.AtLeast != "" {
		q.Set("atleast", w.AtLeast)
	}
	if w.Ratios != "" {
		q.Set("ratios", w.Ratios)
	}
	header := http.Header{}
	if w.APIKeyFile != "" {
		key, err := os.ReadFile(w.APIKeyFile)
		if err != nil {
			return nil, fmt.Errorf("wallhaven api key: %w", err)
		}
		header.Set("X-API-Key", strings.TrimSpace(string(key)))
	}
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			URL        string `json:"url"`
			Path       string `json:"path"`
			Resolution string `json:"resolution"`
		} `json:"data"`
	}
	search := func(page int) error {
		q.Set("page", fmt.Sprint(page))
		return getJSON(ctx, "https://wallhaven.cc/api/v1/search?"+q.Encode(), header, &resp)
	}
	page := rand.IntN(3) + 1
	err := search(page)
	if err == nil && len(resp.Data) == 0 && page > 1 {
		// A narrow query or strict filters can have fewer pages.
		err = search(1)
	}
	if err != nil {
		return nil, err
	}
	var out []Image
	for _, d := range resp.Data {
		out = append(out, Image{
			ID:     "wallhaven:" + d.ID,
			Source: "wallhaven",
			URL:    d.Path,
			Title:  "wallhaven " + d.ID + " (" + d.Resolution + ")",
			Link:   d.URL,
		})
	}
	if len(out) == 0 {
		return nil, errors.New("wallhaven: no results")
	}
	return out, nil
}

func getJSON(ctx context.Context, u string, header http.Header, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", req.URL.Host, res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}

func dedupe(in []Image) []Image {
	seen := map[string]bool{}
	var out []Image
	for _, im := range in {
		if !seen[im.ID] {
			seen[im.ID] = true
			out = append(out, im)
		}
	}
	return out
}
