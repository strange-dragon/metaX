package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/strange-dragon/metaX/common/fetch"
	"github.com/strange-dragon/metaX/imageutil"
	mt "github.com/strange-dragon/metaX/provider"
)

// The artwork probe answers a question the metadata probe cannot: how many
// independent picture sources this build actually has for a title, and which of
// them serve a picture at all from the current egress.
//
// A provider that returns a cover URL is not a picture source until the bytes
// arrive. Several providers also sit behind the same image CDN (FANZA, Jav321
// and JavBus all hand out pics.dmm.co.jp), so counting providers overstates the
// number of sources: when that CDN is blocked or slow, all of them fail
// together. This probe therefore reports the host next to the result and the
// summary groups by host.
type artworkResult struct {
	provider string
	number   string
	title    string
	kind     string
	rawURL   string
	host     string
	width    int
	height   int
	bytes    int64
	elapsed  time.Duration
	stage    string
	err      error
}

// artworkCandidates orders the artwork a provider publishes from the sharpest
// variant down. The engine prefers big_thumb for a poster and big_cover for a
// backdrop; either way the useful question here is how large the provider's
// best picture is.
func artworkCandidates() []struct {
	kind string
	pick func(*movieArtwork) string
} {
	return []struct {
		kind string
		pick func(*movieArtwork) string
	}{
		{"big_cover", func(a *movieArtwork) string { return a.bigCover }},
		{"big_thumb", func(a *movieArtwork) string { return a.bigThumb }},
		{"cover", func(a *movieArtwork) string { return a.cover }},
		{"thumb", func(a *movieArtwork) string { return a.thumb }},
	}
}

type movieArtwork struct {
	cover    string
	bigCover string
	thumb    string
	bigThumb string
	previews []string
}

func pickArtwork(a movieArtwork) (kind, rawURL string) {
	for _, candidate := range artworkCandidates() {
		if value := candidate.pick(&a); strings.TrimSpace(value) != "" {
			return candidate.kind, value
		}
	}
	return "", ""
}

func runArtworkProbe(ctx context.Context, factories map[string]mt.MovieFactory, selected []string, override string, timeout time.Duration, concurrency int) int {
	if concurrency < 1 {
		concurrency = 1
	}
	results := make([]artworkResult, len(selected))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, name := range selected {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probeArtwork(ctx, factories[name], name, override, timeout)
		}(i, name)
	}
	wg.Wait()

	ok, failed := 0, 0
	for _, res := range results {
		if res.err != nil {
			failed++
			fmt.Printf("FAIL  %-18s %-8s %6.2fs  %s\n", res.provider, res.stage, res.elapsed.Seconds(), truncate(res.err.Error(), 120))
			continue
		}
		ok++
		fmt.Printf("OK    %-18s %-9s %4dx%-4d %7s %6.2fs  %s\n",
			res.provider, res.kind, res.width, res.height, humanBytes(res.bytes), res.elapsed.Seconds(), res.host)
	}

	printArtworkSources(results)
	fmt.Printf("\n%d/%d providers served artwork, %d failed\n", ok, len(results), failed)
	if ok == 0 {
		return 1
	}
	return 0
}

// printArtworkSources groups the successes by host. This is the number that
// matters when adding picture sources: a build with twelve artwork-capable
// providers on four hosts has four sources, not twelve.
func printArtworkSources(results []artworkResult) {
	hosts := make(map[string][]string)
	sizes := make(map[string]string)
	for _, res := range results {
		if res.err != nil || res.host == "" {
			continue
		}
		hosts[res.host] = append(hosts[res.host], res.provider)
		sizes[res.host] = fmt.Sprintf("%dx%d", res.width, res.height)
	}
	if len(hosts) == 0 {
		return
	}
	names := make([]string, 0, len(hosts))
	for host := range hosts {
		names = append(names, host)
	}
	sort.Strings(names)
	fmt.Printf("\n%d distinct artwork host(s):\n", len(names))
	for _, host := range names {
		providers := hosts[host]
		sort.Strings(providers)
		fmt.Printf("  %-34s %-10s %s\n", host, sizes[host], strings.Join(providers, ", "))
	}
}

func probeArtwork(ctx context.Context, factory mt.MovieFactory, name, override string, timeout time.Duration) (res artworkResult) {
	res = artworkResult{provider: name, stage: "detail"}
	start := time.Now()
	defer func() { res.elapsed = time.Since(start) }()

	sample := override
	if sample == "" {
		sample = defaultSamples[strings.ToLower(name)]
	}
	if sample == "" {
		res.stage = "sample"
		res.err = fmt.Errorf("no sample number configured; pass -id")
		return res
	}

	p, err := newProvider(factory, timeout)
	if err != nil {
		res.stage = "proxy"
		res.err = err
		return res
	}
	if closer, ok := p.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	info, err := p.GetMovieInfoByID(p.NormalizeMovieID(sample))
	if err != nil {
		res.err = err
		return res
	}
	if info == nil {
		res.err = fmt.Errorf("nil info without error")
		return res
	}
	res.number, res.title = info.Number, info.Title

	kind, rawURL := pickArtwork(movieArtwork{
		cover:    info.CoverURL,
		bigCover: info.BigCoverURL,
		thumb:    info.ThumbURL,
		bigThumb: info.BigThumbURL,
		previews: info.PreviewImages,
	})
	if rawURL == "" {
		res.stage = "artwork"
		res.err = fmt.Errorf("no artwork url in detail")
		return res
	}
	res.kind, res.rawURL = kind, rawURL
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil {
		res.host = parsed.Hostname()
	}

	res.stage = "download"
	width, height, size, err := downloadArtwork(p, rawURL, timeout)
	if err != nil {
		res.err = err
		return res
	}
	res.width, res.height, res.bytes = width, height, size
	return res
}

// downloadArtwork fetches the bytes through the provider's own fetcher, which
// is what the image endpoints do: a provider's Fetch serves both its pages and
// its artwork, so an unlisted image host is not fetched with anyone's cookies
// or referer but its own.
func downloadArtwork(p mt.MovieProvider, rawURL string, timeout time.Duration) (width, height int, size int64, err error) {
	var client interface {
		Fetch(string) (*http.Response, error)
	}
	if fetcher, ok := p.(mt.Fetcher); ok {
		client = fetcher
	} else {
		// Same fallback the engine uses when a provider has no fetcher of its
		// own: the shared fetch layer, no antibot browser.
		client = fetch.DefaultFetcher
	}

	resp, err := client.Fetch(rawURL)
	if err != nil {
		return 0, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, 0, 0, fmt.Errorf("%s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return 0, 0, 0, err
	}
	img, _, decodeErr := imageutil.Decode(bytes.NewReader(body))
	if decodeErr != nil {
		return 0, 0, int64(len(body)), fmt.Errorf("decode %d bytes: %w", len(body), decodeErr)
	}
	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), int64(len(body)), nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
