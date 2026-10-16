// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source or
// fixtures are vendored. See NOTICE.md for attribution.
package xcity

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
	Name           = "XCity"
	Priority       = 1000 - 30
	defaultURL     = "https://xcity.jp"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound                         = errors.New("xcity: movie not found")
	_                mt.MovieProvider        = (*XCity)(nil)
	_                mt.MovieSearcher        = (*XCity)(nil)
	_                mt.Fetcher              = (*XCity)(nil)
	_                mt.ProxySetter          = (*XCity)(nil)
	_                mt.RequestTimeoutSetter = (*XCity)(nil)
)

type XCity struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

type detail struct {
	info *model.MovieInfo
}

func New() *XCity {
	u, _ := url.Parse(defaultURL)
	p := &XCity{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *XCity {
	u, _ := url.Parse(base)
	p := &XCity{baseURL: u, timeout: defaultTimeout, priority: Priority}
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

func (p *XCity) Name() string { return Name }
func (p *XCity) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}
func (p *XCity) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}
func (p *XCity) Language() language.Tag { return language.Japanese }
func (p *XCity) URL() *url.URL          { u := *p.baseURL; return &u }
func (p *XCity) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}
func (p *XCity) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (p *XCity) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	base := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host != base && !(base == "xcity.jp" && host == "www.xcity.jp") && !(base == "www.xcity.jp" && host == "xcity.jp") {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}

func (p *XCity) ParseMovieIDFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("xcity: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("xcity: unexpected detail origin %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("xcity: detail URL must not contain query or fragment")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "titles" || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("xcity: invalid detail path %q", u.Path)
	}
	return parts[1], nil
}

func (p *XCity) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("xcity: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("xcity: refusing cross-origin redirect to %q", req.URL)
	}
	if _, err := p.ParseMovieIDFromURL(req.URL.String()); err != nil {
		// Search requests are allowed to redirect within the same origin.
		if req.URL.Path != "/result_published/" {
			return err
		}
	}
	return nil
}

func (p *XCity) SetRequestTimeout(v time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if v <= 0 {
		v = defaultTimeout
	}
	p.timeout = v
	p.rebuildClientLocked()
}
func (p *XCity) SetProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("xcity: invalid proxy URL %q", raw)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("xcity: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = raw
	p.rebuildClientLocked()
	return nil
}
func (p *XCity) rebuildClientLocked() {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		if u, err := url.Parse(p.proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: p.checkRedirect}
}
func (p *XCity) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}
func (p *XCity) Fetch(raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("xcity: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("xcity: refusing request to unexpected origin %q", raw)
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("xcity: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	req.AddCookie(&http.Cookie{Name: "adc", Value: "1", Path: "/"})
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("xcity: GET %s: %w", raw, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("xcity: unexpected HTTP status %s", resp.Status)
	}
	return resp, nil
}
func (p *XCity) getHTML(raw string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(raw)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	finalURL := raw
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("xcity: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("xcity: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("xcity: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func normalizeNumber(s string) string {
	s = strings.ToUpper(norm.NFKC.String(strings.TrimSpace(s)))
	return regexp.MustCompile(`[-_ .　]+`).ReplaceAllString(s, "")
}
func (p *XCity) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = "/result_published/"
	q := url.Values{}
	q.Set("q", strings.ReplaceAll(normalizeNumber(keyword), "-", ""))
	u.RawQuery = q.Encode()
	return u.String()
}
func absoluteURL(base *url.URL, raw string) string {
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
func uniqueTexts(doc *goquery.Document, selector string) []string {
	var out []string
	seen := map[string]bool{}
	doc.Find(selector).Each(func(_ int, n *goquery.Selection) {
		v := strings.Join(strings.Fields(strings.TrimSpace(n.Text())), " ")
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	})
	return out
}
func parseRuntime(s string) int {
	m := regexp.MustCompile(`\d+`).FindString(s)
	if m == "" {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(m, "%d", &n)
	return n
}

func (p *XCity) parseDetail(raw string) (*detail, string, error) {
	id, err := p.ParseMovieIDFromURL(raw)
	if err != nil {
		return nil, "", err
	}
	doc, finalURL, err := p.getHTML(raw)
	if err != nil {
		return nil, "", err
	}
	finalID, err := p.ParseMovieIDFromURL(finalURL)
	if err != nil || finalID != id {
		return nil, finalURL, fmt.Errorf("%w: detail route changed", ErrMovieNotFound)
	}

	title := strings.Join(strings.Fields(strings.TrimSpace(doc.Find("h1").First().Text())), " ")
	number := strings.TrimSpace(doc.Find("span.kopin").First().Text())
	if title == "" || number == "" {
		return nil, finalURL, ErrMovieNotFound
	}
	cover := strings.TrimSpace(doc.Find("img.photo").First().AttrOr("src", ""))
	if cover == "" {
		cover = strings.TrimSpace(doc.Find("a.fancy_image").First().AttrOr("href", ""))
	}
	cover = absoluteURL(p.baseURL, cover)
	actors := uniqueTexts(doc, `a[href*="/idol/"]`)
	tags := uniqueTexts(doc, `a[href*="/genres/"]`)
	studio := strings.TrimSpace(doc.Find(`a[href*="/studios/"]`).First().Text())
	release := strings.TrimSpace(doc.Find(`li:has(span)`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(s.Find("span").First().Text(), "発売日")
	}).First().Clone().Children().Remove().End().Text())
	runtime := strings.TrimSpace(doc.Find(`li:has(span)`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(s.Find("span").First().Text(), "収録時間")
	}).First().Clone().Children().Remove().End().Text())
	info := &model.MovieInfo{
		ID: id, Number: number, Title: title, Provider: p.Name(), Homepage: raw,
		Actors: pq.StringArray(actors), Maker: studio, Genres: pq.StringArray(tags),
		CoverURL: cover, ThumbURL: cover, PreviewImages: pq.StringArray{},
		Runtime: parseRuntime(runtime),
	}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if !info.IsValid() {
		return nil, finalURL, ErrMovieNotFound
	}
	return &detail{info: info}, finalURL, nil
}

func (p *XCity) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	query := normalizeNumber(keyword)
	if query == "" {
		return nil, nil
	}
	doc, _, err := p.getHTML(p.searchURL(keyword))
	if err != nil {
		return nil, err
	}
	var candidates []string
	seen := map[string]bool{}
	doc.Find(`table.resultList a[href*="/titles/"]`).Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		raw := absoluteURL(p.baseURL, href)
		id, e := p.ParseMovieIDFromURL(raw)
		if e != nil || seen[id] {
			return
		}
		seen[id] = true
		candidates = append(candidates, raw)
	})
	results := make([]*model.MovieSearchResult, 0, len(candidates))
	for _, raw := range candidates {
		d, _, e := p.parseDetail(raw)
		if e != nil || normalizeNumber(d.info.Number) != query {
			continue
		}
		results = append(results, d.info.ToSearchResult())
	}
	return results, nil
}

func (p *XCity) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("xcity: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	// IDs from parsed detail URLs are opaque slugs; catalog numbers must go through exact search.
	if strings.ContainsAny(id, "/\\?#") {
		return nil, fmt.Errorf("xcity: invalid movie ID %q", id)
	}
	if regexp.MustCompile(`(?i)[a-z]`).MatchString(id) && !strings.Contains(id, "-") {
		u := *p.baseURL
		u.Path = path.Join("/titles", id)
		return p.GetMovieInfoByURL(u.String())
	}
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	query := normalizeNumber(id)
	for _, r := range results {
		if normalizeNumber(r.Number) == query {
			return p.GetMovieInfoByURL(r.Homepage)
		}
	}
	return nil, ErrMovieNotFound
}
func (p *XCity) GetMovieInfoByURL(raw string) (*model.MovieInfo, error) {
	d, _, err := p.parseDetail(raw)
	if err != nil {
		return nil, err
	}
	return d.info, nil
}

func init() { mt.Register(Name, New) }
