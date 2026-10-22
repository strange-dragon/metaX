package javbus

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/fetch"
	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*JavBus)(nil)
	_ provider.MovieSearcher = (*JavBus)(nil)
	_ provider.Fetcher       = (*JavBus)(nil)
)

const (
	Name     = "JavBus"
	Priority = 1000 - 5
)

const (
	baseURL             = "https://www.javbus.com/"
	movieURL            = "https://www.javbus.com/ja/%s"
	searchURL           = "https://www.javbus.com/ja/search/%s"
	searchUncensoredURL = "https://www.javbus.com/ja/uncensored/search/%s"
)

type JavBus struct {
	*fetch.Fetcher
	*scraper.Scraper
}

func New() *JavBus {
	return &JavBus{
		Fetcher: fetch.Default(&fetch.Config{Referer: baseURL}),
		Scraper: scraper.NewDefaultScraper(
			Name, baseURL, Priority,
			language.Japanese,
			scraper.WithDisableRedirects(),
			scraper.WithHeaders(map[string]string{
				"Referer": baseURL,
			}),
			scraper.WithCookies(baseURL, []*http.Cookie{
				// existmag=all
				{Name: "dv", Value: "1"},
				{Name: "existmag", Value: "all"},
			})),
	}
}

func (bus *JavBus) NormalizeMovieID(id string) string {
	return strings.ToUpper(id)
}

func (bus *JavBus) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return bus.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (bus *JavBus) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !homepage.IsAbs() || homepage.Host == "" || !strings.EqualFold(homepage.Scheme, "https") {
		return "", fmt.Errorf("javbus: expected an absolute HTTPS detail URL")
	}
	if homepage.User != nil {
		return "", fmt.Errorf("javbus: userinfo is not allowed in detail URLs")
	}
	host := strings.TrimSuffix(strings.ToLower(homepage.Hostname()), ".")
	if host != "javbus.com" && host != "www.javbus.com" {
		return "", fmt.Errorf("javbus: unexpected detail host %q", homepage.Hostname())
	}
	if port := homepage.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("javbus: unexpected detail port %q", port)
	}

	parts := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	var id string
	switch {
	case len(parts) == 1 && parts[0] != "":
		id = parts[0]
	case len(parts) == 2 && parts[0] == "ja" && parts[1] != "":
		id = parts[1]
	default:
		return "", fmt.Errorf("javbus: URL is not a supported detail path")
	}
	return bus.NormalizeMovieID(id), nil
}

func (bus *JavBus) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := bus.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Provider:      bus.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := bus.ClonedCollector()

	// Image+Title
	c.OnXML(`//a[@class="bigImage"]/img`, func(e *colly.XMLElement) {
		info.Title = e.Attr("title")
		info.CoverURL = e.Request.AbsoluteURL(e.Attr("src"))
	})

	// Fields
	c.OnXML(`//div[@class="col-md-3 info"]/p`, func(e *colly.XMLElement) {
		switch e.ChildText(`.//span`) {
		case "品番:":
			info.Number = e.ChildText(`.//span[2]`)
		case "発売日:":
			fields := strings.Fields(e.Text)
			info.ReleaseDate = parser.ParseDate(fields[len(fields)-1])
		case "収録時間:":
			fields := strings.Fields(e.Text)
			info.Runtime = parser.ParseRuntime(fields[len(fields)-1])
		case "監督:":
			info.Director = e.ChildText(`.//a`)
		case "メーカー:":
			info.Maker = e.ChildText(`.//a`)
		case "レーベル:":
			info.Label = e.ChildText(`.//a`)
		case "シリーズ:":
			info.Series = e.ChildText(`.//a`)
		}
	})

	// Genres
	c.OnXML(`//span[@class="genre"]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.ChildText(`.//label/a`)); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// Previews
	c.OnXML(`//*[@id="sample-waterfall"]/a`, func(e *colly.XMLElement) {
		info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(e.Attr("href")))
	})

	// Actors
	c.OnXML(`//div[@class="star-name"]`, func(e *colly.XMLElement) {
		info.Actors = append(info.Actors, e.ChildAttr(`.//a`, "title"))
	})

	// Thumb
	c.OnScraped(func(_ *colly.Response) {
		if re := regexp.MustCompile(`(?i)/cover/([a-z\d]+)(?:_b)?\.(jpg|png)`); re.MatchString(info.CoverURL) {
			var (
				thumb  = re.ReplaceAllString(info.CoverURL, "/thumb/${1}.${2}")
				thumbs = re.ReplaceAllString(info.CoverURL, "/thumbs/${1}.${2}")
			)
			var mu sync.Mutex
			d := c.Clone()
			d.Async = true
			d.ParseHTTPErrorResponse = false
			d.OnScraped(func(r *colly.Response) {
				mu.Lock()
				defer mu.Unlock()
				info.ThumbURL = r.Request.URL.String()
			})
			// Head request to test if is a valid thumb url.
			d.Head(thumb)
			d.Head(thumbs)
			d.Wait()
		}
	})

	if err = c.Visit(info.Homepage); err != nil {
		return nil, err
	}
	if !hasRequiredMovieFields(info.Title, info.Number) {
		return nil, fmt.Errorf("javbus: detail page is missing required title or number")
	}
	return info, nil
}

func (bus *JavBus) NormalizeMovieKeyword(keyword string) string {
	if number.IsSpecial(keyword) && !regexp.MustCompile(`^(?i)([\d-_]{4,}|[a-z]{1,4}\d{2,4}|heyzo[-_].+)$`).MatchString(keyword) {
		return "" // JavBus has no those special contents.
	}
	return strings.ToUpper(keyword)
}

func hasRequiredMovieFields(title, number string) bool {
	return strings.TrimSpace(title) != "" && strings.TrimSpace(number) != ""
}

// foldMovieNumber follows Amane's JavBus matching rules: case, hyphens, and
// spaces are ignored, but underscores remain significant.
func foldMovieNumber(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
}

func splitLeadingDigits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

func movieNumberMatch(found, query string) int {
	if strings.TrimSpace(found) == "" || strings.TrimSpace(query) == "" {
		return 0
	}
	found, query = foldMovieNumber(found), foldMovieNumber(query)
	if found == query {
		return 2
	}
	foundDigits, foundRest := splitLeadingDigits(found)
	queryDigits, queryRest := splitLeadingDigits(query)
	if foundRest == queryRest && !(foundDigits != "" && queryDigits != "") {
		return 1
	}
	return 0
}

func movieNumberVariants(number string) []string {
	variants := []string{number}
	digits, _ := splitLeadingDigits(number)
	if digits != "" {
		variants = append(variants, number[len(digits):])
	} else {
		for _, item := range []struct{ label, digits string }{
			{"LUXU-", "259"}, {"ARA-", "261"}, {"MIUM-", "300"}, {"NTK-", "300"}, {"MAAN-", "300"},
		} {
			if strings.HasPrefix(strings.ToUpper(number), item.label) {
				variants = append(variants, item.digits+number)
				break
			}
		}
	}
	seen := make(map[string]bool, len(variants))
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		if !seen[variant] {
			seen[variant] = true
			out = append(out, variant)
		}
	}
	return out
}

func selectMovieNumberResult(results []*model.MovieSearchResult, query string) []*model.MovieSearchResult {
	exact, loose := -1, -1
	looseURLs := make(map[string]bool)
	for i, result := range results {
		switch movieNumberMatch(result.Number, query) {
		case 2:
			if exact == -1 {
				exact = i
			}
		case 1:
			key := result.Homepage
			if key == "" {
				key = result.ID + "\x00" + result.Number
			}
			if !looseURLs[key] {
				looseURLs[key] = true
				loose = i
			}
		}
	}
	if exact >= 0 {
		return []*model.MovieSearchResult{results[exact]}
	}
	if len(looseURLs) == 1 {
		return []*model.MovieSearchResult{results[loose]}
	}
	return nil
}

func (bus *JavBus) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := bus.ClonedCollector()
	c.Async = true /* ASYNC */

	var mu sync.Mutex
	c.OnXML(`//a[@class="movie-box"]`, func(e *colly.XMLElement) {
		mu.Lock()
		defer mu.Unlock()

		var thumb, cover string
		thumb = e.Request.AbsoluteURL(e.ChildAttr(`.//div[1]/img`, "src"))
		if re := regexp.MustCompile(`(?i)/thumbs?/([a-z\d]+)(?:_b)?\.(jpg|png)`); re.MatchString(thumb) {
			cover = re.ReplaceAllString(thumb, "/cover/${1}_b.${2}") // guess
		}

		homepage := e.Request.AbsoluteURL(e.Attr("href"))
		id, _ := bus.ParseMovieIDFromURL(homepage)
		resultNumber := strings.TrimSpace(e.ChildText(`.//div[2]/span/date[1]`))
		results = append(results, &model.MovieSearchResult{
			ID:          id,
			Number:      resultNumber,
			Title:       strings.SplitN(e.ChildText(`.//div[2]/span`), "\n", 2)[0],
			Provider:    bus.Name(),
			Homepage:    homepage,
			ThumbURL:    thumb,
			CoverURL:    cover,
			ReleaseDate: parser.ParseDate(e.ChildText(`.//div[2]/span/date[2]`)),
		})
	})

	for _, variant := range movieNumberVariants(keyword) {
		for _, u := range []string{
			fmt.Sprintf(searchURL, variant),
			fmt.Sprintf(searchUncensoredURL, variant),
		} {
			if err = c.Visit(u); err != nil {
				return nil, err
			}
		}
	}
	c.Wait()
	return selectMovieNumberResult(results, keyword), nil
}

func init() {
	provider.Register(Name, New)
}
