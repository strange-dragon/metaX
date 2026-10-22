// Copyright 2026 metaX contributors.
//
/*
Feature-level comparison reference: https://github.com/sqzw-x/amane.
This is a native Go implementation for the metaX Provider API; no Amane
source file or test fixture is vendored. See ../../NOTICE.md for attribution.
*/
package javdb

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

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	Name           = "JavDB"
	Priority       = 1000 - 10
	defaultURL     = "https://javdb.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound          = errors.New("javdb: movie not found")
	movieNumberRE             = regexp.MustCompile(`(?i)^[0-9]{0,4}[a-z]{1,16}(?:[._-]?[a-z0-9]+)*[._-][0-9]{2,}(?:[._-][a-z0-9]+)*$`)
	numericUnderscoreNumberRE = regexp.MustCompile(`^[0-9]{3,}(?:_[0-9]+)+$`)
	leadingDigitsRE           = regexp.MustCompile(`(?i)^\d+`)
	dayNumberRE               = regexp.MustCompile(`(?i)^(?P<studio>.+?)(?P<sep0>[._-])(?P<year>\d{4}|\d{2})(?P<sep1>[._-])(?P<month>\d{2})(?P<sep2>[._-])(?P<day>\d{2})$`)
	scoreRE                   = regexp.MustCompile(`\d+(?:\.\d+)?`)
	runtimeRE                 = regexp.MustCompile(`\d+`)
)

var (
	_ mt.MovieProvider        = (*JavDB)(nil)
	_ mt.MovieSearcher        = (*JavDB)(nil)
	_ mt.Fetcher              = (*JavDB)(nil)
	_ mt.ProxySetter          = (*JavDB)(nil)
	_ mt.RequestTimeoutSetter = (*JavDB)(nil)
	_ mt.ConfigSetter         = (*JavDB)(nil)
)

// JavDB implements metaX's native movie Provider contract.
type JavDB struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string
	cookies  string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *JavDB {
	u, _ := url.Parse(defaultURL)
	p := &JavDB{
		baseURL:  u,
		timeout:  defaultTimeout,
		priority: Priority,
	}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *JavDB {
	u, _ := url.Parse(base)
	p := &JavDB{
		baseURL:  u,
		timeout:  defaultTimeout,
		priority: Priority,
		client:   client,
	}
	if p.client == nil {
		p.rebuildClientLocked()
	}
	return p
}

func (p *JavDB) Name() string { return Name }

func (p *JavDB) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *JavDB) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *JavDB) Language() language.Tag { return language.SimplifiedChinese }

func (p *JavDB) URL() *url.URL {
	u := *p.baseURL
	return &u
}

// NormalizeMovieID preserves opaque JavDB detail IDs, which need not equal the
// displayed movie number. URL-shaped values are reduced to their /v/{id} slug.
func (p *JavDB) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(id)
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		if parsed, err := p.ParseMovieIDFromURL(id); err == nil {
			return parsed
		}
	}
	if movieNumberRE.MatchString(id) {
		return strings.ToUpper(norm.NFKC.String(id))
	}
	return id
}

func (p *JavDB) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.TrimSpace(norm.NFKC.String(keyword)))
}

func (p *JavDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("javdb: parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("javdb: expected an absolute detail URL")
	}
	if u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("javdb: detail URL must not contain userinfo or fragment")
	}
	if !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return "", fmt.Errorf("javdb: unexpected detail scheme %q", u.Scheme)
	}
	if !p.allowedHost(u.Hostname()) {
		return "", fmt.Errorf("javdb: unexpected detail host %q", u.Hostname())
	}
	if effectivePort(u) != effectivePort(p.baseURL) {
		return "", fmt.Errorf("javdb: unexpected detail port %q", u.Port())
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "v" || strings.TrimSpace(segments[1]) == "" {
		return "", fmt.Errorf("javdb: URL is not a /v/{id} detail URL")
	}
	return segments[1], nil
}

func effectivePort(u *url.URL) string {
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

func (p *JavDB) allowedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	base := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host == base {
		return true
	}
	// JavDB's canonical host and its common www alias are equivalent.
	return base == "javdb.com" && host == "www.javdb.com"
}

// SetConfig applies provider configuration. JavDB's anonymous FC2 detail pages
// can redirect to a login page, so deployments may supply an authenticated
// Cookie header through MT_MOVIE_PROVIDER_JAVDB__COOKIES (or __COOKIE).
func (p *JavDB) SetConfig(config mt.Config) error {
	if config == nil {
		return nil
	}
	var (
		raw string
		err error
	)
	switch {
	case config.Has("cookies"):
		raw, err = config.GetString("cookies")
	case config.Has("cookie"):
		raw, err = config.GetString("cookie")
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("javdb: read cookies config: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\r\n") {
		return errors.New("javdb: cookies must not contain CR or LF")
	}
	p.clientMu.Lock()
	p.cookies = raw
	p.clientMu.Unlock()
	return nil
}

// SetRequestTimeout replaces the HTTP client with the requested timeout while
// preserving its proxy setting. A non-positive value restores the default.
func (p *JavDB) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

// SetProxy supports HTTP(S) and SOCKS5 proxy URLs supported by net/http.
func (p *JavDB) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("javdb: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("javdb: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *JavDB) rebuildClientLocked() {
	var transport *http.Transport
	if baseTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = baseTransport.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		u, err := url.Parse(p.proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{
		Transport: httpretry.WrapTransport(transport),
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("javdb: stopped after 10 redirects")
			}
			if !p.allowedRedirectURL(req.URL) {
				return fmt.Errorf("javdb: refusing redirect to unexpected URL %q", req.URL.String())
			}
			return nil
		},
	}
}

// allowedRedirectURL prevents a source-page redirect from escaping the
// configured origin, including redirects to a different port on the same host.
// The canonical www alias is allowed when it uses the same effective port.
func (p *JavDB) allowedRedirectURL(u *url.URL) bool {
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

func (p *JavDB) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

// Fetch makes a single request using the Provider's configured HTTP client.
// Callers are responsible for closing the returned response body.
func (p *JavDB) Fetch(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("javdb: create request: %w", err)
	}
	p.setHeaders(req)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("javdb: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("javdb: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *JavDB) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	p.clientMu.RLock()
	cookies := p.cookies
	p.clientMu.RUnlock()
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
}

func (p *JavDB) getHTML(rawURL string) (*goquery.Document, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("javdb: read %s: %w", rawURL, err)
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("javdb: response from %s exceeds %d bytes", rawURL, maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("javdb: parse HTML from %s: %w", rawURL, err)
	}
	return doc, nil
}

func (p *JavDB) detailURL(id string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "v", id)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (p *JavDB) searchURL(keyword string) string {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "search")
	q := u.Query()
	q.Set("q", keyword)
	q.Set("locale", "zh")
	u.RawQuery = q.Encode()
	return u.String()
}

func (p *JavDB) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("javdb: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	// Recognized work numbers are search terms. FC2 needs its own recognizer
	// because the prefix contains a digit. Other values are treated as opaque
	// detail slugs, including slugs that happen to contain hyphens.
	if !movieNumberRE.MatchString(id) && !numericUnderscoreNumberRE.MatchString(id) && fc2ArticleID(id) == "" {
		info, err := p.GetMovieInfoByURL(p.detailURL(id))
		if err == nil && info != nil {
			return info, nil
		}
		if err != nil && !errors.Is(err, ErrMovieNotFound) {
			// Preserve transport, timeout, status and parse failures; a network
			// error is not evidence that a slug should be treated as a search term.
			return nil, err
		}
		// Opaque IDs are tried as direct detail slugs first. A valid not-found
		// result may still be a movie number or keyword, so then try search.
	}
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	index := selectNumberIndex(results, id)
	if index < 0 {
		return nil, ErrMovieNotFound
	}
	info, detailErr := p.GetMovieInfoByURL(results[index].Homepage)
	if detailErr == nil {
		return info, nil
	}
	if !errors.Is(detailErr, ErrMovieNotFound) {
		return nil, detailErr
	}

	// JavDB sometimes serves a valid search card while its detail page omits
	// the copy-control number marker. Preserve the exact matched card as a
	// partial result instead of discarding usable title and artwork metadata.
	result := results[index]
	cover := strings.TrimSpace(result.ThumbURL)
	if cover == "" {
		return nil, detailErr
	}
	return &model.MovieInfo{
		ID:       result.ID,
		Number:   result.Number,
		Title:    result.Title,
		Provider: p.Name(),
		Homepage: result.Homepage,
		ThumbURL: cover,
		CoverURL: cover,
	}, nil
}

func (p *JavDB) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := p.getHTML(rawURL)
	if err != nil {
		return nil, err
	}

	number := attrText(doc.Find(".copy-to-clipboard[data-clipboard-text]").First(), "data-clipboard-text")
	if number == "" {
		// Keep the extraction tolerant of markup changes to the copy control while
		// still requiring an explicit work-number attribute on the detail page.
		number = attrText(doc.Find("[data-clipboard-text]").First(), "data-clipboard-text")
	}
	if number == "" {
		// A normal HTTP response without the detail-page marker can be a
		// not-found page or an anti-bot interstitial. Do not fabricate fields.
		return nil, ErrMovieNotFound
	}
	number = strings.TrimSpace(norm.NFKC.String(number))

	title := cleanText(doc.Find("h2.title.is-4 strong.current-title").First().Text())
	if title == "" {
		title = cleanText(doc.Find("h2.title.is-4").First().Text())
	}
	if title == "" {
		return nil, fmt.Errorf("javdb: detail page %s has a number but no title", rawURL)
	}

	studio := fieldValues(doc, "片商", "片商:", "製作商", "制作商")
	publisher := fieldValues(doc, "發行", "发行", "發售")
	releaseText := fieldText(doc, "日期", "發售日", "発売日", "發行日期", "发行日期")
	runtimeText := fieldText(doc, "時長", "时长", "長度", "长度", "時間", "時間長")
	series := fieldValues(doc, "系列")
	directors := fieldValues(doc, "導演", "导演", "監督", "监督")
	genres := fieldValues(doc, "類別", "类别", "ジャンル", "分類", "分类")
	actors := actorNames(doc)
	coverURL := absoluteURL(p.baseURL, firstAttr(doc.Find("img.video-cover").First(), "data-src", "data-original", "src"))
	previews := p.absoluteLinks(doc, ".tile-images.preview-images a.tile-item", "href")
	trailerURL := absoluteURL(p.baseURL, firstAttr(doc.Find("video#preview-video source").First(), "src"))
	if trailerURL == "" {
		trailerURL = absoluteURL(p.baseURL, firstAttr(doc.Find("video#preview-video").First(), "src"))
	}
	scoreText := cleanText(doc.Find("span.score-stars").First().NextAll().First().Text())
	if scoreText == "" {
		scoreText = cleanText(doc.Find("span.score-stars").First().Parent().Text())
	}

	info := &model.MovieInfo{
		ID:              id,
		Number:          number,
		Title:           title,
		Provider:        p.Name(),
		Homepage:        rawURL,
		Maker:           firstNonEmpty(studio),
		Label:           firstNonEmpty(publisher),
		Series:          firstNonEmpty(series),
		Director:        strings.Join(directors, ", "),
		Actors:          pq.StringArray(actors),
		Genres:          pq.StringArray(genres),
		CoverURL:        coverURL,
		ThumbURL:        coverURL,
		PreviewImages:   pq.StringArray(previews),
		PreviewVideoURL: trailerURL,
		Score:           parseScore(scoreText),
		Runtime:         parseRuntimeMinutes(runtimeText),
	}
	if strings.TrimSpace(releaseText) != "" {
		info.ReleaseDate = parser.ParseDate(releaseText)
	}
	if info.CoverURL == "" {
		// Some JavDB layouts expose the poster through an og:image meta tag.
		info.CoverURL = absoluteURL(p.baseURL, attrText(doc.Find("meta[property='og:image']").First(), "content"))
		info.ThumbURL = info.CoverURL
	}
	if info.CoverURL == "" {
		return nil, fmt.Errorf("javdb: detail page %s has no cover image", rawURL)
	}
	if info.Number == "" || info.Title == "" {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func (p *JavDB) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}

	terms := searchTerms(keyword)
	seen := make(map[string]struct{})
	results := make([]*model.MovieSearchResult, 0, 12)
	for _, term := range terms {
		doc, err := p.getHTML(p.searchURL(term))
		if err != nil {
			return nil, err
		}
		doc.Find("a.box").Each(func(_ int, card *goquery.Selection) {
			href := strings.TrimSpace(attrText(card, "href"))
			if href == "" {
				return
			}
			homepage := absoluteURL(p.baseURL, href)
			id, err := p.ParseMovieIDFromURL(homepage)
			if err != nil {
				return
			}
			if _, ok := seen[homepage]; ok {
				return
			}

			number := cleanText(card.Find(".video-title strong").First().Text())
			videoTitle := cleanText(card.Find(".video-title").First().Text())
			if number == "" {
				number = leadingToken(videoTitle)
			}
			if number == "" {
				return
			}
			title := removeLeadingNumber(videoTitle, number)
			if title == "" {
				title = number
			}
			thumb := absoluteURL(p.baseURL, firstAttr(card.Find("img").First(), "data-src", "data-original", "src"))
			result := &model.MovieSearchResult{
				ID:       id,
				Number:   strings.TrimSpace(norm.NFKC.String(number)),
				Title:    title,
				Provider: p.Name(),
				Homepage: homepage,
				ThumbURL: thumb,
			}
			if !result.IsValid() {
				return
			}
			seen[homepage] = struct{}{}
			results = append(results, result)
		})
	}
	return results, nil
}

func init() {
	mt.Register(Name, New)
}

func searchTerms(number string) []string {
	terms := []string{number}
	if articleID := fc2ArticleID(number); articleID != "" {
		terms = appendUnique(terms, "FC2-PPV-"+articleID)
		terms = appendUnique(terms, "FC2-"+articleID)
	} else if prefix := leadingDigitPrefix(number); prefix != "" {
		terms = appendUnique(terms, number[len(prefix):])
	}
	if alt := alternateWesternNumber(number); alt != "" {
		terms = appendUnique(terms, alt)
	}
	return terms
}

func selectNumberIndex(results []*model.MovieSearchResult, query string) int {
	exact := -1
	loose := make([]int, 0, 2)
	for i, result := range results {
		switch numberMatch(result.Number, query) {
		case 2:
			if exact < 0 {
				exact = i
			}
		case 1:
			loose = append(loose, i)
		}
	}
	if exact >= 0 {
		return exact
	}
	if len(loose) == 1 {
		return loose[0]
	}
	return -1
}

// numberMatch returns 2 for exact equality, 1 only when exactly one side has a
// leading numeric series prefix, and 0 for unrelated identifiers.
func numberMatch(found, query string) int {
	foundFold, queryFold := foldNumber(found), foldNumber(query)
	if foundFold == "" || queryFold == "" {
		return 0
	}
	if foundFold == queryFold {
		return 2
	}
	if foundID := fc2ArticleID(found); foundID != "" && foundID == fc2ArticleID(query) {
		return 2
	}
	if sameWesternDate(found, query) {
		return 2
	}
	foundDigits, foundRest := splitLeadingDigits(found)
	queryDigits, queryRest := splitLeadingDigits(query)
	if foundRest == queryRest && !(foundDigits != "" && queryDigits != "") {
		return 1
	}
	return 0
}

func foldNumber(value string) string {
	// Match Amane: case, hyphens and spaces are insignificant; underscores are significant.
	value = strings.ToUpper(norm.NFKC.String(strings.TrimSpace(value)))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

var fc2NumberRE = regexp.MustCompile("(?i)^fc2(?:_?ppv)?_?(\\d+)$")

func fc2ArticleID(value string) string {
	match := fc2NumberRE.FindStringSubmatch(foldNumber(value))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func leadingDigitPrefix(value string) string {
	prefix := leadingDigitsRE.FindString(value)
	if prefix == "" || len(prefix) >= len(value) {
		return ""
	}
	next := value[len(prefix)]
	if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
		return prefix
	}
	return ""
}

func splitLeadingDigits(value string) (string, string) {
	folded := foldNumber(value)
	prefix := leadingDigitPrefix(folded)
	if prefix == "" {
		return "", folded
	}
	return folded[:len(prefix)], folded[len(prefix):]
}

func foldStudio(value string) string {
	value = strings.ToLower(norm.NFKC.String(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sameWesternDate(a, b string) bool {
	partsA, okA := westernDateParts(a)
	partsB, okB := westernDateParts(b)
	if !okA || !okB {
		return false
	}
	return strings.EqualFold(partsA.studio, partsB.studio) && partsA.date == partsB.date
}

type westernParts struct {
	studio string
	date   string
}

func westernDateParts(value string) (westernParts, bool) {
	match := dayNumberRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) == 0 {
		return westernParts{}, false
	}
	year := match[dayNumberRE.SubexpIndex("year")]
	if len(year) == 2 {
		year = "20" + year
	}
	month := match[dayNumberRE.SubexpIndex("month")]
	day := match[dayNumberRE.SubexpIndex("day")]
	dateText := year + "-" + month + "-" + day
	parsed, err := time.Parse("2006-01-02", dateText)
	if err != nil || parsed.Format("2006-01-02") != dateText {
		return westernParts{}, false
	}
	studio := foldStudio(match[dayNumberRE.SubexpIndex("studio")])
	// A four-digit year can match its two-digit counterpart, while the rest
	// of the date and the normalized studio must remain equal.
	return westernParts{studio: studio, date: dateText}, true
}

func alternateWesternNumber(value string) string {
	match := dayNumberRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) == 0 {
		return ""
	}
	yearIndex := dayNumberRE.SubexpIndex("year")
	year := match[yearIndex]
	if _, ok := westernDateParts(value); !ok {
		return ""
	}
	var alternateYear string
	if len(year) == 4 {
		alternateYear = year[2:]
	} else {
		alternateYear = "20" + year
	}
	studio := match[dayNumberRE.SubexpIndex("studio")]
	sep0 := match[dayNumberRE.SubexpIndex("sep0")]
	sep1 := match[dayNumberRE.SubexpIndex("sep1")]
	sep2 := match[dayNumberRE.SubexpIndex("sep2")]
	month := match[dayNumberRE.SubexpIndex("month")]
	day := match[dayNumberRE.SubexpIndex("day")]
	return studio + sep0 + alternateYear + sep1 + month + sep2 + day
}

func fieldValues(doc *goquery.Document, labels ...string) []string {
	var result []string
	doc.Find("strong").Each(func(_ int, label *goquery.Selection) {
		if !labelMatches(cleanText(label.Text()), labels) {
			return
		}
		value := fieldValueNode(label)
		if value.Length() == 0 {
			return
		}
		links := value.Find("a")
		if links.Length() > 0 {
			links.Each(func(_ int, node *goquery.Selection) {
				if value := cleanText(node.Text()); value != "" {
					result = appendUnique(result, value)
				}
			})
			return
		}
		if value := cleanText(value.Text()); value != "" {
			result = appendUnique(result, value)
		}
	})
	return result
}

func fieldText(doc *goquery.Document, labels ...string) string {
	values := fieldValues(doc, labels...)
	return strings.Join(values, " ")
}

func fieldValueNode(label *goquery.Selection) *goquery.Selection {
	value := label.NextAllFiltered("span.value").First()
	if value.Length() == 0 {
		value = label.Parent().Find("span.value").First()
	}
	if value.Length() == 0 {
		value = label.NextAllFiltered("span").First()
	}
	return value
}

func labelMatches(label string, tokens []string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
		return strings.TrimRight(value, ":")
	}
	normalized := normalize(label)
	for _, token := range tokens {
		if token != "" && normalized == normalize(token) {
			return true
		}
	}
	return false
}

func actorNames(doc *goquery.Document) []string {
	var names []string
	doc.Find("strong").Each(func(_ int, label *goquery.Selection) {
		if !labelMatches(cleanText(label.Text()), []string{"演員", "演员"}) {
			return
		}
		value := fieldValueNode(label)
		value.Find("a").Each(func(_ int, node *goquery.Selection) {
			if name := cleanText(node.Text()); name != "" {
				names = appendUnique(names, name)
			}
		})
	})
	return names
}

// absoluteURL resolves root-relative and absolute source URLs against the
// configured origin without inventing a host when the input is invalid.
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

func (p *JavDB) absoluteLinks(doc *goquery.Document, selector, attribute string) []string {
	var links []string
	doc.Find(selector).Each(func(_ int, node *goquery.Selection) {
		if value := absoluteURL(p.baseURL, attrText(node, attribute)); value != "" {
			links = appendUnique(links, value)
		}
	})
	return links
}

func attrText(node *goquery.Selection, key string) string {
	if value, ok := node.Attr(key); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstAttr(node *goquery.Selection, keys ...string) string {
	for _, key := range keys {
		if value := attrText(node, key); value != "" {
			return value
		}
	}
	return ""
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(norm.NFKC.String(value))), " ")
}

func leadingToken(value string) string {
	fields := strings.Fields(cleanText(value))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func removeLeadingNumber(title, number string) string {
	title = cleanText(title)
	number = cleanText(number)
	if title == "" || number == "" {
		return title
	}
	if len(title) >= len(number) && strings.EqualFold(title[:len(number)], number) {
		title = strings.TrimSpace(title[len(number):])
		title = strings.TrimLeft(title, " -—:：|")
	}
	return cleanText(title)
}

func parseRuntimeMinutes(value string) int {
	value = cleanText(value)
	if value == "" {
		return 0
	}
	normalized := strings.ToLower(value)
	hours := regexp.MustCompile(`(\d+)\s*(?:時間|小時|小时|hours?|hrs?|h)`).FindStringSubmatch(normalized)
	minutes := regexp.MustCompile(`(\d+)\s*(?:分鐘|分钟|分|minutes?|mins?|m)`).FindStringSubmatch(normalized)
	if hours != nil || minutes != nil {
		total := 0
		if hours != nil {
			total += parseInt(hours[1]) * 60
		}
		if minutes != nil {
			total += parseInt(minutes[1])
		}
		return total
	}
	match := runtimeRE.FindString(value)
	return parseInt(match)
}

func parseScore(value string) float64 {
	match := scoreRE.FindString(strings.TrimSpace(value))
	if match == "" {
		return 0
	}
	score, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	return score
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(value)
	return n
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
