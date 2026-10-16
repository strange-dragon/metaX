// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source or
// fixtures are vendored. See ../../NOTICE.md for attribution.
package freejavbt

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	Name           = "Freejavbt"
	Priority       = 1000 - 22
	defaultURL     = "https://freejavbt.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound = errors.New("freejavbt: movie not found")
	numberTokenRE    = regexp.MustCompile(`(?i)^[a-z]+[- ]?\d+[a-z0-9_-]*$`)
	leadingNumberRE  = regexp.MustCompile(`(?i)^([a-z]+[- ]?\d+[a-z0-9_-]*)`)
	validSlugRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

var (
	_ mt.MovieProvider        = (*Freejavbt)(nil)
	_ mt.MovieSearcher        = (*Freejavbt)(nil)
	_ mt.Fetcher              = (*Freejavbt)(nil)
	_ mt.ProxySetter          = (*Freejavbt)(nil)
	_ mt.RequestTimeoutSetter = (*Freejavbt)(nil)
)

type Freejavbt struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Freejavbt {
	u, _ := url.Parse(defaultURL)
	p := &Freejavbt{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Freejavbt {
	u, _ := url.Parse(base)
	p := &Freejavbt{baseURL: u, timeout: defaultTimeout, priority: Priority}
	if client == nil {
		p.rebuildClientLocked()
		return p
	}
	c := *client
	c.CheckRedirect = p.checkRedirect
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	p.client = &c
	return p
}

func (p *Freejavbt) Name() string { return Name }

func (p *Freejavbt) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *Freejavbt) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *Freejavbt) Language() language.Tag { return language.Japanese }

func (p *Freejavbt) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *Freejavbt) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}

func (p *Freejavbt) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (p *Freejavbt) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("freejavbt: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("freejavbt: unexpected detail origin %q", rawURL)
	}
	trimmed := strings.Trim(u.Path, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") || strings.EqualFold(trimmed, "search") {
		return "", fmt.Errorf("freejavbt: unsupported detail path %q", u.Path)
	}
	id, err := url.PathUnescape(path.Base(u.Path))
	if err != nil || !validSlugRE.MatchString(id) || id == "." || id == ".." {
		return "", fmt.Errorf("freejavbt: invalid detail ID in path %q", u.Path)
	}
	return id, nil
}

func (p *Freejavbt) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	baseHost := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host != baseHost {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (p *Freejavbt) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("freejavbt: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("freejavbt: refusing redirect to unexpected origin %q", req.URL)
	}
	return nil
}

func (p *Freejavbt) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *Freejavbt) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("freejavbt: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("freejavbt: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *Freejavbt) rebuildClientLocked() {
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		if proxyURL, err := url.Parse(p.proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: p.checkRedirect}
}

func (p *Freejavbt) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *Freejavbt) Fetch(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("freejavbt: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("freejavbt: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("freejavbt: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("freejavbt: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("freejavbt: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *Freejavbt) getHTML(rawURL string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("freejavbt: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("freejavbt: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("freejavbt: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func normalizeNumber(value string) string {
	value = strings.ToUpper(strings.TrimSpace(norm.NFKC.String(value)))
	return strings.NewReplacer("-", "", " ", "", "\t", "").Replace(value)
}

func numberFromText(value string) string {
	match := leadingNumberRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func lastPathSegment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return path.Base(strings.TrimRight(u.Path, "/"))
}

func (p *Freejavbt) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "search", url.PathEscape(keyword))
	return u.String()
}

func (p *Freejavbt) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" || !numberTokenRE.MatchString(keyword) {
		return nil, nil
	}
	doc, _, err := p.getHTML(p.searchURL(keyword))
	if err != nil {
		return nil, err
	}
	query := normalizeNumber(keyword)
	results := make([]*model.MovieSearchResult, 0, 4)
	seen := make(map[string]struct{})
	doc.Find("div.category-page.video-list-item").Each(func(_ int, card *goquery.Selection) {
		href, ok := card.Find("a[href]").First().Attr("href")
		if !ok || href == "" {
			return
		}
		homepage := p.baseURL.ResolveReference(mustParseURL(href)).String()
		if _, exists := seen[homepage]; exists {
			return
		}
		candidate := lastPathSegment(homepage)
		if normalizeNumber(candidate) != query {
			return
		}
		title := strings.TrimSpace(card.Find("h1,h2,h3,h4,.entry-title").First().Text())
		if title == "" {
			title = strings.TrimSpace(card.Find("a[href]").First().Text())
		}
		if title == "" {
			return
		}
		id, err := p.ParseMovieIDFromURL(homepage)
		if err != nil {
			return
		}
		cover, _ := card.Find("img").First().Attr("data-src")
		if strings.TrimSpace(cover) == "" {
			cover, _ = card.Find("img").First().Attr("src")
		}
		coverURL := resolveURL(p.baseURL, cover)
		displayTitle := strings.TrimSpace(strings.TrimPrefix(title, numberFromText(title)))
		displayTitle = strings.TrimLeft(displayTitle, " -—:：|")
		if displayTitle == "" {
			displayTitle = title
		}
		result := &model.MovieSearchResult{
			ID: id, Number: numberFromText(title), Title: displayTitle,
			Provider: p.Name(), Homepage: homepage, CoverURL: coverURL, ThumbURL: coverURL,
		}
		if result.Number == "" {
			result.Number = candidate
		}
		if !result.IsValid() {
			return
		}
		seen[homepage] = struct{}{}
		results = append(results, result)
	})
	return results, nil
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return u
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

func fieldValues(doc *goquery.Document, label string) []string {
	var values []string
	doc.Find("strong").Each(func(_ int, strong *goquery.Selection) {
		if !strings.Contains(strings.TrimSpace(strong.Text()), label) {
			return
		}
		parent := strong.Parent()
		if parent.Length() == 0 {
			parent = strong
		}
		before := len(values)
		parent.Find("a").Each(func(_ int, a *goquery.Selection) {
			if value := strings.TrimSpace(a.Text()); value != "" {
				values = appendUnique(values, value)
			}
		})
		if len(values) == before {
			value := strings.TrimSpace(strings.TrimPrefix(parent.Text(), strong.Text()))
			if value != "" {
				values = appendUnique(values, value)
			}
		}
	})
	return values
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (p *Freejavbt) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = p.NormalizeMovieID(id)
	if id == "" {
		return nil, fmt.Errorf("freejavbt: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	if strings.ContainsAny(id, "/\\?#") || !validSlugRE.MatchString(id) || id == "." || id == ".." {
		return nil, fmt.Errorf("freejavbt: invalid movie ID %q", id)
	}
	if numberTokenRE.MatchString(id) {
		results, err := p.SearchMovie(id)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, ErrMovieNotFound
		}
		return p.GetMovieInfoByURL(results[0].Homepage)
	}
	u := *p.baseURL
	u.Path = "/" + id
	return p.GetMovieInfoByURL(u.String())
}

func (p *Freejavbt) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, finalURL, err := p.getHTML(rawURL)
	if err != nil {
		return nil, err
	}
	finalID, err := p.ParseMovieIDFromURL(finalURL)
	if err != nil || finalID != id {
		return nil, fmt.Errorf("%w: detail URL redirected from %q to %q", ErrMovieNotFound, rawURL, finalURL)
	}
	title := strings.TrimSpace(doc.Find("h1.entry-title").First().Text())
	if title == "" {
		return nil, ErrMovieNotFound
	}
	number := numberFromText(title)
	if number == "" {
		return nil, fmt.Errorf("%w: detail title has no catalog number", ErrMovieNotFound)
	}
	if normalizeNumber(lastPathSegment(rawURL)) != normalizeNumber(number) {
		return nil, fmt.Errorf("%w: URL identifier does not match title number", ErrMovieNotFound)
	}
	cover := strings.TrimSpace(doc.Find("article img.alignnone").First().AttrOr("src", ""))
	if cover == "" {
		cover = strings.TrimSpace(doc.Find("meta[property='og:image']").First().AttrOr("content", ""))
	}
	cover = resolveURL(p.baseURL, cover)
	var samples []string
	doc.Find("article a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if strings.HasSuffix(strings.ToLower(strings.Split(href, "?")[0]), ".jpg") {
			if resolved := resolveURL(p.baseURL, href); resolved != "" && resolved != cover {
				samples = appendUnique(samples, resolved)
			}
		}
	})
	releaseValues := fieldValues(doc, "配信日")
	if len(releaseValues) == 0 {
		releaseValues = fieldValues(doc, "発売日")
	}
	runtimeValues := fieldValues(doc, "収録時間")
	info := &model.MovieInfo{
		ID: id, Number: number, Title: title, Provider: p.Name(), Homepage: rawURL,
		Actors:   pq.StringArray(fieldValues(doc, "出演者")),
		Maker:    firstValue(fieldValues(doc, "メーカー")),
		Label:    firstValue(fieldValues(doc, "レーベル")),
		Series:   firstValue(fieldValues(doc, "シリーズ")),
		Genres:   pq.StringArray(fieldValues(doc, "ジャンル")),
		CoverURL: cover, ThumbURL: cover, PreviewImages: pq.StringArray(samples),
	}
	if len(releaseValues) > 0 {
		info.ReleaseDate = parser.ParseDate(releaseValues[0])
	}
	if len(runtimeValues) > 0 {
		info.Runtime = parser.ParseRuntime(runtimeValues[0])
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func init() {
	mt.Register(Name, New)
}
