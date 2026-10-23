// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for metaX's Provider API; no Amane source or
// fixtures are vendored. See ../../NOTICE.md for attribution.
package iqqtv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
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
	Name           = "IQQTV"
	Priority       = 1000 - 24
	defaultURL     = "https://iqq5.xyz"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound  = errors.New("iqqtv: movie not found")
	ErrUnsafeRedirect = errors.New("iqqtv: unsafe redirect")
	excludedWords     = []string{"克破", "无码破解", "無碼破解", "无码流出", "無碼流出"}
	validPathPartRE   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	catalogNumberRE   = regexp.MustCompile(`(?i)^[A-Za-z0-9]+[-_ ]?\d+[A-Za-z0-9_-]*$`)
)

var (
	_ mt.MovieProvider        = (*IQQTV)(nil)
	_ mt.MovieSearcher        = (*IQQTV)(nil)
	_ mt.Fetcher              = (*IQQTV)(nil)
	_ mt.ProxySetter          = (*IQQTV)(nil)
	_ mt.RequestTimeoutSetter = (*IQQTV)(nil)
)

type IQQTV struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *IQQTV {
	u, _ := url.Parse(defaultURL)
	p := &IQQTV{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *IQQTV {
	u, _ := url.Parse(base)
	p := &IQQTV{baseURL: u, timeout: defaultTimeout, priority: Priority}
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

func (p *IQQTV) Name() string { return Name }

func (p *IQQTV) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *IQQTV) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *IQQTV) Language() language.Tag { return language.SimplifiedChinese }

func (p *IQQTV) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *IQQTV) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}

func (p *IQQTV) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (p *IQQTV) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	baseHost := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host == baseHost && effectivePort(u) == effectivePort(p.baseURL) {
		return true
	}

	// Amane documents that iqq5.xyz redirects to rotating IQQTV hosts such as
	// iqqk4.quest and iqqq5.work. Accept only those two narrow HTTPS host
	// families; never trust an arbitrary cross-origin redirect or relax TLS
	// certificate verification.
	return strings.EqualFold(p.baseURL.Scheme, "https") &&
		effectivePort(u) == "443" &&
		rotatingHostRE.MatchString(host)
}

var rotatingHostRE = regexp.MustCompile(`^iqq[a-z0-9]+\.(?:quest|work)$`)

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

func (p *IQQTV) validDetailPath(rawPath string) bool {
	clean := path.Clean(rawPath)
	if clean != rawPath && clean+"/" != rawPath {
		return false
	}
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" || strings.EqualFold(path.Base(clean), "search.php") {
		return false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 0 && (parts[0] == "cn" || parts[0] == "jp") {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !validPathPartRE.MatchString(part) {
			return false
		}
	}
	return true
}

func (p *IQQTV) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("iqqtv: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("iqqtv: unexpected detail origin %q", rawURL)
	}
	if !p.validDetailPath(u.Path) {
		return "", fmt.Errorf("iqqtv: unsupported detail path %q", u.Path)
	}
	return strings.Trim(u.Path, "/"), nil
}

func (p *IQQTV) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("iqqtv: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("%w: refusing redirect to unexpected origin %q", ErrUnsafeRedirect, req.URL)
	}
	return nil
}

func (p *IQQTV) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *IQQTV) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("iqqtv: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("iqqtv: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *IQQTV) rebuildClientLocked() {
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
	p.client = &http.Client{Transport: httpretry.WrapTransport(transport), Timeout: timeout, CheckRedirect: p.checkRedirect}
}

func (p *IQQTV) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *IQQTV) Fetch(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("iqqtv: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("iqqtv: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("iqqtv: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("iqqtv: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("iqqtv: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *IQQTV) getHTML(rawURL string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("iqqtv: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("iqqtv: response exceeds %d bytes", maxPageBytes)
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("iqqtv: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func normalizeNumber(value string) string {
	value = strings.ToUpper(strings.TrimSpace(norm.NFKC.String(value)))
	return strings.NewReplacer("-", "", "_", "", " ", "", "\t", "").Replace(value)
}

func isExcluded(title string) bool {
	for _, word := range excludedWords {
		if strings.Contains(title, word) {
			return true
		}
	}
	return false
}

func titleNumberTokens(title string) []string {
	var values []string
	for _, token := range strings.Fields(title) {
		token = strings.Trim(token, "()[]{}，。:：")
		if token != "" {
			values = append(values, token)
		}
	}
	return values
}

func cleanNumber(raw string) string {
	value := strings.TrimSpace(norm.NFKC.String(raw))
	lower := strings.ToLower(value)
	for _, prefix := range []string{"_1pondo_", "1pondo_", "caribbeancom-", "caribbeancom"} {
		if strings.HasPrefix(lower, prefix) {
			value = value[len(prefix):]
			lower = strings.ToLower(value)
			break
		}
	}
	if strings.HasSuffix(lower, "-ppv") {
		value = value[:len(value)-4]
	}
	return strings.Trim(value, " _-")
}

func cleanTitle(title string) string {
	parts := strings.Fields(strings.TrimSpace(title))
	for i := len(parts) - 1; i >= 0; i-- {
		token := cleanNumber(parts[i])
		if catalogNumberRE.MatchString(parts[i]) || (token != "" && catalogNumberRE.MatchString(token)) {
			parts = append(parts[:i], parts[i+1:]...)
			break
		}
	}
	return strings.Join(parts, " ")
}

func (p *IQQTV) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "cn", "search.php")
	q := u.Query()
	q.Set("kw", keyword)
	u.RawQuery = q.Encode()
	return u.String()
}

func (p *IQQTV) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}
	doc, _, err := p.getHTML(p.searchURL(keyword))
	if err != nil {
		return nil, err
	}
	query := normalizeNumber(cleanNumber(keyword))
	results := make([]*model.MovieSearchResult, 0, 4)
	seen := make(map[string]struct{})
	doc.Find("span.title a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		title := strings.TrimSpace(a.AttrOr("title", a.Text()))
		if href == "" || title == "" || isExcluded(title) {
			return
		}
		matchedNumber := ""
		for _, token := range titleNumberTokens(title) {
			if catalogNumberRE.MatchString(token) && normalizeNumber(cleanNumber(token)) == query {
				matchedNumber = cleanNumber(token)
				break
			}
		}
		if matchedNumber == "" {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		candidate := p.baseURL.ResolveReference(ref)
		if !p.validOrigin(candidate) {
			return
		}
		detailPath := strings.Trim(candidate.Path, "/")
		detailPath = strings.TrimPrefix(strings.TrimPrefix(detailPath, "cn/"), "jp/")
		if detailPath == "" {
			return
		}
		candidate.Path = "/cn/" + detailPath
		candidate.RawQuery = ref.RawQuery
		candidate.Fragment = ""
		homepage := candidate.String()
		id, err := p.ParseMovieIDFromURL(homepage)
		if err != nil {
			return
		}
		if _, exists := seen[homepage]; exists {
			return
		}
		seen[homepage] = struct{}{}
		cover := resolveURL(p.baseURL, firstAttr(a.Find("img").First(), "data-src", "src"))
		number := matchedNumber
		displayTitle := cleanTitle(title)
		result := &model.MovieSearchResult{
			ID: id, Number: number, Title: displayTitle, Provider: p.Name(),
			Homepage: homepage, CoverURL: cover, ThumbURL: cover,
		}
		if result.Number == "" {
			result.Number = keyword
		}
		if !result.IsValid() {
			return
		}
		results = append(results, result)
	})
	return results, nil
}

func lastToken(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (p *IQQTV) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = p.NormalizeMovieID(id)
	if id == "" {
		return nil, fmt.Errorf("iqqtv: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	if strings.ContainsAny(id, "\\?#") {
		return nil, fmt.Errorf("iqqtv: invalid movie ID %q", id)
	}
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !validPathPartRE.MatchString(part) {
			return nil, fmt.Errorf("iqqtv: invalid movie ID %q", id)
		}
	}
	u := *p.baseURL
	u.Path = "/" + strings.Trim(id, "/")
	return p.GetMovieInfoByURL(u.String())
}

func (p *IQQTV) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
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
	title := strings.TrimSpace(doc.Find("h1.h4.b").First().Text())
	if title == "" || isExcluded(title) {
		return nil, ErrMovieNotFound
	}
	number := cleanNumber(lastToken(title))
	if !catalogNumberRE.MatchString(number) {
		return nil, fmt.Errorf("%w: detail title has no catalog number", ErrMovieNotFound)
	}
	actors := uniqueTexts(doc.Find("a[href*='actor'] span"))
	studio := strings.TrimSpace(doc.Find("a[href*='fac'] div[itemprop]").First().Text())
	release := strings.ReplaceAll(strings.TrimSpace(doc.Find("div.date").First().Text()), "/", "-")
	runtime := parseRuntime(doc.Find("meta[itemprop='duration']").AttrOr("content", ""))
	series := strings.TrimSpace(doc.Find("a[href*='series']").First().Text())
	thumb := strings.TrimSpace(doc.Find("meta[property='og:image']").First().AttrOr("content", ""))
	thumb = resolveURL(p.baseURL, thumb)
	plot := extractPlot(doc)
	var samples []string
	doc.Find("div.cover img[data-src]").Each(func(_ int, img *goquery.Selection) {
		if src := strings.TrimSpace(img.AttrOr("data-src", "")); src != "" {
			if resolved := resolveURL(p.baseURL, src); resolved != "" && resolved != thumb {
				samples = appendUnique(samples, resolved)
			}
		}
	})
	info := &model.MovieInfo{
		ID: id, Number: number, Title: cleanTitle(title), Provider: p.Name(), Homepage: rawURL,
		Actors: pq.StringArray(actors), Maker: studio, Label: studio, Series: series,
		Genres:  pq.StringArray(uniqueTexts(doc.Find("div.tag-info a[href*='tag']"))),
		Summary: plot, CoverURL: thumb, ThumbURL: thumb, PreviewImages: pq.StringArray(samples),
	}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if runtime != nil {
		info.Runtime = *runtime
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func parseRuntime(value string) *int {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return nil
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	s, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || h < 0 || m < 0 || m > 59 || s < 0 || s > 59 {
		return nil
	}
	minutes := h*60 + m
	if s >= 30 {
		minutes++
	}
	return &minutes
}

func extractPlot(doc *goquery.Document) string {
	var plot string
	doc.Find("p").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		value := strings.TrimSpace(node.Text())
		if !strings.Contains(value, "简介") && !strings.Contains(value, "簡介") {
			return true
		}
		value = strings.ReplaceAll(value, "简介：", "")
		value = strings.ReplaceAll(value, "簡介：", "")
		value = strings.ReplaceAll(value, "\n", "")
		value = strings.ReplaceAll(value, "\t", "")
		plot = strings.TrimSpace(strings.SplitN(value, "*根据分发", 2)[0])
		return false
	})
	return plot
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
