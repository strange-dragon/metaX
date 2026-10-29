// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for metaX's Provider API; no Amane source file or
// fixture is vendored. See ../../NOTICE.md for attribution.
package jav321

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/net/html"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/internal/antibot"
)

const (
	Name           = "Jav321"
	Priority       = 1000 - 11
	defaultURL     = "https://www.jav321.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
	userAgent      = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

	// maxTransientAttempts bounds retries for 408/429/503/504 responses. Retries
	// cannot turn a hard block into content, so the attempt count stays small.
	maxTransientAttempts = 3
)

var (
	ErrMovieNotFound = errors.New("jav321: movie not found")
	// A detail slug is either <label><digits> (ssis00033) or, when the bare
	// label would collide with an existing route, a short digit prefix glued in
	// front of it (SDMM-031 -> 1sdmm00031). The prefix is part of the site's
	// URL scheme, not of the catalog number, but it still has to be accepted
	// here or every prefixed work is reported as missing even though the search
	// redirected straight to its detail page.
	movieNumberRE    = regexp.MustCompile(`(?i)^(?:[A-Z0-9]{1,16}[-_][A-Z0-9]{1,24}(?:[-_][A-Z0-9]+)*|[0-9]{1,3}[A-Z]{2,12}[0-9]{2,12}|[A-Z]{2,12}[0-9]{2,12}|FC2[-_]?PPV[-_]?[0-9]+)$`)
	numberInTitleRE  = regexp.MustCompile(`(?i)(FC2[-_]?PPV[-_]?[0-9]+|[A-Z0-9]{1,16}[-_][A-Z0-9]{1,24}(?:[-_][A-Z0-9]+)*)`)
	runtimeMinutesRE = regexp.MustCompile(`(?i)(\d+)\s*(?:min|minutes?|分)`)
	previewImageRE   = regexp.MustCompile(`(?i)jp-(\d+)\.jpg$`)
)

var (
	_ mt.MovieProvider        = (*Jav321)(nil)
	_ mt.MovieSearcher        = (*Jav321)(nil)
	_ mt.Fetcher              = (*Jav321)(nil)
	_ mt.ProxySetter          = (*Jav321)(nil)
	_ mt.RequestTimeoutSetter = (*Jav321)(nil)
	_ mt.ConfigSetter         = (*Jav321)(nil)
)

// Jav321 implements the metaX movie Provider contract for jav321.com.
//
// The site is reachable from most networks, but its edge protection answers
// plain Go clients with a Cloudflare challenge from some egress IPs. All
// requests therefore go through the shared antibot layer, which rotates browser
// TLS fingerprints and falls back to Chromium for interactive challenges.
type Jav321 struct {
	baseURL *url.URL

	stateMu  sync.RWMutex
	timeout  time.Duration
	proxyURL string
	cookies  string
	fetcher  *antibot.Fetcher

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Jav321 {
	u, _ := url.Parse(defaultURL)
	p := &Jav321{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildFetcherLocked()
	return p
}

func newForTest(base string, fetcher *antibot.Fetcher) *Jav321 {
	u, _ := url.Parse(base)
	p := &Jav321{baseURL: u, timeout: defaultTimeout, priority: Priority, fetcher: fetcher}
	if p.fetcher == nil {
		p.rebuildFetcherLocked()
	}
	return p
}

func (p *Jav321) Name() string { return Name }

func (p *Jav321) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *Jav321) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *Jav321) Language() language.Tag { return language.Japanese }

func (p *Jav321) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *Jav321) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(norm.NFKC.String(id))
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		if parsed, err := p.ParseMovieIDFromURL(id); err == nil {
			return parsed
		}
	}
	if movieNumberRE.MatchString(id) {
		return strings.ToUpper(id)
	}
	return id
}

func (p *Jav321) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.TrimSpace(norm.NFKC.String(keyword)))
}

func (p *Jav321) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("jav321: parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("jav321: expected an absolute detail URL")
	}
	if !p.allowedHost(u.Hostname()) {
		return "", fmt.Errorf("jav321: unexpected detail host %q", u.Hostname())
	}
	if !p.allowedURL(u) {
		return "", fmt.Errorf("jav321: unexpected detail origin %q", u.String())
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "video" || !movieNumberRE.MatchString(segments[1]) {
		return "", fmt.Errorf("jav321: URL is not a /video/{number} detail URL")
	}
	return strings.ToUpper(segments[1]), nil
}

func (p *Jav321) allowedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	base := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host == base {
		return true
	}
	return (base == "jav321.com" && host == "www.jav321.com") ||
		(base == "www.jav321.com" && host == "jav321.com")
}

func (p *Jav321) allowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	if !p.allowedHost(u.Hostname()) {
		return false
	}
	if strings.EqualFold(p.baseURL.Scheme, "https") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	port := func(u *url.URL) string {
		if value := u.Port(); value != "" {
			return value
		}
		switch strings.ToLower(u.Scheme) {
		case "https":
			return "443"
		case "http":
			return "80"
		default:
			return ""
		}
	}
	return port(u) == port(p.baseURL)
}

func (p *Jav321) SetRequestTimeout(timeout time.Duration) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildFetcherLocked()
}

func (p *Jav321) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("jav321: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("jav321: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildFetcherLocked()
	return nil
}

// SetConfig accepts BASE_URL and COOKIES/COOKIE, matching the configuration
// convention documented in docs/PROVIDER_ACCESS.md.
func (p *Jav321) SetConfig(config mt.Config) error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	if config.Has("base_url") {
		raw, err := config.GetString("base_url")
		if err != nil {
			return fmt.Errorf("jav321: read base_url: %w", err)
		}
		u, err := p.parseBaseURL(raw)
		if err != nil {
			return err
		}
		p.baseURL = u
	}
	for _, key := range []string{"cookies", "cookie"} {
		if !config.Has(key) {
			continue
		}
		value, err := config.GetString(key)
		if err != nil {
			return fmt.Errorf("jav321: read %s: %w", key, err)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("jav321: %s must not contain CR/LF", key)
		}
		p.cookies = strings.TrimSpace(value)
		break
	}
	p.rebuildFetcherLocked()
	return nil
}

func (p *Jav321) parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("jav321: empty base_url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("jav321: parse base_url: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("jav321: base_url must be a bare https origin, got %q", raw)
	}
	if path := strings.TrimSuffix(u.Path, "/"); path != "" {
		return nil, fmt.Errorf("jav321: base_url must not contain a path, got %q", raw)
	}
	if !p.allowedHost(u.Hostname()) {
		return nil, fmt.Errorf("jav321: unsupported base_url host %q", u.Hostname())
	}
	u.Path = ""
	return u, nil
}

// rebuildFetcherLocked replaces the fetch layer. Callers must hold stateMu.
func (p *Jav321) rebuildFetcherLocked() {
	if p.fetcher != nil {
		p.fetcher.Close()
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.fetcher = antibot.New(antibot.Options{
		Timeout:      timeout,
		ProxyURL:     p.proxyURL,
		Cookies:      p.cookies,
		AllowURL:     p.allowedURL,
		MaxBodyBytes: maxPageBytes,
	})
}

func (p *Jav321) currentFetcher() *antibot.Fetcher {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.fetcher
}

func (p *Jav321) Fetch(rawURL string) (*http.Response, error) {
	req, err := p.newRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return p.send(req)
}

func (p *Jav321) newRequest(method, rawURL string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("jav321: parse URL: %w", err)
	}
	if !p.allowedURL(u) {
		return nil, fmt.Errorf("jav321: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("jav321: create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en;q=0.8,zh;q=0.6")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	return req, nil
}

// send performs the request through the antibot layer, retrying transient
// statuses. Challenge detection, TLS rotation and the Chromium fallback are
// handled by that layer.
func (p *Jav321) send(req *http.Request) (*http.Response, error) {
	fetcher := p.currentFetcher()
	if fetcher == nil {
		return nil, errors.New("jav321: fetch layer is not initialised")
	}
	var lastErr error
	for attempt := 1; attempt <= maxTransientAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepContext(req.Context(), time.Duration(attempt-1)*500*time.Millisecond); err != nil {
				return nil, err
			}
		}
		resp, err := fetcher.Do(req)
		if err != nil {
			lastErr = err
			if antibot.IsChallengeError(err) {
				return nil, fmt.Errorf("jav321: GET %s blocked by edge protection: %w", req.URL, err)
			}
			continue
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, req.URL)
		case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
			return resp, nil
		case isTransientStatus(resp.StatusCode):
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("jav321: GET %s: unexpected HTTP status %s", req.URL, resp.Status)
		default:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("jav321: GET %s: unexpected HTTP status %s", req.URL, resp.Status)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("jav321: request attempts exhausted")
	}
	return nil, lastErr
}

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Jav321) getDocument(rawURL string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("jav321: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("jav321: response exceeds %d bytes", maxPageBytes)
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("jav321: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func (p *Jav321) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("jav321: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	return p.searchDetail(id)
}

func (p *Jav321) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, finalURL, err := p.getDocument(rawURL)
	if err != nil {
		return nil, err
	}
	homepage := rawURL
	if _, err := p.ParseMovieIDFromURL(finalURL); err == nil {
		homepage = finalURL
	}
	return p.parseDetail(doc, id, homepage)
}

// parseDetail maps a Jav321 detail document.
//
// The live layout is a Bootstrap panel: a <h3> carrying the title followed by a
// nested <small> that repeats the compressed number and the cast, a metadata
// block of <b>label</b> value pairs, a video panel, and one free-text
// description panel.
func (p *Jav321) parseDetail(doc *goquery.Document, id, homepage string) (*model.MovieInfo, error) {
	heading := doc.Find("div.panel-heading h3").First()
	if heading.Length() == 0 {
		return nil, ErrMovieNotFound
	}
	title := cleanText(stripSelection(heading, "small").Text())
	if title == "" {
		return nil, ErrMovieNotFound
	}

	fields := readLabeledFields(doc)

	number := firstNonEmpty(fields["品番"])
	if match := numberInTitleRE.FindString(number); match != "" {
		number = match
	}
	number = strings.ToUpper(strings.TrimSpace(number))
	if number == "" {
		number = id
	}
	displayTitle := removeLeadingNumber(title, number)
	if displayTitle == "" {
		displayTitle = title
	}

	images := collectImages(doc, p.baseURL, id, number)
	cover := images.cover
	thumb := images.thumb
	previews := images.previews
	if videoURL := attrValue(doc.Find("video source[src]").First(), "src"); videoURL != "" {
		images.previewVideo = resolveURL(p.baseURL, videoURL)
	}
	if cover == "" {
		// The player poster is the package cover on pages that omit the <img>.
		cover = preferLargeCover(resolveImageURL(p.baseURL, attrValue(doc.Find("video[poster]").First(), "poster")))
	}
	if cover == "" && thumb != "" {
		// Pages that only expose the sample image still have a usable cover.
		cover = preferLargeCover(thumb)
	}
	if cover == "" {
		cover = resolveImageURL(p.baseURL, attrValue(doc.Find("meta[property='og:image']").First(), "content"))
	}
	if cover == "" {
		return nil, fmt.Errorf("jav321: detail page %s has no cover image", homepage)
	}
	if thumb == "" {
		thumb = cover
	}

	info := &model.MovieInfo{
		ID:              number,
		Number:          number,
		Title:           displayTitle,
		Summary:         detailSummary(doc),
		Provider:        p.Name(),
		Homepage:        homepage,
		Maker:           firstNonEmpty(fields["メーカー"]),
		Label:           firstNonEmpty(fields["レーベル"]),
		Series:          seriesName(doc),
		Actors:          pq.StringArray(fields["出演者"]),
		Genres:          pq.StringArray(fields["ジャンル"]),
		CoverURL:        cover,
		BigCoverURL:     cover,
		ThumbURL:        thumb,
		PreviewImages:   pq.StringArray(previews),
		PreviewVideoURL: images.previewVideo,
	}
	if score := parser.ParseScore(firstNonEmpty(fields["平均評価"])); score > 0 {
		info.Score = score
	}
	if release := firstNonEmpty(fields["配信開始日"]); release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if runtime := firstNonEmpty(fields["収録時間"]); runtime != "" {
		info.Runtime = parseRuntimeMinutes(runtime)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func (p *Jav321) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if !movieNumberRE.MatchString(keyword) {
		return nil, nil
	}
	info, err := p.searchDetail(keyword)
	if errors.Is(err, ErrMovieNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}

// searchDetail uses Jav321's own POST search form. The route is a compact
// site-specific slug (for example SSIS-033 -> /video/ssis00033), not the
// catalog number, so constructing /video/{input} directly misses valid works.
func (p *Jav321) searchDetail(keyword string) (*model.MovieInfo, error) {
	searchURL := *p.baseURL
	searchURL.Path = path.Join(searchURL.Path, "search")
	form := url.Values{"sn": {keyword}}
	req, err := p.newRequest(http.MethodPost, searchURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.send(req)
	if err != nil {
		if errors.Is(err, ErrMovieNotFound) {
			return nil, ErrMovieNotFound
		}
		return nil, fmt.Errorf("jav321: search %q: %w", keyword, err)
	}
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	_ = resp.Body.Close()
	if finalURL == "" {
		return nil, ErrMovieNotFound
	}
	if _, err := p.ParseMovieIDFromURL(finalURL); err != nil {
		return nil, ErrMovieNotFound
	}
	info, err := p.GetMovieInfoByURL(finalURL)
	if err != nil || !sameCatalogNumber(keyword, info.Number) {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func sameCatalogNumber(a, b string) bool {
	compact := func(s string) string {
		return strings.ToUpper(strings.NewReplacer("-", "", "_", "").Replace(strings.TrimSpace(s)))
	}
	return compact(a) == compact(b)
}

// readLabeledFields walks <b>label</b> value pairs inside the metadata panel.
// Values may be plain text, anchors, or a mix; every value is deduplicated.
func readLabeledFields(doc *goquery.Document) map[string][]string {
	fields := make(map[string][]string)
	var container *goquery.Selection
	doc.Find("b").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if strings.Contains(cleanText(node.Text()), "品番") {
			container = node.Parent()
			return false
		}
		return true
	})
	if container == nil {
		container = doc.Selection
	}
	current := ""
	container.Contents().Each(func(_ int, node *goquery.Selection) {
		raw := node.Get(0)
		if raw == nil {
			return
		}
		if raw.Type == html.ElementNode && raw.Data == "b" {
			current = strings.TrimSuffix(cleanText(node.Text()), ":")
			current = strings.TrimSuffix(current, "：")
			if current != "" {
				if _, ok := fields[current]; !ok {
					fields[current] = nil
				}
			}
			return
		}
		if current == "" {
			return
		}
		value := cleanLabelValue(node.Text())
		if value == "" {
			return
		}
		fields[current] = appendUnique(fields[current], value)
	})
	return fields
}

func cleanLabelValue(raw string) string {
	raw = cleanText(raw)
	raw = strings.TrimLeft(raw, ":： ")
	// The metadata panel concatenates a label, its value and the next label in
	// one text run when the value is not an anchor; cut at the next label.
	for _, label := range []string{"出演者", "メーカー", "品番", "配信開始日", "収録時間", "ジャンル", "お気に入り登録数", "平均評価", "レーベル", "シリーズ"} {
		if idx := strings.Index(raw, label); idx > 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

// detailSummary returns the site's free-text description. The first
// col-md-12 panel holds the video player and ad slots, so panels containing
// media or scripts are skipped.
func detailSummary(doc *goquery.Document) string {
	var summary string
	doc.Find("div.panel-body div.row div.col-md-12").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if node.Find("video, img, script, iframe, ins, source").Length() > 0 {
			return true
		}
		text := cleanText(node.Text())
		if len([]rune(text)) < 20 {
			return true
		}
		summary = text
		return false
	})
	return summary
}

func seriesName(doc *goquery.Document) string {
	var name string
	doc.Find("div.panel-heading a[href^='/snp/']").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if text := cleanText(node.Text()); text != "" {
			name = text
			return false
		}
		return true
	})
	return name
}

type imageSet struct {
	cover        string
	thumb        string
	previews     []string
	previewVideo string
}

// collectImages separates the package cover (pl.jpg), the thumbnail (ps.jpg)
// and the numbered gallery images (jp-N.jpg). Samples of other works use the
// same suffixes on other pages, so entries are matched against the current
// number when one is known.
func collectImages(doc *goquery.Document, base *url.URL, id, number string) imageSet {
	var set imageSet
	prefixes := []string{}
	for _, candidate := range []string{id, number} {
		if compact := compactNumber(candidate); compact != "" {
			prefixes = appendUnique(prefixes, compact)
		}
	}
	matches := func(filename string) bool {
		if len(prefixes) == 0 {
			return true
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(filename, prefix) {
				return true
			}
		}
		return false
	}
	type preview struct {
		order int
		url   string
	}
	var previews []preview
	doc.Find("img").Each(func(_ int, image *goquery.Selection) {
		raw := attrOrFirst(image, "data-src", "data-original", "src")
		resolved := resolveImageURL(base, raw)
		if resolved == "" {
			return
		}
		filename := strings.ToLower(path.Base(raw))
		if !matches(filename) {
			return
		}
		switch {
		case strings.Contains(filename, "pl.jpg"):
			if set.cover == "" {
				set.cover = resolved
			}
		case strings.Contains(filename, "ps.jpg"):
			if set.thumb == "" {
				set.thumb = resolved
			}
		default:
			if match := previewImageRE.FindStringSubmatch(filename); match != nil {
				order, _ := strconv.Atoi(match[1])
				previews = append(previews, preview{order: order, url: resolved})
			}
		}
	})
	sort.SliceStable(previews, func(i, j int) bool { return previews[i].order < previews[j].order })
	for _, item := range previews {
		set.previews = appendUnique(set.previews, item.url)
	}
	return set
}

// compactNumber reduces a catalog number to the alphanumeric form used in image
// file names (SSIS-033 -> ssis033, ssis00033 -> ssis00033).
func compactNumber(number string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(number) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func preferLargeCover(cover string) string {
	cover = strings.Replace(cover, "ps.jpg", "pl.jpg", 1)
	return cover
}

func removeLeadingNumber(title, number string) string {
	title = cleanText(title)
	number = cleanText(number)
	if title == "" || number == "" || len(title) < len(number) || !strings.EqualFold(title[:len(number)], number) {
		return title
	}
	rest := strings.TrimSpace(title[len(number):])
	rest = strings.TrimLeft(rest, " -—:：|")
	return cleanText(rest)
}

// stripSelection returns a copy of sel with the matching descendants removed.
func stripSelection(sel *goquery.Selection, selector string) *goquery.Selection {
	clone := sel.Clone()
	clone.Find(selector).Remove()
	return clone
}

func parseRuntimeMinutes(raw string) int {
	raw = cleanText(raw)
	if raw == "" {
		return 0
	}
	if match := runtimeMinutesRE.FindStringSubmatch(raw); match != nil {
		if minutes, err := strconv.Atoi(match[1]); err == nil {
			return minutes
		}
	}
	return parser.ParseRuntime(raw)
}

func resolveImageURL(base *url.URL, raw string) string {
	resolved := resolveURL(base, raw)
	if resolved == "" {
		return ""
	}
	resolved = strings.ReplaceAll(resolved, "pics.dmm.co.jp//", "pics.dmm.co.jp/")
	resolved = strings.ReplaceAll(resolved, "p.dmm.co.jp//", "p.dmm.co.jp/")
	if strings.HasPrefix(strings.ToLower(resolved), "http://") {
		return "https://" + resolved[len("http://"):]
	}
	return resolved
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(norm.NFKC.String(value))), " ")
}

func attrValue(node *goquery.Selection, key string) string {
	value, _ := node.Attr(key)
	return strings.TrimSpace(value)
}

func attrOrFirst(node *goquery.Selection, keys ...string) string {
	for _, key := range keys {
		if value := attrValue(node, key); value != "" {
			return value
		}
	}
	return ""
}

func resolveURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value = cleanText(value); value != "" {
			return value
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func init() {
	mt.Register(Name, New)
}
