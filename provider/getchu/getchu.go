package getchu

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/language"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*Getchu)(nil)
	_ provider.MovieSearcher = (*Getchu)(nil)
	_ provider.ConfigSetter  = (*Getchu)(nil)
	_ provider.Fetcher       = (*Getchu)(nil)
)

const (
	Name     = "Getchu"
	Priority = 1000
)

const (
	baseURL       = "https://dl.getchu.com/"
	webBaseURL    = "https://www.getchu.com/"
	searchBaseURL = "https://www.getchu.com"
	searchPath    = "/php/search.phtml"
	maxCandidates = 10
)

var (
	detailPathRE           = regexp.MustCompile("^/i/item([0-9]+)$")
	webDetailPathRE        = regexp.MustCompile("^/item/([0-9]+)/?$")
	catalogRE              = regexp.MustCompile(`(?i)^[A-Z]{2,}[-_ ]?\d+[A-Z0-9_-]*$`)
	numericIDRE            = regexp.MustCompile(`^(?i)(?:GETCHU[-_])?(\d+)$`)
	softPathRE             = regexp.MustCompile(`/soft\.phtml$`)
	webImagePathRE         = regexp.MustCompile(`^/brandnew/([0-9]+)/`)
	searchPathRE           = regexp.MustCompile(`(?i)^[a-z]{2,}[-_ ]?\d+[a-z0-9_-]*$`)
	cookieNameRE           = regexp.MustCompile(`^[!#$%&'*+.^_|~0-9A-Za-z-]+$`)
	searchBaseURLParsed, _ = url.Parse(searchBaseURL)
	detailBaseURLParsed, _ = url.Parse(baseURL)
	webBaseURLParsed, _    = url.Parse(webBaseURL)
)

type Getchu struct {
	*scraper.Scraper

	detailBase *url.URL
	webBase    *url.URL
	searchBase *url.URL

	cookieMu sync.RWMutex
	cookies  []*http.Cookie
}

func New() *Getchu {
	return &Getchu{
		Scraper:    scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese),
		detailBase: cloneURL(detailBaseURLParsed),
		webBase:    cloneURL(webBaseURLParsed),
		searchBase: cloneURL(searchBaseURLParsed),
		cookies:    defaultCookies(),
	}
}

func newForTest(detailBase, searchBase string) *Getchu {
	detail, _ := url.Parse(detailBase)
	search, _ := url.Parse(searchBase)
	return &Getchu{
		Scraper:    scraper.NewDefaultScraper(Name, detailBase, Priority, language.Japanese),
		detailBase: detail,
		webBase:    cloneURL(search),
		searchBase: search,
		cookies:    defaultCookies(),
	}
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	copy := *u
	return &copy
}

func defaultCookies() []*http.Cookie {
	return []*http.Cookie{
		{Name: "getchu_adalt_flag", Value: "getchu.com", Path: "/"},
		{Name: "gc", Value: "gc", Path: "/"},
	}
}

func (gcu *Getchu) NormalizeMovieID(id string) string {
	if ss := numericIDRE.FindStringSubmatch(id); len(ss) == 2 {
		return ss[1]
	}
	return ""
}

func (gcu *Getchu) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(norm.NFKC.String(keyword))
}

func (gcu *Getchu) SetConfig(config provider.Config) error {
	if err := gcu.Scraper.SetConfig(config); err != nil {
		return err
	}
	if config == nil {
		return nil
	}
	var raw string
	switch {
	case config.Has("cookies"):
		value, err := config.GetString("cookies")
		if err != nil {
			return err
		}
		raw = value
	case config.Has("cookie"):
		value, err := config.GetString("cookie")
		if err != nil {
			return err
		}
		raw = value
	}
	cookies, err := parseCookieHeader(raw)
	if err != nil {
		return err
	}
	gcu.cookieMu.Lock()
	gcu.cookies = mergeCookies(defaultCookies(), cookies)
	gcu.cookieMu.Unlock()
	return nil
}

func parseCookieHeader(raw string) ([]*http.Cookie, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || !cookieNameRE.MatchString(name) {
			return nil, fmt.Errorf("getchu: invalid cookie setting")
		}
		if strings.ContainsAny(name+value, "\r\n") {
			return nil, fmt.Errorf("getchu: cookie setting must not contain CR or LF")
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	return cookies, nil
}

func mergeCookies(base, overrides []*http.Cookie) []*http.Cookie {
	result := make([]*http.Cookie, 0, len(base)+len(overrides))
	index := make(map[string]int, len(base)+len(overrides))
	for _, cookie := range append(append([]*http.Cookie{}, base...), overrides...) {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		value := *cookie
		if position, ok := index[value.Name]; ok {
			result[position] = &value
			continue
		}
		index[value.Name] = len(result)
		result = append(result, &value)
	}
	return result
}

func (gcu *Getchu) configuredCookies() []*http.Cookie {
	gcu.cookieMu.RLock()
	defer gcu.cookieMu.RUnlock()
	result := make([]*http.Cookie, 0, len(gcu.cookies))
	for _, cookie := range gcu.cookies {
		if cookie == nil {
			continue
		}
		value := *cookie
		result = append(result, &value)
	}
	return result
}

// Fetch fetches Getchu media with the same age/configuration cookies used for
// detail pages. Retail cover images are hotlink-protected and require a
// referer pointing at the matching item page; without it the image host
// returns 403 even when the cookies are present.
func (gcu *Getchu) Fetch(raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("getchu: parse image URL: %w", err)
	}
	if !gcu.validWebOrigin(u) && !gcu.validDetailOrigin(u) {
		return nil, fmt.Errorf("getchu: refusing image request to unexpected origin %q", raw)
	}

	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("getchu: create image request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en-US;q=0.9,en;q=0.8")
	if referer := gcu.imageReferer(u); referer != "" {
		req.Header.Set("Referer", referer)
	}
	for _, cookie := range gcu.configuredCookies() {
		req.AddCookie(cookie)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("getchu: stopped after 10 redirects")
			}
			if !gcu.validWebOrigin(next.URL) && !gcu.validDetailOrigin(next.URL) {
				return fmt.Errorf("getchu: refusing redirect to unexpected origin %q", next.URL)
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getchu: GET %s: %w", raw, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", provider.ErrImageNotFound, raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("getchu: GET %s: unexpected HTTP status %s", raw, resp.Status)
	}
	return resp, nil
}

func (gcu *Getchu) imageReferer(u *url.URL) string {
	if gcu.validWebOrigin(u) {
		if match := webImagePathRE.FindStringSubmatch(u.Path); len(match) == 2 {
			return gcu.webDetailURL(match[1])
		}
		if gcu.webBase != nil {
			return gcu.webBase.String()
		}
	}
	if gcu.validDetailOrigin(u) {
		if match := detailPathRE.FindStringSubmatch(u.Path); len(match) == 2 {
			return gcu.detailURL(match[1])
		}
		if gcu.detailBase != nil {
			return gcu.detailBase.String()
		}
	}
	return ""
}

func (gcu *Getchu) detailURL(id string) string {
	u := cloneURL(gcu.detailBase)
	if u == nil {
		return ""
	}
	u.Path = "/i/item" + id
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (gcu *Getchu) webDetailURL(id string) string {
	u := cloneURL(gcu.webBase)
	if u == nil {
		return ""
	}
	u.Path = "/item/" + id + "/"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (gcu *Getchu) validDetailOrigin(u *url.URL) bool {
	if u == nil || gcu.detailBase == nil || u.User != nil || u.Host == "" ||
		!strings.EqualFold(u.Scheme, gcu.detailBase.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(gcu.detailBase.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(gcu.detailBase)
}

func (gcu *Getchu) validWebOrigin(u *url.URL) bool {
	if u == nil || gcu.webBase == nil || u.User != nil || u.Host == "" ||
		!strings.EqualFold(u.Scheme, gcu.webBase.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(gcu.webBase.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(gcu.webBase)
}

func (gcu *Getchu) validSearchOrigin(u *url.URL) bool {
	if u == nil || gcu.searchBase == nil || u.User != nil || u.Host == "" ||
		!strings.EqualFold(u.Scheme, gcu.searchBase.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(gcu.searchBase.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(gcu.searchBase)
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

func (gcu *Getchu) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	id = gcu.NormalizeMovieID(id)
	if id == "" {
		return nil, fmt.Errorf("getchu: invalid movie ID")
	}
	// Download items live on dl.getchu.com. Search results point at the retail
	// catalog on www.getchu.com, which uses a different detail URL and page
	// layout. Try both and keep the first complete response.
	for _, rawURL := range []string{gcu.detailURL(id), gcu.webDetailURL(id)} {
		if rawURL == "" {
			continue
		}
		candidate, candidateErr := gcu.GetMovieInfoByURL(rawURL)
		if candidateErr == nil && candidate != nil && candidate.IsValid() {
			return candidate, nil
		}
		if candidateErr != nil {
			err = candidateErr
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("getchu: movie not found: %s", id)
}

func (gcu *Getchu) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if gcu.validDetailOrigin(u) {
		if path.Dir(u.Path) != "/i" {
			return "", fmt.Errorf("getchu: invalid detail path %q", u.Path)
		}
		if match := detailPathRE.FindStringSubmatch(u.Path); len(match) == 2 {
			return match[1], nil
		}
		return "", fmt.Errorf("getchu: invalid detail path %q", u.Path)
	}
	if gcu.validWebOrigin(u) {
		if match := webDetailPathRE.FindStringSubmatch(u.Path); len(match) == 2 {
			return match[1], nil
		}
		return "", fmt.Errorf("getchu: invalid detail path %q", u.Path)
	}
	return "", fmt.Errorf("getchu: unexpected detail origin %q", rawURL)
}

// SearchMovie implements title and catalog searches. Title searches are read
// directly from the result list (avoiding N+1 detail requests); catalog
// searches still verify every candidate against the exact normalized number
// before returning it.
func (gcu *Getchu) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = gcu.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}

	queryID := gcu.NormalizeMovieID(keyword)
	if queryID == "" && !catalogRE.MatchString(keyword) {
		// Title searches can be answered directly from the result list. This
		// avoids fetching up to ten detail pages just to discover that a result
		// is unrelated, which made Japanese title searches take tens of
		// seconds. Catalog searches keep the exact detail-based path below.
		return gcu.searchTitleResults(keyword)
	}
	candidates, searchErr := gcu.searchCandidates(keyword)
	if searchErr != nil && queryID == "" {
		return nil, searchErr
	}
	if queryID == "" && len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	results := make([]*model.MovieSearchResult, 0, 1)
	seen := map[string]struct{}{}
	addCandidate := func(id string) bool {
		if id == "" {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
		info, err := gcu.GetMovieInfoByID(id)
		if err != nil {
			return false
		}
		if !matchesQuery(keyword, queryID, info) {
			return false
		}
		results = append(results, info.ToSearchResult())
		return len(results) >= maxCandidates
	}

	for _, id := range candidates {
		if queryID != "" && id != queryID {
			continue
		}
		if addCandidate(id) {
			return results, nil
		}
	}

	// Exact numeric IDs are also valid direct detail IDs. This preserves the
	// stock GETCHU-<digits> fallback when catalog search is unavailable, while
	// non-numeric searches never fall back to a fuzzy first result.
	if queryID != "" {
		addCandidate(queryID)
	}
	if len(results) > 0 {
		return results, nil
	}
	if searchErr != nil {
		return nil, searchErr
	}
	return nil, nil
}

func encodeEUCJP(value string) (string, error) {
	encoded, _, err := transform.String(japanese.EUCJP.NewEncoder(), value)
	return encoded, err
}

func matchesTitleQuery(keyword, title string) bool {
	query := normalizeIdentity(keyword)
	title = normalizeIdentity(title)
	return query != "" && title != "" && strings.Contains(title, query)
}

func largerGetchuImage(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, suffix := range []string{"_ss.jpg", "_s.jpg"} {
		if strings.HasSuffix(raw, suffix) {
			return strings.TrimSuffix(raw, suffix) + ".jpg"
		}
	}
	return raw
}

func (gcu *Getchu) searchTitleResults(keyword string) ([]*model.MovieSearchResult, error) {
	u := cloneURL(gcu.searchBase)
	if u == nil {
		return nil, fmt.Errorf("getchu: missing search base URL")
	}
	u.Path = searchPath
	encodedKeyword, err := encodeEUCJP(keyword)
	if err != nil {
		// Getchu's catalog is EUC-JP. Simplified Chinese (and other text that
		// cannot be represented there) is not searchable; let the other
		// providers in the category answer instead of failing the fan-out.
		return nil, nil
	}
	query := url.Values{}
	query.Set("search_keyword", encodedKeyword)
	query.Set("check_key_dtl", "1")
	query.Set("genre", "anime_dvd")
	query.Set("gc", "gc")
	u.RawQuery = query.Encode()
	u.Fragment = ""

	c := gcu.ClonedCollector()
	if err := c.SetCookies(gcu.searchBase.String(), gcu.configuredCookies()); err != nil {
		return nil, fmt.Errorf("getchu: set search cookies: %w", err)
	}
	c.AllowURLRevisit = true

	results := make([]*model.MovieSearchResult, 0, maxCandidates)
	seen := map[string]struct{}{}
	releaseDateRE := regexp.MustCompile(`発売日：\s*(\d{4}/\d{2}/\d{2})`)
	c.OnHTML("ul.display > li", func(e *colly.HTMLElement) {
		link := e.ChildAttr(`#detail_block a.blueb`, "href")
		id, ok := gcu.softResultID(link)
		if !ok {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		title := strings.TrimSpace(e.ChildText(`#detail_block a.blueb`))
		if title == "" {
			return
		}
		info := &model.MovieInfo{
			ID:       id,
			Number:   fmt.Sprintf("GETCHU-%s", id),
			Title:    title,
			Provider: gcu.Name(),
			Homepage: gcu.webDetailURL(id),
			Genres:   []string{},
		}
		if raw := strings.TrimSpace(e.ChildAttr(`#package_block img`, "data-original")); raw != "" {
			ref, refErr := url.Parse(raw)
			if refErr == nil {
				info.CoverURL = gcu.searchBase.ResolveReference(ref).String()
				info.ThumbURL = largerGetchuImage(info.CoverURL)
			}
		}
		if match := releaseDateRE.FindStringSubmatch(e.Text); len(match) == 2 {
			info.ReleaseDate = parser.ParseDate(match[1])
		}
		if !matchesTitleQuery(keyword, info.Title) {
			return
		}
		results = append(results, info.ToSearchResult())
	})
	if err := c.Visit(u.String()); err != nil {
		return nil, fmt.Errorf("getchu: title search: %w", err)
	}
	if len(results) > maxCandidates {
		results = results[:maxCandidates]
	}
	return results, nil
}

func (gcu *Getchu) searchCandidates(keyword string) ([]string, error) {
	u := cloneURL(gcu.searchBase)
	if u == nil {
		return nil, fmt.Errorf("getchu: missing search base URL")
	}
	u.Path = searchPath
	encodedKeyword, err := encodeEUCJP(keyword)
	if err != nil {
		// Getchu's catalog is EUC-JP. Simplified Chinese (and other text that
		// cannot be represented there) is not searchable; let the other
		// providers in the category answer instead of failing the fan-out.
		return nil, nil
	}
	query := url.Values{}
	query.Set("search_keyword", encodedKeyword)
	query.Set("check_key_dtl", "1")
	query.Set("genre", "anime_dvd")
	query.Set("gc", "gc")
	u.RawQuery = query.Encode()
	u.Fragment = ""

	c := gcu.ClonedCollector()
	if err := c.SetCookies(gcu.searchBase.String(), gcu.configuredCookies()); err != nil {
		return nil, fmt.Errorf("getchu: set search cookies: %w", err)
	}
	c.AllowURLRevisit = true

	ids := make([]string, 0)
	seen := map[string]struct{}{}
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		id, ok := gcu.softResultID(e.Attr("href"))
		if !ok {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	})
	if err := c.Visit(u.String()); err != nil {
		return nil, fmt.Errorf("getchu: catalog search: %w", err)
	}
	return ids, nil
}

func (gcu *Getchu) softResultID(raw string) (string, bool) {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	absolute := gcu.searchBase.ResolveReference(ref)
	if !gcu.validSearchOrigin(absolute) || !softPathRE.MatchString(absolute.Path) {
		return "", false
	}
	id := absolute.Query().Get("id")
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(id) {
		return "", false
	}
	return id, true
}

func matchesQuery(keyword, queryID string, info *model.MovieInfo) bool {
	if info == nil || strings.TrimSpace(info.Title) == "" {
		return false
	}
	if queryID != "" {
		return info.ID == queryID
	}
	normalizedQuery := normalizeIdentity(keyword)
	if normalizedQuery == "" {
		return false
	}
	if normalizeIdentity(info.Title) == normalizedQuery {
		return true
	}
	if normalizedQuery == normalizeIdentity(leadingToken(info.Title)) {
		return true
	}
	if catalogRE.MatchString(keyword) {
		for _, token := range strings.Fields(info.Title) {
			if normalizeIdentity(token) == normalizedQuery {
				return true
			}
		}
	}
	return false
}

func leadingToken(title string) string {
	fields := strings.Fields(strings.TrimSpace(title))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], "[]【】()（）「」『』:：")
}

func normalizeIdentity(value string) string {
	value = strings.ToLower(norm.NFKC.String(strings.TrimSpace(value)))
	replacer := strings.NewReplacer(
		" ", "", "　", "", "\t", "", "\n", "", "\r", "",
		"-", "", "_", "", ".", "", ":", "", "：", "",
		"[", "", "]", "", "【", "", "】", "", "(", "", ")", "",
		"（", "", "）", "", "「", "", "」", "", "『", "", "』", "",
	)
	return replacer.Replace(value)
}

func (gcu *Getchu) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := gcu.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("GETCHU-%s", id),
		Provider:      gcu.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := gcu.ClonedCollector()
	for _, base := range []*url.URL{gcu.detailBase, gcu.webBase} {
		if base == nil {
			continue
		}
		if err := c.SetCookies(base.String(), gcu.configuredCookies()); err != nil {
			return nil, fmt.Errorf("getchu: set detail cookies: %w", err)
		}
	}

	// Misc
	c.OnXML(`//td`, func(e *colly.XMLElement) {
		switch {
		// Title
		case e.ChildAttr(`.//div`, "style") == "color: #333333; padding: 3px 0px 0px 5px;":
			info.Title = strings.TrimSpace(e.Text)
		// Cover
		case e.Attr("bgcolor") == "#ffffff":
			info.CoverURL = e.Request.AbsoluteURL(e.ChildAttr(`.//img`, "src"))
		// Preview Images
		case strings.Contains(e.Attr("style"), "background-color: #444444;"):
			info.PreviewImages = append(info.PreviewImages,
				e.Request.AbsoluteURL(e.ChildAttr(`.//a`, "href")))
		}
	})

	// Fields
	c.OnXML(`//tr`, func(e *colly.XMLElement) {
		key := strings.TrimSuffix(strings.TrimSpace(e.ChildText(`.//td[1]`)), "：")
		switch key {
		case "サークル":
			info.Label = strings.TrimSpace(e.ChildText(`.//td[2]`))
		case "作者":
			// info.Director = e.ChildText(`.//td[2]`)
		case "画像数&ページ数":
			// info.Runtime = parser.ParseRuntime(e.ChildText(`.//td[2]`))
		case "配信開始日", "発売日":
			info.ReleaseDate = parser.ParseDate(e.ChildText(`.//td[2]`))
		case "ブランド":
			info.Label = strings.TrimSpace(e.ChildText(`.//td[2]`))
		case "ジャンル":
			parser.ParseTexts(htmlquery.FindOne(e.DOM.(*html.Node), `.//td[2]`),
				(*[]string)(&info.Genres))
		case "出演":
			info.Actors = strings.Fields(e.ChildText(`.//td[2]`))
		case "趣向":
			parser.ParseTexts(htmlquery.FindOne(e.DOM.(*html.Node), `.//td[2]`),
				(*[]string)(&info.Genres))
		case "作品内容", "商品紹介":
			info.Summary = strings.TrimSpace(e.ChildText(`.//td[2]`))
		}
	})

	// Title (fallback)
	c.OnXML(`//meta[@property="og:title"]`, func(e *colly.XMLElement) {
		if info.Title != "" {
			return
		}
		title := strings.TrimSpace(e.Attr("content"))
		if i := strings.Index(title, " | "); i > 0 {
			title = strings.TrimSpace(title[:i])
		}
		info.Title = title
	})

	// Cover (fallback)
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if info.CoverURL != "" {
			return
		}
		info.CoverURL = e.Request.AbsoluteURL(e.Attr("content"))
	})

	// Fallbacks
	c.OnScraped(func(_ *colly.Response) {
		if info.ThumbURL == "" {
			info.ThumbURL = info.CoverURL // same as cover.
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func init() {
	provider.Register(Name, New)
}
