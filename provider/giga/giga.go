// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source or
// fixtures are vendored. See ../../NOTICE.md for attribution.
package giga

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	Name           = "GIGA"
	Priority       = 1000 - 23
	defaultURL     = "https://www.giga-web.jp"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
	ageGatePath    = "/cookie_set.php"
	sessionCookie  = "PHPSESSID"
)

var (
	ErrMovieNotFound   = errors.New("giga: movie not found")
	ErrAgeVerification = errors.New("giga: age-verification session was not accepted")
	productIDRE        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	numberTokenRE      = regexp.MustCompile(`(?i)^[A-Za-z0-9]+[-_ ]?\d+[A-Za-z0-9_-]*$`)
)

var (
	_ mt.MovieProvider        = (*Giga)(nil)
	_ mt.MovieSearcher        = (*Giga)(nil)
	_ mt.Fetcher              = (*Giga)(nil)
	_ mt.ProxySetter          = (*Giga)(nil)
	_ mt.RequestTimeoutSetter = (*Giga)(nil)
)

type Giga struct {
	baseURL *url.URL

	// The site's age-gate flow mutates the session cookie. Serialize all
	// requests, not only session initialization, to avoid overwriting it.
	requestMu      sync.Mutex
	client         *http.Client
	ageClient      *http.Client
	jar            http.CookieJar
	timeout        time.Duration
	proxyURL       string
	sessionClaimed bool

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Giga {
	u, _ := url.Parse(defaultURL)
	jar, _ := cookiejar.New(nil)
	p := &Giga{baseURL: u, jar: jar, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Giga {
	u, _ := url.Parse(base)
	jar, _ := cookiejar.New(nil)
	p := &Giga{baseURL: u, jar: jar, timeout: defaultTimeout, priority: Priority}
	if client == nil {
		p.rebuildClientLocked()
		return p
	}
	c := *client
	c.Jar = jar
	c.CheckRedirect = p.checkRedirect
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	p.client = &c
	ageClient := c
	ageClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	p.ageClient = &ageClient
	return p
}

func (p *Giga) Name() string { return Name }

func (p *Giga) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *Giga) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *Giga) Language() language.Tag { return language.Japanese }

func (p *Giga) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *Giga) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}

func (p *Giga) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (p *Giga) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("giga: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("giga: unexpected detail origin %q", rawURL)
	}
	if u.Path != "/product/index.php" {
		return "", fmt.Errorf("giga: unsupported product path %q", u.Path)
	}
	id := strings.TrimSpace(u.Query().Get("product_id"))
	if !productIDRE.MatchString(id) {
		return "", fmt.Errorf("giga: invalid product ID in URL")
	}
	return id, nil
}

func (p *Giga) validOrigin(u *url.URL) bool {
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

func (p *Giga) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("giga: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("giga: refusing redirect to unexpected origin %q", req.URL)
	}
	return nil
}

func (p *Giga) SetRequestTimeout(timeout time.Duration) {
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *Giga) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("giga: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("giga: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *Giga) rebuildClientLocked() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
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
	p.client = &http.Client{Transport: transport, Timeout: timeout, Jar: p.jar, CheckRedirect: p.checkRedirect}
	p.ageClient = &http.Client{
		Transport: transport, Timeout: timeout, Jar: p.jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (p *Giga) requestAgeGateLocked() (bool, error) {
	u := *p.baseURL
	u.Path = ageGatePath
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false, fmt.Errorf("giga: create age-gate request: %w", err)
	}
	p.setHeaders(req)
	resp, err := p.ageClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("giga: request age gate: %w", err)
	}
	defer resp.Body.Close()
	issuedPrefix := sessionCookie + "="
	deletedPrefix := sessionCookie + "=deleted"
	for _, value := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(value, issuedPrefix) && !strings.HasPrefix(value, deletedPrefix) {
			return true, nil
		}
	}
	return false, nil
}

func (p *Giga) claimSessionLocked() error {
	for attempt := 0; attempt < 2; attempt++ {
		issued, err := p.requestAgeGateLocked()
		if err != nil {
			return err
		}
		if issued {
			p.sessionClaimed = true
			return nil
		}
	}
	p.sessionClaimed = false
	return ErrAgeVerification
}

func isAgeGate(body []byte) bool {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	return doc.Find("a[href*='"+ageGatePath+"']").Length() > 0
}

func (p *Giga) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")
}

func (p *Giga) fetchLocked(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("giga: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("giga: refusing request to unexpected origin %q", rawURL)
	}
	if !p.sessionClaimed {
		if err := p.claimSessionLocked(); err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("giga: create request: %w", err)
		}
		p.setHeaders(req)
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("giga: GET %s: %w", rawURL, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("giga: read response: %w", readErr)
		}
		if len(body) > maxPageBytes {
			return nil, fmt.Errorf("giga: response exceeds %d bytes", maxPageBytes)
		}
		if isAgeGate(body) {
			p.sessionClaimed = false
			if attempt == 0 {
				if err := p.claimSessionLocked(); err != nil {
					return nil, err
				}
				continue
			}
			return nil, ErrAgeVerification
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("giga: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		return resp, nil
	}
	return nil, ErrAgeVerification
}

func (p *Giga) Fetch(rawURL string) (*http.Response, error) {
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	return p.fetchLocked(rawURL)
}

func (p *Giga) getHTML(rawURL string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("giga: read HTML: %w", err)
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("giga: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func normalizeNumber(value string) string {
	value = strings.ToUpper(strings.TrimSpace(norm.NFKC.String(value)))
	return strings.NewReplacer("-", "", " ", "", "\t", "").Replace(value)
}

func (p *Giga) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "search") + "/"
	q := u.Query()
	q.Set("keyword", keyword)
	u.RawQuery = q.Encode()
	return u.String()
}

func (p *Giga) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}
	doc, _, err := p.getHTML(p.searchURL(keyword))
	if err != nil {
		return nil, err
	}
	var candidates []string
	seen := make(map[string]struct{})
	doc.Find("div.thumBox a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.Contains(href, "/product/index.php") {
			return
		}
		ref, err := url.Parse(strings.TrimSpace(href))
		if err != nil {
			return
		}
		homepage := p.baseURL.ResolveReference(ref).String()
		if _, exists := seen[homepage]; exists {
			return
		}
		if _, err := p.ParseMovieIDFromURL(homepage); err != nil {
			return
		}
		seen[homepage] = struct{}{}
		candidates = append(candidates, homepage)
	})
	results := make([]*model.MovieSearchResult, 0, 1)
	query := normalizeNumber(keyword)
	for _, homepage := range candidates {
		info, err := p.GetMovieInfoByURL(homepage)
		if err != nil {
			if errors.Is(err, ErrMovieNotFound) {
				continue
			}
			return nil, err
		}
		if normalizeNumber(info.Number) != query {
			continue
		}
		result := info.ToSearchResult()
		if result.IsValid() {
			results = append(results, result)
		}
		break
	}
	return results, nil
}

func (p *Giga) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = p.NormalizeMovieID(id)
	if id == "" || !productIDRE.MatchString(id) {
		return nil, fmt.Errorf("giga: invalid product ID %q", id)
	}
	u := *p.baseURL
	u.Path = "/product/index.php"
	q := u.Query()
	q.Set("product_id", id)
	u.RawQuery = q.Encode()
	return p.GetMovieInfoByURL(u.String())
}

func (p *Giga) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
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
	title := strings.TrimSpace(doc.Find("#works_pic h5").First().Text())
	number := strings.TrimSpace(doc.Find("#works_txt dl").FilterFunction(func(_ int, dl *goquery.Selection) bool {
		return strings.TrimSpace(dl.Find("dt").First().Text()) == "作品番号"
	}).Find("dd").First().Text())
	if title == "" || number == "" {
		return nil, ErrMovieNotFound
	}
	actors := uniqueTexts(doc.Find("#works_txt a[href*='actor_id']"))
	cover := resolveURL(p.baseURL, firstAttr(doc.Find("#works_pic img").First(), "data-src", "src"))
	samples := make([]string, 0)
	doc.Find(".gasatsu_images_pc img").Each(func(_ int, img *goquery.Selection) {
		if src := firstAttr(img, "data-src", "src"); src != "" {
			if resolved := resolveURL(p.baseURL, src); resolved != "" && resolved != cover {
				samples = appendUnique(samples, resolved)
			}
		}
	})
	release := fieldText(doc, "DVDリリース日")
	runtime := fieldText(doc, "収録時間")
	info := &model.MovieInfo{
		ID: id, Number: number, Title: title, Provider: p.Name(), Homepage: rawURL,
		Maker: "GIGA", Actors: pq.StringArray(actors), CoverURL: cover, ThumbURL: cover,
		PreviewImages: pq.StringArray(samples), Genres: pq.StringArray{},
	}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if runtime != "" {
		info.Runtime = parser.ParseRuntime(runtime)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func fieldText(doc *goquery.Document, label string) string {
	var value string
	doc.Find("#works_txt dl").EachWithBreak(func(_ int, dl *goquery.Selection) bool {
		if strings.TrimSpace(dl.Find("dt").First().Text()) != label {
			return true
		}
		value = strings.TrimSpace(dl.Find("dd").First().Text())
		return false
	})
	return value
}

func uniqueTexts(selection *goquery.Selection) []string {
	var values []string
	selection.Each(func(_ int, node *goquery.Selection) {
		if value := strings.TrimSpace(node.Text()); value != "" {
			values = appendUnique(values, value)
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

func firstAttr(node *goquery.Selection, attrs ...string) string {
	for _, name := range attrs {
		if value, ok := node.Attr(name); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
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

func init() {
	mt.Register(Name, New)
}
