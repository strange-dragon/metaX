// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source file or
// fixture is vendored. See ../../NOTICE.md for attribution.
package fc2club

import (
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

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	Name           = "FC2Club"
	Priority       = 1000 - 12
	defaultURL     = "https://fc2club.top"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound = errors.New("fc2club: movie not found")
	fc2PrefixRE      = regexp.MustCompile(`(?i)^FC2(?:[-_]?PPV)?[-_]?`)
	digitsOnlyRE     = regexp.MustCompile(`^\d+$`)
	numberInTextRE   = regexp.MustCompile(`(?i)FC2[-_](\d+)`)
	releaseDateRE    = regexp.MustCompile(`(\d{4})[-/](\d{2})[-/](\d{2})`)
)

var (
	_ mt.MovieProvider        = (*FC2Club)(nil)
	_ mt.MovieSearcher        = (*FC2Club)(nil)
	_ mt.Fetcher              = (*FC2Club)(nil)
	_ mt.ProxySetter          = (*FC2Club)(nil)
	_ mt.RequestTimeoutSetter = (*FC2Club)(nil)
)

type FC2Club struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *FC2Club {
	u, _ := url.Parse(defaultURL)
	p := &FC2Club{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *FC2Club {
	u, _ := url.Parse(base)
	p := &FC2Club{baseURL: u, timeout: defaultTimeout, priority: Priority, client: client}
	if p.client == nil {
		p.rebuildClientLocked()
	}
	return p
}

func (p *FC2Club) Name() string { return Name }

func (p *FC2Club) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *FC2Club) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *FC2Club) Language() language.Tag { return language.SimplifiedChinese }

func (p *FC2Club) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func normalizeFC2Number(input string) (string, bool) {
	input = strings.TrimSpace(norm.NFKC.String(input))
	if match := numberInTextRE.FindStringSubmatch(input); len(match) == 2 {
		input = match[1]
	} else {
		input = fc2PrefixRE.ReplaceAllString(input, "")
	}
	input = strings.TrimSpace(strings.Trim(input, "-_ "))
	if !digitsOnlyRE.MatchString(input) {
		return "", false
	}
	return "FC2-" + input, true
}

func (p *FC2Club) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(id)
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		if parsed, err := p.ParseMovieIDFromURL(id); err == nil {
			return parsed
		}
	}
	if number, ok := normalizeFC2Number(id); ok {
		return number
	}
	return id
}

func (p *FC2Club) NormalizeMovieKeyword(keyword string) string {
	return p.NormalizeMovieID(strings.ToUpper(strings.TrimSpace(norm.NFKC.String(keyword))))
}

func (p *FC2Club) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("fc2club: parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("fc2club: expected an absolute detail URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	baseHost := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host != baseHost && !(baseHost == "fc2club.top" && host == "www.fc2club.top") {
		return "", fmt.Errorf("fc2club: unexpected detail host %q", host)
	}
	if path.Dir(u.Path) != "/html" {
		return "", fmt.Errorf("fc2club: URL is not an /html/FC2-{number}.html detail URL")
	}
	name := path.Base(u.Path)
	match := numberInTextRE.FindStringSubmatch(name)
	if len(match) != 2 || !strings.HasSuffix(strings.ToLower(name), ".html") {
		return "", fmt.Errorf("fc2club: invalid detail path %q", u.Path)
	}
	return "FC2-" + match[1], nil
}

func (p *FC2Club) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *FC2Club) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("fc2club: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("fc2club: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *FC2Club) rebuildClientLocked() {
	var transport *http.Transport
	if baseTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = baseTransport.Clone()
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
	p.client = &http.Client{Transport: transport, Timeout: timeout}
}

func (p *FC2Club) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *FC2Club) Fetch(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fc2club: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fc2club: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fc2club: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *FC2Club) getHTML(rawURL string) (*goquery.Document, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fc2club: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("fc2club: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("fc2club: parse HTML: %w", err)
	}
	return doc, nil
}

func (p *FC2Club) detailURL(id string) (string, error) {
	number, ok := normalizeFC2Number(id)
	if !ok {
		return "", fmt.Errorf("fc2club: invalid FC2 movie number %q", id)
	}
	digits := strings.TrimPrefix(number, "FC2-")
	u := *p.baseURL
	u.Path = path.Join(u.Path, "html", "FC2-"+digits+".html")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (p *FC2Club) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("fc2club: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	rawURL, err := p.detailURL(id)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByURL(rawURL)
}

func (p *FC2Club) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := p.getHTML(rawURL)
	if err != nil {
		return nil, err
	}
	if doc.Find("div.show-top-grids").Length() == 0 {
		return nil, ErrMovieNotFound
	}
	title := cleanText(doc.Find("div.show-top-grids h3").First().Text())
	if title == "" {
		return nil, ErrMovieNotFound
	}
	cover := resolveURL(p.baseURL, firstAttr(doc.Find("div.show-top-grids img").First(), "data-src", "data-original", "src"))
	if cover == "" {
		cover = resolveURL(p.baseURL, attrValue(doc.Find("meta[property='og:image']").First(), "content"))
	}
	if cover == "" {
		return nil, fmt.Errorf("fc2club: detail page %s has no cover image", rawURL)
	}

	actors := texts(doc, "ul.slides h5.title")
	genres := texts(doc, "a[href*='/tag/']")
	var releaseText string
	doc.Find("p").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if !strings.Contains(cleanText(node.Text()), "日期") {
			return true
		}
		releaseText = cleanText(node.Text())
		return false
	})
	release := ""
	if match := releaseDateRE.FindStringSubmatch(releaseText); len(match) == 4 {
		release = match[1] + "-" + match[2] + "-" + match[3]
	}
	displayTitle := removeLeadingNumber(title, id)
	if displayTitle == "" {
		displayTitle = title
	}

	info := &model.MovieInfo{
		ID:            id,
		Number:        id,
		Title:         displayTitle,
		Provider:      p.Name(),
		Homepage:      rawURL,
		Actors:        pq.StringArray(actors),
		Genres:        pq.StringArray(genres),
		CoverURL:      cover,
		ThumbURL:      cover,
		PreviewImages: pq.StringArray{},
	}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func (p *FC2Club) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	number, ok := normalizeFC2Number(p.NormalizeMovieKeyword(keyword))
	if !ok {
		return nil, nil
	}
	info, err := p.GetMovieInfoByID(number)
	if errors.Is(err, ErrMovieNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}

func texts(doc *goquery.Document, selector string) []string {
	var values []string
	doc.Find(selector).Each(func(_ int, node *goquery.Selection) {
		if value := cleanText(node.Text()); value != "" {
			values = appendUnique(values, value)
		}
	})
	return values
}

func removeLeadingNumber(title, number string) string {
	title = cleanText(title)
	number = cleanText(number)
	if title == "" || number == "" || len(title) < len(number) || !strings.EqualFold(title[:len(number)], number) {
		return title
	}
	rest := strings.TrimLeft(strings.TrimSpace(title[len(number):]), " -—:：|")
	return cleanText(rest)
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(norm.NFKC.String(value))), " ")
}

func attrValue(node *goquery.Selection, key string) string {
	value, _ := node.Attr(key)
	return strings.TrimSpace(value)
}

func firstAttr(node *goquery.Selection, keys ...string) string {
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

func init() {
	mt.Register(Name, New)
}
