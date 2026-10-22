// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for metaX's Provider API; no Amane source or
// fixtures are vendored. See NOTICE.md for attribution.
package airav

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	Name           = "Airav"
	Priority       = 1000 - 20
	defaultURL     = "https://airav.io"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound = errors.New("airav: movie not found")
)

var (
	_ mt.MovieProvider        = (*Airav)(nil)
	_ mt.MovieSearcher        = (*Airav)(nil)
	_ mt.Fetcher              = (*Airav)(nil)
	_ mt.ProxySetter          = (*Airav)(nil)
	_ mt.RequestTimeoutSetter = (*Airav)(nil)
)

type Airav struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Airav {
	u, _ := url.Parse(defaultURL)
	p := &Airav{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Airav {
	u, _ := url.Parse(base)
	p := &Airav{baseURL: u, timeout: defaultTimeout, priority: Priority}
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

func (p *Airav) Name() string { return Name }

func (p *Airav) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *Airav) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *Airav) Language() language.Tag { return language.SimplifiedChinese }

func (p *Airav) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *Airav) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}

func (p *Airav) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (p *Airav) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("airav: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("airav: unexpected detail origin %q", rawURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("airav: detail URL must not contain query or fragment")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "video" || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("airav: invalid detail path %q", u.Path)
	}
	return parts[1], nil
}

func (p *Airav) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(p.baseURL.Hostname(), ".")) {
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

func (p *Airav) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("airav: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("airav: refusing redirect to unexpected origin %q", req.URL)
	}
	return nil
}

func (p *Airav) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *Airav) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("airav: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("airav: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *Airav) rebuildClientLocked() {
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

func (p *Airav) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *Airav) Fetch(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("airav: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("airav: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("airav: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("airav: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("airav: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *Airav) getHTML(rawURL string) (*goquery.Document, error) {
	doc, _, err := p.getHTMLWithFinalURL(rawURL)
	return doc, err
}

func (p *Airav) getHTMLWithFinalURL(rawURL string) (*goquery.Document, string, error) {
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
		return nil, "", fmt.Errorf("airav: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("airav: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("airav: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func (p *Airav) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "search_result")
	q := u.Query()
	q.Set("kw", keyword)
	u.RawQuery = q.Encode()
	return u.String()
}

func normalizeNumber(value string) string {
	value = strings.ToUpper(strings.TrimSpace(norm.NFKC.String(value)))
	value = strings.NewReplacer("-", "", " ", "", "\t", "").Replace(value)
	return value
}

/*
fc2ArticleID recognizes the FC2 catalog variants used by Airav and Amane.
Hyphens and spaces are insignificant; underscores are accepted only at the
FC2/PPV and PPV/ID boundaries.
*/
func fc2ArticleID(number string) string {
	value := strings.ToLower(strings.NewReplacer("-", "", " ", "", "\t", "").Replace(norm.NFKC.String(strings.TrimSpace(number))))
	if !strings.HasPrefix(value, "fc2") {
		return ""
	}
	suffix := strings.TrimPrefix(value, "fc2")
	suffix = strings.TrimPrefix(suffix, "_")
	if strings.HasPrefix(suffix, "ppv") {
		suffix = strings.TrimPrefix(suffix, "ppv")
		suffix = strings.TrimPrefix(suffix, "_")
	}
	if suffix == "" {
		return ""
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return suffix
}

func sameNumber(left, right string) bool {
	if normalizeNumber(left) == normalizeNumber(right) {
		return true
	}
	leftID, rightID := fc2ArticleID(left), fc2ArticleID(right)
	return leftID != "" && leftID == rightID
}

func leadingToken(value string) string {
	fields := strings.Fields(strings.TrimSpace(norm.NFKC.String(value)))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func removeLeadingNumber(title, number string) string {
	title = strings.TrimSpace(norm.NFKC.String(title))
	number = strings.TrimSpace(norm.NFKC.String(number))
	if title == "" || number == "" || len(title) < len(number) ||
		!strings.EqualFold(title[:len(number)], number) {
		return title
	}
	return strings.TrimSpace(strings.TrimLeft(title[len(number):], " -—:：|"))
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

func (p *Airav) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}

	// Airav indexes FC2 articles under FC2-PPV-<id> even when the caller
	// supplies the shorter FC2-<id> form. Try that canonical variant first,
	// then the original query; stop at the first exact result like Amane.
	terms := make([]string, 0, 2)
	if articleID := fc2ArticleID(keyword); articleID != "" {
		canonical := "FC2-PPV-" + articleID
		if normalizeNumber(canonical) != normalizeNumber(keyword) {
			terms = append(terms, canonical)
		}
	}
	terms = append(terms, keyword)

	var firstErr error
	for _, term := range terms {
		results, err := p.searchMovieTerm(term, keyword)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

func (p *Airav) searchMovieTerm(term, requestedNumber string) ([]*model.MovieSearchResult, error) {
	doc, err := p.getHTML(p.searchURL(term))
	if err != nil {
		return nil, err
	}
	results := make([]*model.MovieSearchResult, 0, 4)
	seen := make(map[string]struct{})
	doc.Find("div.oneVideo").Each(func(_ int, card *goquery.Selection) {
		href, _ := card.Find("a[href]").First().Attr("href")
		title := strings.TrimSpace(card.Find("h5").First().Text())
		token := leadingToken(title)
		if href == "" || token == "" || !sameNumber(token, requestedNumber) {
			return
		}
		homepage := absoluteURL(p.baseURL, href)
		if homepage == "" {
			return
		}
		if _, exists := seen[homepage]; exists {
			return
		}
		id, err := p.ParseMovieIDFromURL(homepage)
		if err != nil {
			return
		}
		coverRaw, _ := card.Find("img").First().Attr("data-src")
		if strings.TrimSpace(coverRaw) == "" {
			coverRaw, _ = card.Find("img").First().Attr("src")
		}
		result := &model.MovieSearchResult{
			ID:       id,
			Number:   token,
			Title:    removeLeadingNumber(title, token),
			Provider: p.Name(),
			Homepage: homepage,
			CoverURL: absoluteURL(p.baseURL, coverRaw),
			ThumbURL: absoluteURL(p.baseURL, coverRaw),
		}
		if result.Title == "" {
			result.Title = title
		}
		if !result.IsValid() {
			return
		}
		seen[homepage] = struct{}{}
		results = append(results, result)
	})
	return results, nil
}

func (p *Airav) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("airav: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}

	// Search by catalog number first, since Airav's internal detail IDs may
	// differ from the displayed number. Never accept a fuzzy/near-number hit.
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if sameNumber(result.Number, id) {
			return p.GetMovieInfoByURL(result.Homepage)
		}
	}

	// The Provider API also calls this method with IDs parsed from Homepage.
	// If no exact catalog-number search result exists, try the site's detail
	// route using that opaque path ID.
	if strings.ContainsAny(id, "/\\\\?#") {
		return nil, fmt.Errorf("airav: invalid movie ID %q", id)
	}
	u := *p.baseURL
	u.Path = path.Join(u.Path, "video", id)
	return p.GetMovieInfoByURL(u.String())
}

func (p *Airav) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, finalURL, err := p.getHTMLWithFinalURL(rawURL)
	if err != nil {
		return nil, err
	}
	finalID, finalErr := p.ParseMovieIDFromURL(finalURL)
	if finalErr != nil || finalID != id {
		return nil, fmt.Errorf("%w: detail URL redirected from %q to %q", ErrMovieNotFound, rawURL, finalURL)
	}
	number := ""
	doc.Find("li").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if !strings.Contains(item.Text(), "番") {
			return true
		}
		number = strings.TrimSpace(item.Find("span").First().Text())
		return false
	})
	heading := strings.TrimSpace(doc.Find("div.video-title h1").First().Text())
	if number == "" || heading == "" {
		return nil, ErrMovieNotFound
	}
	token := leadingToken(heading)
	title := heading
	if normalizeNumber(token) == normalizeNumber(number) {
		title = removeLeadingNumber(heading, token)
	}
	cover := strings.TrimSpace(doc.Find("meta[property='og:image']").First().AttrOr("content", ""))
	cover = absoluteURL(p.baseURL, cover)
	actors := uniqueTexts(doc, "a[href*='actor?id=']")
	studio := strings.TrimSpace(doc.Find("a[href*='tag?fid=']").First().Text())
	releaseText := strings.TrimSpace(doc.Find("div.video-item i.fa-clock").First().Parent().Text())
	tags := uniqueTexts(doc, "a[href*='tag?tid=']")
	plot := strings.TrimSpace(doc.Find("div.video-info p").First().Text())

	info := &model.MovieInfo{
		ID:            id,
		Number:        number,
		Title:         title,
		Provider:      p.Name(),
		Homepage:      rawURL,
		Maker:         studio,
		Actors:        pq.StringArray(actors),
		Genres:        pq.StringArray(tags),
		CoverURL:      cover,
		ThumbURL:      cover,
		PreviewImages: pq.StringArray{},
		Summary:       strings.ReplaceAll(plot, "\n", "<br />"),
	}
	if releaseText != "" {
		info.ReleaseDate = parser.ParseDate(releaseText)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func uniqueTexts(doc *goquery.Document, selector string) []string {
	var values []string
	doc.Find(selector).Each(func(_ int, node *goquery.Selection) {
		value := strings.Join(strings.Fields(strings.TrimSpace(node.Text())), " ")
		if value == "" {
			return
		}
		for _, existing := range values {
			if existing == value {
				return
			}
		}
		values = append(values, value)
	})
	return values
}

func init() {
	mt.Register(Name, New)
}
