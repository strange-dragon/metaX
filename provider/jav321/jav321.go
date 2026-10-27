// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for metaX's Provider API; no Amane source file or
// fixture is vendored. See ../../NOTICE.md for attribution.
package jav321

import (
	"crypto/tls"
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

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	Name           = "Jav321"
	Priority       = 1000 - 11
	defaultURL     = "https://www.jav321.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound = errors.New("jav321: movie not found")
	movieNumberRE    = regexp.MustCompile(`(?i)^(?:[A-Z0-9]{1,16}[-_][A-Z0-9]{1,24}(?:[-_][A-Z0-9]+)*|[A-Z]{2,12}[0-9]{2,12}|FC2[-_]?PPV[-_]?[0-9]+)$`)
	numberInTitleRE  = regexp.MustCompile(`(?i)(FC2[-_]?PPV[-_]?[0-9]+|[A-Z0-9]{1,16}[-_][A-Z0-9]{1,24}(?:[-_][A-Z0-9]+)*)`)
)

var (
	_ mt.MovieProvider        = (*Jav321)(nil)
	_ mt.MovieSearcher        = (*Jav321)(nil)
	_ mt.Fetcher              = (*Jav321)(nil)
	_ mt.ProxySetter          = (*Jav321)(nil)
	_ mt.RequestTimeoutSetter = (*Jav321)(nil)
)

// Jav321 implements the metaX movie Provider contract for jav321.com.
type Jav321 struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Jav321 {
	u, _ := url.Parse(defaultURL)
	p := &Jav321{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Jav321 {
	u, _ := url.Parse(base)
	p := &Jav321{baseURL: u, timeout: defaultTimeout, priority: Priority, client: client}
	if p.client == nil {
		p.rebuildClientLocked()
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
	if !p.allowedRedirectURL(u) {
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

func (p *Jav321) allowedRedirectURL(u *url.URL) bool {
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
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
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
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *Jav321) rebuildClientLocked() {
	var transport *http.Transport
	if baseTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = baseTransport.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.Proxy = http.ProxyFromEnvironment
	// Match Amane's scraper HTTP behavior: some Jav321 deployments present an
	// invalid certificate, so do not let certificate-name/chain issues prevent metadata scraping.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	if p.proxyURL != "" {
		if proxyURL, err := url.Parse(p.proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{
		Transport: httpretry.WrapTransport(transport),
		Timeout:   timeout,
	}
}

func (p *Jav321) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *Jav321) Fetch(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jav321: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en;q=0.8,zh;q=0.6")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("jav321: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("jav321: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *Jav321) getHTML(rawURL string) (*goquery.Document, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("jav321: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("jav321: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("jav321: parse HTML: %w", err)
	}
	return doc, nil
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
	doc, err := p.getHTML(rawURL)
	if err != nil {
		return nil, err
	}

	title := cleanText(doc.Find("div.panel-heading h3").First().Text())
	if title == "" {
		return nil, ErrMovieNotFound
	}
	number := id
	if match := numberInTitleRE.FindString(cleanText(doc.Find("div.panel-heading h3 small").First().Text())); match != "" {
		number = strings.ToUpper(match)
	}
	if match := numberInTitleRE.FindString(title); match != "" {
		number = strings.ToUpper(match)
	}
	displayTitle := removeLeadingNumber(title, number)
	if displayTitle == title && number != "" {
		displayTitle = cleanText(regexp.MustCompile(`(?i)`+regexp.QuoteMeta(number)).ReplaceAllString(title, " "))
	}
	if displayTitle == "" {
		displayTitle = title
	}

	actors := collectTexts(doc, "b", "出演者", "a")
	studio := collectTexts(doc, "b", "メーカー", "a")
	genres := collectTexts(doc, "b", "ジャンル", "a")
	releaseText := collectFollowingText(doc, "配信開始日")
	runtimeText := collectFollowingText(doc, "収録時間")
	plot := cleanText(doc.Find("div.panel-body div.row div.col-md-12").First().Text())
	images := doc.Find("img")
	var cover, thumb string
	images.Each(func(_ int, image *goquery.Selection) {
		raw := attrOrFirst(image, "data-src", "data-original", "src")
		resolved := resolveImageURL(p.baseURL, raw)
		if resolved == "" {
			return
		}
		filename := strings.ToLower(path.Base(raw))
		if !strings.HasPrefix(filename, strings.ToLower(id)) {
			return
		}
		switch {
		case strings.Contains(filename, "pl.jpg"):
			cover = resolved
		case strings.Contains(filename, "ps.jpg") && thumb == "":
			thumb = resolved
		case strings.Contains(filename, "jp-"):
			// Preview galleries are exposed separately from the package cover.
		}
	})
	if cover == "" {
		cover = resolveImageURL(p.baseURL, attrOrFirst(images.First(), "data-src", "data-original", "src"))
	}
	if thumb == "" {
		thumb = cover
	}
	var previews []string
	images.Each(func(_ int, image *goquery.Selection) {
		raw := attrOrFirst(image, "data-src", "data-original", "src")
		filename := strings.ToLower(path.Base(raw))
		if strings.HasPrefix(filename, strings.ToLower(id)) && strings.Contains(filename, "jp-") {
			previews = appendUnique(previews, resolveImageURL(p.baseURL, raw))
		}
	})
	if cover == "" {
		cover = resolveURL(p.baseURL, attrValue(doc.Find("meta[property='og:image']").First(), "content"))
	}
	if cover == "" {
		return nil, fmt.Errorf("jav321: detail page %s has no cover image", rawURL)
	}

	info := &model.MovieInfo{
		ID:            number,
		Number:        number,
		Title:         displayTitle,
		Summary:       plot,
		Provider:      p.Name(),
		Homepage:      rawURL,
		Maker:         firstNonEmpty(studio),
		Actors:        pq.StringArray(actors),
		Genres:        pq.StringArray(genres),
		CoverURL:      cover,
		ThumbURL:      thumb,
		PreviewImages: pq.StringArray(previews),
	}
	if strings.TrimSpace(releaseText) != "" {
		info.ReleaseDate = parser.ParseDate(releaseText)
	}
	if strings.TrimSpace(runtimeText) != "" {
		info.Runtime = parser.ParseRuntime(runtimeText)
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
	u := *p.baseURL
	u.Path = path.Join(u.Path, "search")
	form := url.Values{"sn": {keyword}}
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("jav321: create search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("jav321: search %q: %w", keyword, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("jav321: search %q: unexpected HTTP status %s", keyword, resp.Status)
	}
	finalURL := resp.Request.URL.String()
	_, err = p.ParseMovieIDFromURL(finalURL)
	if err != nil {
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

func resolveImageURL(base *url.URL, raw string) string {
	resolved := resolveURL(base, raw)
	resolved = strings.Replace(resolved, "pics.dmm.co.jp//", "pics.dmm.co.jp/", 1)
	if strings.HasPrefix(strings.ToLower(resolved), "http://pics.dmm.co.jp/") {
		return "https://" + strings.TrimPrefix(resolved, "http://")
	}
	return resolved
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

func collectTexts(doc *goquery.Document, labelSelector, label, valueSelector string) []string {
	var results []string
	doc.Find(labelSelector).Each(func(_ int, node *goquery.Selection) {
		if !strings.Contains(cleanText(node.Text()), label) {
			return
		}
		node.Parent().Find(valueSelector).Each(func(_ int, value *goquery.Selection) {
			if text := cleanText(value.Text()); text != "" {
				results = appendUnique(results, text)
			}
		})
	})
	return results
}

func collectFollowingText(doc *goquery.Document, label string) string {
	var result string
	doc.Find("b").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if !strings.Contains(cleanText(node.Text()), label) {
			return true
		}
		result = cleanText(node.Parent().Text())
		result = strings.TrimLeft(strings.TrimSpace(strings.TrimPrefix(result, label)), ":： ")
		return false
	})
	return result
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
