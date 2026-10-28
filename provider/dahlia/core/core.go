package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/internal/antibot"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

type Core struct {
	*scraper.Scraper

	// URLs
	BaseURL   string
	MovieURL  string
	SearchURL string

	// Values
	DefaultPriority float64
	DefaultName     string

	// transport routes every scrape through the shared anti-bot layer: these
	// sites answer a plain Go client with a Cloudflare 403.
	transport *antibot.Transport
}

func (core *Core) Init() *Core {
	core.transport = antibot.NewTransport(antibot.Options{
		Timeout:  45 * time.Second,
		AllowURL: core.allowedURL,
	})
	core.Scraper = scraper.NewDefaultScraper(
		core.DefaultName,
		core.BaseURL,
		core.DefaultPriority,
		language.Japanese,
		scraper.WithCookies(core.BaseURL, []*http.Cookie{
			{Name: "modal", Value: "off"},
		}),
		// The browser fallback needs longer than colly's default client timeout.
		scraper.WithRequestTimeout(60*time.Second),
		scraper.WithTransport(core.transport))
	return core
}

// allowedURL keeps redirects and browser navigation inside the provider origin.
// The base URL is the reference, so a test server on http://127.0.0.1 works the
// same way as the production https origin.
func (core *Core) allowedURL(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	base, err := url.Parse(core.BaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, base.Scheme) &&
		strings.EqualFold(u.Hostname(), base.Hostname()) &&
		effectivePort(u) == effectivePort(base)
}

// SetProxy mirrors the proxy onto the anti-bot transport.
func (core *Core) SetProxy(proxyURL string) error {
	if err := core.Scraper.SetProxy(proxyURL); err != nil {
		return err
	}
	core.transport.SetOptions(func(opts *antibot.Options) { opts.ProxyURL = proxyURL })
	return nil
}

// SetRequestTimeout keeps the transport in step with the scraper.
func (core *Core) SetRequestTimeout(timeout time.Duration) {
	core.Scraper.SetRequestTimeout(timeout)
	core.transport.SetOptions(func(opts *antibot.Options) { opts.Timeout = timeout })
}

// Close releases the browser the transport may have started.
func (core *Core) Close() error {
	if core.transport != nil {
		core.transport.Close()
	}
	return nil
}

func (core *Core) NormalizeMovieID(id string) string { return strings.ToLower(id) }

// restWorksEndpoint builds the WordPress REST URL for one work slug.
//
// Both hosts sit behind a WAF that answers the pretty /wp-json/ path with 403
// for foreign traffic, while the equivalent ?rest_route= query form is served
// normally. Everything else about the payload is identical.
func (core *Core) restWorksEndpoint(id string) string {
	// WordPress slugs are lowercase, so a catalog number such as "DLDSS-325"
	// has to be normalized before it can match.
	return strings.TrimRight(core.BaseURL, "/") + "/?rest_route=/wp/v2/works&slug=" +
		url.QueryEscape(core.NormalizeMovieID(id))
}

// verifyMovieSlug checks the WordPress REST API before scraping the detail
// page. A missing slug may otherwise redirect to an unrelated work page.
func (core *Core) verifyMovieSlug(id string) error {
	endpoint := core.restWorksEndpoint(id)
	c := core.ClonedCollector()
	c.ParseHTTPErrorResponse = true
	var slugs []struct {
		Slug string `json:"slug"`
	}
	var (
		decodeErr error
		// The REST route sits behind the host's WAF, which answers foreign
		// traffic with 403 even when the work page itself is public. That is a
		// limitation of the lookup, not a missing work, so the detail page
		// stays the authority.
		unavailable bool
	)
	c.OnResponse(func(r *colly.Response) {
		switch {
		case r.StatusCode == http.StatusForbidden,
			r.StatusCode == http.StatusUnauthorized,
			r.StatusCode == http.StatusTooManyRequests,
			r.StatusCode >= http.StatusInternalServerError:
			unavailable = true
			return
		case r.StatusCode < http.StatusOK || r.StatusCode >= http.StatusMultipleChoices:
			decodeErr = fmt.Errorf("work slug lookup returned HTTP %d", r.StatusCode)
			return
		}
		decodeErr = json.Unmarshal(r.Body, &slugs)
	})
	if err := c.Visit(endpoint); err != nil {
		// A transport-level failure is also a lookup limitation.
		unavailable = true
	}
	if unavailable {
		return nil
	}
	if decodeErr != nil {
		return decodeErr
	}
	for _, post := range slugs {
		if core.NormalizeMovieID(post.Slug) == id {
			return nil
		}
	}
	return fmt.Errorf("work slug %q not found", id)
}

func (core *Core) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return core.GetMovieInfoByURL(fmt.Sprintf(core.MovieURL, id))
}

func (core *Core) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(core.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid provider base URL: %w", err)
	}
	if !strings.EqualFold(homepage.Scheme, base.Scheme) || !strings.EqualFold(homepage.Hostname(), base.Hostname()) ||
		effectivePort(homepage) != effectivePort(base) || homepage.User != nil ||
		homepage.RawQuery != "" || homepage.Fragment != "" {
		return "", fmt.Errorf("invalid detail URL origin or components: %q", rawURL)
	}

	basePath := strings.TrimRight(base.Path, "/")
	prefix := basePath + "/works/"
	if !strings.HasPrefix(homepage.Path, prefix) || !strings.HasSuffix(homepage.Path, "/") {
		return "", fmt.Errorf("invalid detail URL path: %q", rawURL)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(homepage.Path, prefix), "/")
	if id == "" || strings.Contains(id, "/") || id == "." || id == ".." {
		return "", fmt.Errorf("invalid detail URL work slug: %q", rawURL)
	}
	return core.NormalizeMovieID(id), nil
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

func (core *Core) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := core.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	work, err := core.fetchWordPressWork(id)
	if err != nil {
		return nil, err
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        parser.ParseIDToNumber(id),
		Provider:      core.Name(),
		Homepage:      rawURL,
		Maker:         core.Name(),
		Label:         core.Name(),
		Actors:        []string{},
		Genres:        []string{},
		PreviewImages: []string{},
	}

	c := core.ClonedCollector()

	// Title
	c.OnXML(`//div[@class="bar02_works"]/h1/text()`, func(e *colly.XMLElement) {
		info.Title = strings.TrimSpace(e.Text)
	})

	// Title (fallback)
	c.OnXML(`//div[@class="bar02"]/h1/text()`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})

	// Cover
	c.OnXML(`//div[@class="box_works01_img"]/a/img`, func(e *colly.XMLElement) {
		info.CoverURL = strings.Split(e.Request.AbsoluteURL(e.Attr("src")), "?")[0]
	})

	// Preview Video
	c.OnXML(`//div[@class="box_works01_img"]/a`, func(e *colly.XMLElement) {
		info.PreviewVideoURL = e.Request.AbsoluteURL(e.Attr("href"))
	})

	// Previews
	c.OnXML(`//div[@class="box_works01_ga"]//li/a`, func(e *colly.XMLElement) {
		info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(e.Attr("href")))
	})

	// Summary
	c.OnXML(`//div[@class="box_works01_text"]`, func(e *colly.XMLElement) {
		if info.Summary != "" {
			return
		}
		if summary := strings.TrimSpace(e.Text); summary != "" {
			info.Summary = strings.ReplaceAll(summary, "\n", "<br />")
		}
	})

	// Fields
	c.OnXML(`//div[contains(@class, "box_works01_list")]/ul/*[child::span or (@class="view_timer" and not(contains(@style,'display: none')))]//span/parent::*`, func(e *colly.XMLElement) {
		switch e.ChildText(`.//span`) {
		case "出演女優":
			if actors := e.ChildText(`.//p`); actors != "" {
				info.Actors = strings.Split(actors, "/")
			}
		case "収録時間":
			info.Runtime = parser.ParseRuntime(e.ChildText(`.//p`))
		case "配信開始日":
			info.ReleaseDate = parser.ParseDate(e.ChildText(`.//p`))
		case "発売日":
			if time.Time(info.ReleaseDate).IsZero() {
				info.ReleaseDate = parser.ParseDate(e.ChildText(`.//p`))
			}
		}
	})

	// ReleaseDate (fallback)
	c.OnXML(`//div[contains(@class, "box_works01_list")]/ul/div[@class="view_timer" and not(contains(@style,'display: none'))]/li/span[text()="配信開始日"]/following-sibling::p`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Text)
		}
	})

	var identityErr error
	c.OnResponse(func(r *colly.Response) {
		resolvedID := path.Base(r.Request.URL.Path)
		if core.NormalizeMovieID(resolvedID) != id {
			identityErr = fmt.Errorf("requested work %q resolved to different work %q", id, resolvedID)
		}
	})

	c.OnScraped(func(_ *colly.Response) { core.applyWordPressWork(info, work) })
	err = c.Visit(info.Homepage)
	if err == nil {
		err = identityErr
	}
	if err != nil {
		return nil, err
	}
	return
}

type wpRendered struct {
	Rendered string `json:"rendered"`
}

type wpWorkACF struct {
	Title   string `json:"作品名"`
	Actors  string `json:"出演女優"`
	Plot    string `json:"作品紹介文"`
	Runtime string `json:"収録時間"`
	Release string `json:"-"`
}

func (acf *wpWorkACF) UnmarshalJSON(data []byte) error {
	type plain wpWorkACF
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*acf = wpWorkACF(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["発売日（表示用）"]; ok {
		if err := json.Unmarshal(raw, &acf.Release); err != nil {
			return err
		}
	}
	return nil
}

type wpWorkImages struct {
	Main    string   `json:"main_photo_url"`
	Sub     string   `json:"sub_photo_url"`
	Gallery []string `json:"gallery_urls"`
}

type wpWork struct {
	Slug   string       `json:"slug"`
	Title  *wpRendered  `json:"title"`
	ACF    wpWorkACF    `json:"acf"`
	Images wpWorkImages `json:"acf_custom_images"`
}

func (core *Core) fetchWordPressWork(id string) (*wpWork, error) {
	endpoint := core.restWorksEndpoint(id)
	c := core.ClonedCollector()
	c.ParseHTTPErrorResponse = true
	var works []wpWork
	var decodeErr error
	c.OnResponse(func(r *colly.Response) {
		if r.StatusCode < http.StatusOK || r.StatusCode >= http.StatusMultipleChoices {
			decodeErr = fmt.Errorf("work slug lookup returned HTTP %d", r.StatusCode)
			return
		}
		decodeErr = json.Unmarshal(r.Body, &works)
	})
	if err := c.Visit(endpoint); err != nil {
		return nil, err
	}
	if decodeErr != nil {
		return nil, decodeErr
	}
	for i := range works {
		if strings.EqualFold(strings.TrimSpace(works[i].Slug), id) {
			return &works[i], nil
		}
	}
	return nil, ErrWordPressWorkNotFound
}

func splitWordPressActors(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '、' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	actors := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			actors = append(actors, part)
		}
	}
	return actors
}

func (core *Core) applyWordPressWork(info *model.MovieInfo, work *wpWork) {
	actors := splitWordPressActors(work.ACF.Actors)
	title := strings.TrimSpace(work.ACF.Title)
	if title == "" && work.Title != nil {
		title = strings.TrimSpace(work.Title.Rendered)
	}
	for _, actor := range actors {
		if strings.HasSuffix(title, actor) {
			title = strings.TrimSpace(strings.TrimSuffix(title, actor))
			break
		}
	}
	if title != "" {
		info.Title = title
	}
	if len(actors) > 0 {
		info.Actors = actors
	}
	if plot := strings.TrimSpace(work.ACF.Plot); plot != "" {
		info.Summary = plot
	}
	if runtime := parser.ParseRuntime(work.ACF.Runtime); runtime > 0 {
		info.Runtime = runtime
	}
	release := parser.ParseDate(work.ACF.Release)
	if time.Time(info.ReleaseDate).IsZero() && !time.Time(release).IsZero() {
		info.ReleaseDate = release
	}
	if work.Images.Sub != "" {
		info.CoverURL = work.Images.Sub
	}
	if work.Images.Main != "" {
		info.ThumbURL = work.Images.Main
	}
	if len(work.Images.Gallery) > 0 {
		images := make([]string, 0, len(work.Images.Gallery))
		for _, image := range work.Images.Gallery {
			if strings.TrimSpace(image) != "" {
				images = append(images, strings.TrimSpace(image))
			}
		}
		info.PreviewImages = images
	}
	if strings.EqualFold(core.Name(), "FALENO") {
		info.Label = "FALENOstar"
		prefix := work.Slug
		for i, r := range prefix {
			if r >= '0' && r <= '9' {
				prefix = prefix[:i]
				break
			}
		}
		switch strings.ToLower(prefix) {
		case "mgold":
			info.Label = "maryGOLD"
		case "jimmy":
			info.Label = "JimmyScandal"
		}
	}
}
func (core *Core) NormalizeMovieKeyword(keyword string) string {
	return strings.ToLower(strings.ReplaceAll(keyword, "-", ""))
}

var ErrWordPressWorkNotFound = fmt.Errorf("work slug not found")

func slugifyWordPressNumber(number string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(number)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slug.WriteRune(r)
		}
	}
	return slug.String()
}

func wordPressNumberFromSlug(slug string) string {
	value := strings.ToLower(strings.TrimSpace(slug))
	i := 0
	for i < len(value) && value[i] >= 'a' && value[i] <= 'z' {
		i++
	}
	j := i
	for j < len(value) && value[j] >= '0' && value[j] <= '9' {
		j++
	}
	if i == 0 || j == i {
		return strings.ToUpper(slug)
	}
	for k := j; k < len(value); k++ {
		if value[k] < 'a' || value[k] > 'z' {
			return strings.ToUpper(slug)
		}
	}
	return strings.ToUpper(value[:i]) + "-" + value[i:j] + strings.ToUpper(value[j:])
}

func stripWordPressActorSuffix(title string, actors []string) string {
	title = strings.TrimSpace(title)
	for _, actor := range actors {
		if strings.HasSuffix(title, actor) {
			return strings.TrimSpace(strings.TrimSuffix(title, actor))
		}
	}
	return title
}

// SearchMovie uses the same WordPress REST record as detail lookup. The exact
// slug check prevents a fuzzy HTML redirect from being reported as a match.
func (core *Core) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	slug := slugifyWordPressNumber(keyword)
	if slug == "" {
		return nil, nil
	}
	work, err := core.fetchWordPressWork(slug)
	if err == ErrWordPressWorkNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	actors := splitWordPressActors(work.ACF.Actors)
	title := strings.TrimSpace(work.ACF.Title)
	if title == "" && work.Title != nil {
		title = strings.TrimSpace(work.Title.Rendered)
	}
	title = stripWordPressActorSuffix(title, actors)
	result := &model.MovieSearchResult{
		ID:       core.NormalizeMovieID(work.Slug),
		Number:   wordPressNumberFromSlug(work.Slug),
		Title:    title,
		Provider: core.Name(),
		Homepage: fmt.Sprintf(core.MovieURL, work.Slug),
		CoverURL: strings.TrimSpace(work.Images.Sub),
		ThumbURL: strings.TrimSpace(work.Images.Main),
	}
	if release := parser.ParseDate(work.ACF.Release); !time.Time(release).IsZero() {
		result.ReleaseDate = release
	}
	if !result.IsValid() {
		return nil, nil
	}
	return []*model.MovieSearchResult{result}, nil
}
