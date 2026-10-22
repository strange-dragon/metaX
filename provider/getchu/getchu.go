package getchu

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/antchfx/htmlquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/net/html"
	"golang.org/x/text/language"
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
)

const (
	Name     = "Getchu"
	Priority = 1000
)

const (
	baseURL       = "https://dl.getchu.com/"
	searchBaseURL = "https://www.getchu.com"
	searchPath    = "/php/nsearch.phtml"
	maxCandidates = 10
)

var (
	detailPathRE           = regexp.MustCompile("^/i/item([0-9]+)$")
	catalogRE              = regexp.MustCompile(`(?i)^[A-Z]{2,}[-_ ]?\d+[A-Z0-9_-]*$`)
	numericIDRE            = regexp.MustCompile(`^(?i)(?:GETCHU[-_])?(\d+)$`)
	softPathRE             = regexp.MustCompile(`/soft\.phtml$`)
	searchPathRE           = regexp.MustCompile(`(?i)^[a-z]{2,}[-_ ]?\d+[a-z0-9_-]*$`)
	cookieNameRE           = regexp.MustCompile(`^[!#$%&'*+.^_|~0-9A-Za-z-]+$`)
	searchBaseURLParsed, _ = url.Parse(searchBaseURL)
	detailBaseURLParsed, _ = url.Parse(baseURL)
)

type Getchu struct {
	*scraper.Scraper

	detailBase *url.URL
	searchBase *url.URL

	cookieMu sync.RWMutex
	cookies  []*http.Cookie
}

func New() *Getchu {
	return &Getchu{
		Scraper:    scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese),
		detailBase: cloneURL(detailBaseURLParsed),
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
	return gcu.GetMovieInfoByURL(gcu.detailURL(id))
}

func (gcu *Getchu) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !gcu.validDetailOrigin(u) {
		return "", fmt.Errorf("getchu: unexpected detail origin %q", rawURL)
	}
	if path.Dir(u.Path) != "/i" {
		return "", fmt.Errorf("getchu: invalid detail path %q", u.Path)
	}
	match := detailPathRE.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return "", fmt.Errorf("getchu: invalid detail path %q", u.Path)
	}
	return match[1], nil
}

// SearchMovie implements the legacy www.getchu.com catalog search. Amane's
// first-soft-result shortcut is deliberately not reproduced: every candidate
// must pass an exact normalized catalog/title identity check before it is
// returned.
func (gcu *Getchu) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = gcu.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}

	queryID := gcu.NormalizeMovieID(keyword)
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

func (gcu *Getchu) searchCandidates(keyword string) ([]string, error) {
	u := cloneURL(gcu.searchBase)
	if u == nil {
		return nil, fmt.Errorf("getchu: missing search base URL")
	}
	u.Path = searchPath
	query := url.Values{}
	query.Set("search_keyword", keyword)
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
		switch e.ChildText(`.//td[1]`) {
		case "サークル":
			info.Label = strings.TrimSpace(e.ChildText(`.//td[2]`))
		case "作者":
			// info.Director = e.ChildText(`.//td[2]`)
		case "画像数&ページ数":
			// info.Runtime = parser.ParseRuntime(e.ChildText(`.//td[2]`))
		case "配信開始日":
			info.ReleaseDate = parser.ParseDate(e.ChildText(`.//td[2]`))
		case "趣向":
			parser.ParseTexts(htmlquery.FindOne(e.DOM.(*html.Node), `.//td[2]`),
				(*[]string)(&info.Genres))
		case "作品内容":
			info.Summary = strings.TrimSpace(e.ChildText(`.//td[2]`))
		}
	})

	// Title (fallback)
	c.OnXML(`//meta[@property="og:title"]`, func(e *colly.XMLElement) {
		if info.Title != "" {
			return
		}
		info.Title = e.Attr("content")
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
