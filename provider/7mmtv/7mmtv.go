// Copyright 2026 metaX contributors.
//
// Native implementation for metaX's Provider API. The catalogue is a streaming
// index: it holds no reviews or preview galleries, but it indexes Chinese,
// uncensored and amateur titles far beyond what the manufacturer sites expose,
// and every detail URL carries its catalog number in the path.

package sevenmmtv

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/language"
	"gorm.io/datatypes"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

const (
	Name           = "7MMTV"
	Priority       = 1000 - 30
	defaultURL     = "https://7mmtv.sx"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound = fmt.Errorf("7mmtv: %w", mt.ErrMovieNotFound)
)

var (
	_ mt.MovieProvider        = (*SevenMMTV)(nil)
	_ mt.MovieSearcher        = (*SevenMMTV)(nil)
	_ mt.Fetcher              = (*SevenMMTV)(nil)
	_ mt.ProxySetter          = (*SevenMMTV)(nil)
	_ mt.RequestTimeoutSetter = (*SevenMMTV)(nil)
)

// detailPathRE matches the language-prefixed detail path. The catalog number is
// the last segment, so a caller can go straight from a pasted URL to metadata.
var detailPathRE = regexp.MustCompile(`^/(?:zh|en|ja|ko)/([a-z][a-z-]*)_content/([0-9]+)/([^/]+)\.html$`)

// searchPathRE matches the addressable search result page.
var searchPathRE = regexp.MustCompile(`^/(?:zh|en|ja|ko)/[a-z][a-z-]*_search/[a-z]+/([^/]+)/[0-9]+\.html$`)

// labelPrefixRE strips the single tag the catalogue prepends to a shared title
// ("[無碼破解]", "[中字]"). Only a leading bracket is removed, because the title
// itself may legitimately carry brackets later on.
var labelPrefixRE = regexp.MustCompile(`^\s*\[[^\]]{1,16}\]\s*`)

// durationRE matches the runtime line, which sits beside the date and has no
// field in the metadata model.
var durationRE = regexp.MustCompile(`^[0-9]+\s*分$`)

// dateRE matches the catalogue's fixed date spelling.
var dateRE = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// assetHostRE matches the media hosts the catalogue serves artwork and preview
// clips from. They are separate registrable domains (1024cdn.sx / 1026cdn.sx)
// and the edge rotates the numeric label, so the shape is matched rather than a
// fixed hostname.
var assetHostRE = regexp.MustCompile(`^n[0-9]+s?\.102[0-9]cdn\.sx$`)

// categoryRE and itemIDRE validate the structural halves of a composite ID.
var (
	categoryRE = regexp.MustCompile(`^[a-z][a-z-]*$`)
	itemIDRE   = regexp.MustCompile(`^[0-9]+$`)
)

// assetSuffixes are the static extensions the media hosts serve.
var assetSuffixes = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif", ".mp4", ".m3u8"}

type SevenMMTV struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priority float64
}

func New() *SevenMMTV {
	u, err := url.Parse(defaultURL)
	if err != nil {
		panic(fmt.Sprintf("7mmtv: parse base URL: %v", err))
	}
	return newSevenMMTV(u, nil)
}

func newSevenMMTV(base *url.URL, client *http.Client) *SevenMMTV {
	p := &SevenMMTV{baseURL: base, timeout: defaultTimeout, priority: Priority}
	if client != nil {
		p.client = client
	} else {
		p.rebuildClientLocked()
	}
	return p
}

func (p *SevenMMTV) Name() string { return Name }

func (p *SevenMMTV) Priority() float64 { return p.priority }

func (p *SevenMMTV) SetPriority(v float64) { p.priority = v }

func (p *SevenMMTV) Language() language.Tag { return language.SimplifiedChinese }

func (p *SevenMMTV) URL() *url.URL { return p.baseURL }

// detailRef is what a detail URL actually carries: the category the catalogue
// filed the work under, its internal row id, and the catalog number. The row id
// is not unique across categories, so all three travel together as the movie ID.
type detailRef struct {
	category string
	itemID   string
	number   string
}

func (r detailRef) id() string {
	return r.category + ":" + r.itemID + ":" + r.number
}

func (r detailRef) valid() bool {
	return r.category != "" && r.itemID != "" && r.number != ""
}

func (p *SevenMMTV) detailURL(ref detailRef) string {
	return fmt.Sprintf("%s/zh/%s_content/%s/%s.html",
		strings.TrimRight(p.baseURL.String(), "/"),
		ref.category, ref.itemID, url.PathEscape(ref.number))
}

// searchURL builds the addressable search page. The keyword goes through
// PathEscape because catalog numbers carry underscores and dots, which the
// catalogue reads literally in this position.
func (p *SevenMMTV) searchURL(keyword string) string {
	return fmt.Sprintf("%s/zh/searchall_search/all/%s/1.html",
		strings.TrimRight(p.baseURL.String(), "/"), url.PathEscape(keyword))
}

// parseDetailRef reads a detail URL. It is the only place that knows the shape
// of the catalogue's paths, so both ParseMovieIDFromURL and GetMovieInfoByURL
// go through it.
func (p *SevenMMTV) parseDetailRef(u *url.URL) (detailRef, bool) {
	if u == nil || !p.validPageOrigin(u) {
		return detailRef{}, false
	}
	match := detailPathRE.FindStringSubmatch(u.Path)
	if len(match) != 4 {
		return detailRef{}, false
	}
	number := strings.TrimSpace(mustUnescape(match[3]))
	if number == "" {
		return detailRef{}, false
	}
	return detailRef{category: match[1], itemID: match[2], number: number}, true
}

func (p *SevenMMTV) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("7mmtv: parse URL: %w", err)
	}
	if ref, ok := p.parseDetailRef(u); ok {
		return p.NormalizeMovieID(ref.id()), nil
	}
	// A search URL is the other addressable form: the catalogue exposes
	// "/zh/searchall_search/all/SSIS-001/1.html" and callers do paste it.
	if match := searchPathRE.FindStringSubmatch(u.Path); len(match) == 2 && p.validPageOrigin(u) {
		keyword := strings.TrimSpace(mustUnescape(match[1]))
		if keyword != "" {
			return p.NormalizeMovieKeyword(keyword), nil
		}
	}
	return "", fmt.Errorf("7mmtv: not a detail or search URL %q", rawURL)
}

func mustUnescape(v string) string {
	out, err := url.PathUnescape(v)
	if err != nil {
		return v
	}
	return out
}

// NormalizeMovieID keeps the composite ID comparable across pages. The category
// and the row id are structural; only the catalog number varies in case between
// the search page and the detail page.
func (p *SevenMMTV) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(id)
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return strings.ToUpper(id)
	}
	return strings.ToLower(parts[0]) + ":" + parts[1] + ":" + strings.ToUpper(parts[2])
}

func (p *SevenMMTV) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.TrimSpace(keyword))
}

func (p *SevenMMTV) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("7mmtv: stopped after 10 redirects")
	}
	if !p.validPageOrigin(req.URL) {
		return fmt.Errorf("7mmtv: refusing redirect to unexpected origin %q", req.URL)
	}
	return nil
}

func (p *SevenMMTV) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *SevenMMTV) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("7mmtv: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("7mmtv: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *SevenMMTV) rebuildClientLocked() {
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
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

func (p *SevenMMTV) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

// validPageOrigin reports whether a URL belongs to the catalogue itself. Both
// "www." and the bare host are accepted: the site links to the bare host, so an
// operator who configures the base URL with "www." would otherwise have every
// page refused.
func (p *SevenMMTV) validPageOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	if !strings.EqualFold(canonicalHost(u.Hostname()), canonicalHost(p.baseURL.Hostname())) {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}

func canonicalHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return strings.TrimPrefix(host, "www.")
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

// isAssetURL reports whether the URL points at media on one of the rotated CDN
// hosts rather than at a catalogue page.
func isAssetURL(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if assetHostRE.MatchString(host) {
		return true
	}
	lower := strings.ToLower(u.Path)
	for _, suffix := range assetSuffixes {
		if strings.HasSuffix(lower, suffix) && assetHostRE.MatchString(host) {
			return true
		}
	}
	return false
}

// Fetch serves both the pages and the artwork. Artwork lives on a different
// registrable domain than the site, so the origin guard has to admit it
// explicitly; a cover that 404s reports ErrImageNotFound so the aggregate layer
// keeps asking the rest of the chain instead of answering "no such title".
func (p *SevenMMTV) Fetch(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("7mmtv: parse request URL: %w", err)
	}
	asset := isAssetURL(u)
	if !asset && !p.validPageOrigin(u) {
		return nil, fmt.Errorf("7mmtv: refusing request to unexpected origin %q", rawURL)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("7mmtv: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	if asset {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("7mmtv: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		if asset {
			return nil, fmt.Errorf("%w: %s", mt.ErrImageNotFound, rawURL)
		}
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("7mmtv: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *SevenMMTV) getHTML(rawURL string) (*goquery.Document, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("7mmtv: parse %s: %w", rawURL, err)
	}
	return doc, nil
}

// SearchMovie reads the search result page. The catalogue answers one entry per
// category a number appears in: a censored title may also have a Chinese-titled
// and a de-mosaicked entry, each with its own row id but the same catalog
// number. Only the first is returned, because a search is looking for the work,
// not for a particular edition; the other editions stay reachable through their
// own composite IDs.
func (p *SevenMMTV) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}
	doc, err := p.getHTML(p.searchURL(keyword))
	if err != nil {
		return nil, err
	}

	results := make([]*model.MovieSearchResult, 0, 3)
	seen := make(map[string]struct{}, 3)
	doc.Find("div.video").Each(func(_ int, card *goquery.Selection) {
		if card.HasClass("video-related") {
			return
		}
		link := card.Find("figure.video-preview a").First()
		href, _ := link.Attr("href")
		if strings.TrimSpace(href) == "" {
			return
		}
		homepage := absoluteURL(p.baseURL, href)
		ref, ok := p.parseDetailRef(mustParseURL(homepage))
		if !ok {
			return
		}
		if _, exists := seen[normalizeNumber(ref.number)]; exists {
			return
		}

		cover := imageSrc(card.Find("figure.video-preview img").First())
		cover = absoluteURL(p.baseURL, cover)
		title := stripLeadingNumber(cleanTitle(card.Find("h3.video-title").First().Text()), ref.number)
		if title == "" {
			title = ref.number
		}
		result := &model.MovieSearchResult{
			ID:          p.NormalizeMovieID(ref.id()),
			Number:      ref.number,
			Title:       title,
			Provider:    p.Name(),
			Homepage:    homepage,
			CoverURL:    cover,
			ThumbURL:    cover,
			ReleaseDate: parser.ParseDate(card.Find(".video-info span.text-muted").First().Text()),
		}
		if !result.IsValid() {
			return
		}
		seen[normalizeNumber(ref.number)] = struct{}{}
		results = append(results, result)
	})
	return results, nil
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return u
}

// GetMovieInfoByID accepts the composite ID this provider issues, a pasted
// detail URL, or a bare catalog number.
func (p *SevenMMTV) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("7mmtv: empty movie ID")
	}
	// The composite ID is tried first: it contains colons, so url.Parse reads
	// it as a scheme ("censored:1:SSIS-001") and would otherwise be handed to
	// the URL branch below.
	if ref, ok := p.refFromID(id); ok {
		return p.getMovieInfoByRef(ref)
	}
	if u, err := url.Parse(id); err == nil && (u.IsAbs() || strings.HasPrefix(id, "/")) {
		if u.Scheme == "" {
			u = p.baseURL.ResolveReference(u)
		}
		return p.GetMovieInfoByURL(u.String())
	}
	// A bare catalog number: resolve it through the search page the same way a
	// user would, then read the first entry that matches.
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if !sameNumber(result.Number, id) {
			continue
		}
		ref, ok := p.refFromID(result.ID)
		if !ok {
			continue
		}
		return p.getMovieInfoByRef(ref)
	}
	return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, id)
}

func (p *SevenMMTV) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	ref, ok := p.parseDetailRef(mustParseURL(rawURL))
	if !ok {
		return nil, fmt.Errorf("7mmtv: not a detail URL %q", rawURL)
	}
	return p.getMovieInfoByRef(ref)
}

// refFromID rebuilds a detail reference from the composite ID.
func (p *SevenMMTV) refFromID(id string) (detailRef, bool) {
	parts := strings.Split(strings.TrimSpace(id), ":")
	if len(parts) != 3 {
		return detailRef{}, false
	}
	ref := detailRef{category: strings.ToLower(strings.TrimSpace(parts[0])), itemID: strings.TrimSpace(parts[1]), number: strings.TrimSpace(parts[2])}
	if !ref.valid() || !categoryRE.MatchString(ref.category) || !itemIDRE.MatchString(ref.itemID) {
		return detailRef{}, false
	}
	return ref, true
}

func (p *SevenMMTV) getMovieInfoByRef(ref detailRef) (*model.MovieInfo, error) {
	homepage := p.detailURL(ref)
	doc, err := p.getHTML(homepage)
	if err != nil {
		if errors.Is(err, mt.ErrMovieNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, homepage)
		}
		return nil, err
	}

	heading := strings.TrimSpace(doc.Find("h1.fullvideo-title").First().Text())
	number := strings.TrimSpace(doc.Find("div.fullvideo-details span.text-muted").First().Text())
	if number == "" {
		number = ref.number
	}
	actors := uniqueTexts(doc.Find("a[href*='_avperformer/']"))
	// The heading is "number + title + performers" in one line. The number and
	// the performers have their own fields, so both are cut away and the title
	// keeps only what is actually a title.
	title := trimActorSuffix(stripLeadingNumber(cleanTitle(heading), number), actors)
	if title == "" {
		title = number
	}

	cover := imageSrc(doc.Find("div.content_main_cover img").First())
	cover = absoluteURL(p.baseURL, cover)

	info := &model.MovieInfo{
		ID:            p.NormalizeMovieID(ref.id()),
		Number:        number,
		Title:         title,
		Provider:      p.Name(),
		Homepage:      homepage,
		CoverURL:      cover,
		ThumbURL:      cover,
		BigCoverURL:   cover,
		BigThumbURL:   cover,
		Maker:         attributeLink(doc, "發行商"),
		Label:         attributeLink(doc, "製作商"),
		Director:      attributeLink(doc, "導演"),
		Actors:        pq.StringArray(actors),
		Genres:        pq.StringArray(uniqueTexts(doc.Find("div.categories a"))),
		PreviewImages: pq.StringArray{},
	}
	info.Summary = strings.TrimSpace(doc.Find("div.fullvideo-description").First().Text())
	info.ReleaseDate = releaseDate(doc)
	if !info.IsValid() {
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, homepage)
	}
	return info, nil
}

// attributeLink reads the value of a labelled attribute row ("發行商:",
// "製作商:", "導演:"). The label and the value sit in the same row, so the row
// is located by its label and the anchor inside it is read.
func attributeLink(doc *goquery.Document, label string) string {
	var value string
	doc.Find("div.fullvideo-attr div.row").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		if !strings.Contains(row.Find("strong").First().Text(), label) {
			return true
		}
		value = strings.TrimSpace(row.Find("a").First().Text())
		return false
	})
	if value == "" {
		doc.Find("div.row").EachWithBreak(func(_ int, row *goquery.Selection) bool {
			if !strings.Contains(row.Find("strong").First().Text(), label) {
				return true
			}
			value = strings.TrimSpace(row.Find("a").First().Text())
			return false
		})
	}
	return value
}

// releaseDate finds the release date among the muted spans that sit under the
// heading, where the catalogue prints the number, the date and the runtime in
// that order.
func releaseDate(doc *goquery.Document) datatypes.Date {
	var out datatypes.Date
	doc.Find("div.fullvideo-details span.text-muted").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := strings.TrimSpace(s.Text())
		if dateRE.MatchString(text) {
			out = parser.ParseDate(text)
			return false
		}
		return true
	})
	return out
}

// stripLeadingNumber removes the catalog number the catalogue prefixes to every
// title. The number already has its own field, so repeating it in the title
// only pushes the real title down.
func stripLeadingNumber(title, number string) string {
	title, number = strings.TrimSpace(title), strings.TrimSpace(number)
	if title == "" || number == "" {
		return title
	}
	if strings.HasPrefix(strings.ToUpper(title), strings.ToUpper(number)) {
		return strings.TrimSpace(title[len(number):])
	}
	return title
}

// trimActorSuffix removes the performer names the catalogue appends to the
// heading. It runs until nothing more can be removed, because a title normally
// ends with more than one performer.
func trimActorSuffix(title string, actors []string) string {
	for {
		trimmed := title
		for _, actor := range actors {
			actor = strings.TrimSpace(actor)
			if actor == "" {
				continue
			}
			if strings.HasSuffix(trimmed, " "+actor) {
				trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, " "+actor))
			}
		}
		if trimmed == title {
			return title
		}
		title = trimmed
	}
}

// imageSrc reads a card's artwork URL. Cards carry it in data-src (lazy
// loading) and fall back to src; both are checked because the catalogue mixes
// the two spellings.
func imageSrc(img *goquery.Selection) string {
	if img == nil || img.Length() == 0 {
		return ""
	}
	if src := strings.TrimSpace(img.AttrOr("data-src", "")); src != "" {
		return src
	}
	return strings.TrimSpace(img.AttrOr("src", ""))
}

// cleanTitle drops the single category tag the catalogue prepends to a shared
// title and collapses the whitespace the markup leaves behind.
func cleanTitle(raw string) string {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	return strings.TrimSpace(labelPrefixRE.ReplaceAllString(raw, ""))
}

func uniqueTexts(nodes *goquery.Selection) []string {
	values := make([]string, 0, 8)
	nodes.Each(func(_ int, node *goquery.Selection) {
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

// absoluteURL resolves a possibly relative URL against the catalogue host.
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

// numberNoise matches the separators that do not distinguish two catalog
// numbers. Underscores are kept significant: "072026_01" and "072026-01" are
// different entries in this catalogue.
var numberNoise = strings.NewReplacer("-", "", " ", "", "\u3000", "")

func normalizeNumber(value string) string {
	return strings.ToLower(numberNoise.Replace(strings.TrimSpace(value)))
}

func sameNumber(a, b string) bool {
	left, right := normalizeNumber(a), normalizeNumber(b)
	return left != "" && left == right
}

func init() {
	mt.Register(Name, New)
}
