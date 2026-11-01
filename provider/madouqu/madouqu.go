package madouqu

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*MadouQu)(nil)
	_ provider.MovieSearcher = (*MadouQu)(nil)
)

const (
	Name     = "MadouQu"
	Priority = 0 // Disabled by default, use `export MT_MOVIE_PROVIDER_MADOUQU__PRIORITY=1000` to enable.
)

const (
	baseURL   = "https://madouqu.com/"
	movieURL  = "https://madouqu.com/%s/"
	searchURL = "https://madouqu.com/?s=%s"
)

// The 2026 redesign dropped id="post-N" from <article>, so every selector that
// anchored on it matched nothing and the provider answered with an empty result
// set and no error. The class names are the stable hooks now; the id-based
// selectors are kept alongside them for pages still on the old template.
//
// A detail page also carries related-work cards, so the entry article and the
// search card must not be told apart by "post" alone.
const (
	entryArticleXPath = `//article[contains(concat(' ', normalize-space(@class), ' '), ' entry ')]`
	cardArticleXPath  = `//article[contains(concat(' ', normalize-space(@class), ' '), ' resource-card ')]`
)

type MadouQu struct {
	*scraper.Scraper
}

func New() *MadouQu {
	return &MadouQu{scraper.NewDefaultScraper(Name, baseURL, Priority, language.Chinese)}
}

func (mdq *MadouQu) SetRequestTimeout(_ time.Duration) {
	mdq.Scraper.SetRequestTimeout(10 * time.Second) // force timeout setting.
}

func (mdq *MadouQu) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return mdq.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (mdq *MadouQu) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return scraper.LastPathSegment(homepage.Path, ""), nil
}

func (mdq *MadouQu) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := mdq.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Provider:      mdq.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := mdq.ClonedCollector()
	gone := scraper.TrackNotFound(c)

	paragraph := func(e *colly.XMLElement) {
		// The image used to return early, so a paragraph that carried both the
		// artwork and a labelled value lost the value.
		if src := e.ChildAttr(`./img`, "src"); src != "" {
			if info.CoverURL == "" {
				info.CoverURL = e.Request.AbsoluteURL(ExtractImgSrc(src))
			}
		}

		switch {
		case strings.Contains(e.Text, "番號"):
			if info.Number == "" {
				info.Number = detailValue(e.Text)
			}
		case strings.Contains(e.Text, "片名"):
			if info.Title == "" {
				info.Title = detailValue(e.Text)
			}
		case strings.Contains(e.Text, "女郎"):
			for _, actor := range strings.Split(detailValue(e.Text), "、") {
				if actor = strings.TrimSpace(actor); actor != "" {
					info.Actors = append(info.Actors, actor)
				}
			}
		}
	}
	c.OnXML(entryArticleXPath+`//div[@class="entry-content"]//p`, paragraph)
	c.OnXML(`//article[starts-with(@id,'post')]//div[@class="container"]//p`, paragraph)

	actorTags := make([]string, 0, 2)

	// Tags (Actors)
	actorTag := func(e *colly.XMLElement) {
		actorTags = append(actorTags, strings.TrimSpace(e.Text))
	}
	c.OnXML(entryArticleXPath+`//a[@class="post-tag-label"]`, actorTag)
	c.OnXML(`//article[starts-with(@id,'post')]//div[@class="entry-tags"]/a`, actorTag)

	// Maker
	maker := func(e *colly.XMLElement) {
		if info.Maker == "" {
			info.Maker = strings.TrimSpace(e.Text)
		}
	}
	c.OnXML(entryArticleXPath+`//a[@class="category-label"]`, maker)
	c.OnXML(`//article[starts-with(@id,'post')]//span[@class="meta-category"]`, func(e *colly.XMLElement) {
		if info.Maker == "" {
			info.Maker = strings.TrimSpace(e.ChildText(`./a[1]`))
		}
	})

	// Release date, and a title for the pages whose body omits the 片名 line.
	// The heading is only a fallback: it also carries the catalog number, and it
	// is parsed before the body, so storing it directly would shadow 片名.
	headline := ""
	c.OnXML(entryArticleXPath+`//header//time`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Attr("datetime"))
		}
	})
	c.OnXML(entryArticleXPath+`//h1`, func(e *colly.XMLElement) {
		headline = strings.TrimSpace(e.Text)
	})

	// Fallback
	c.OnScraped(func(_ *colly.Response) {
		// Number = Upper ID
		if info.Number == "" {
			info.Number = parser.ParseIDToNumber(info.ID)
		}

		if info.Title == "" {
			info.Title = headline
		}

		// Thumb Image
		if info.ThumbURL == "" {
			info.ThumbURL = info.CoverURL // same as cover
		}

		// Actors
		if len(info.Actors) == 0 && len(actorTags) > 0 {
			info.Actors = actorTags // fallback
		}
	})

	err = gone.Resolve(mdq.Name(), c.Visit(info.Homepage))
	return
}

func (mdq *MadouQu) NormalizeMovieKeyword(keyword string) string {
	if number.IsSpecial(keyword) {
		return ""
	}
	return strings.ToUpper(keyword)
}

func (mdq *MadouQu) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := mdq.ClonedCollector()

	card := func(e *colly.XMLElement) {
		link := e.ChildAttr(`.//h2/a`, "href")
		id, idErr := mdq.ParseMovieIDFromURL(link)
		if idErr != nil {
			return
		}

		// The title attribute now holds "新标签页打开" on every link, so the
		// catalog number and the title have to come from the anchor text.
		origTitle := strings.TrimSpace(e.ChildText(`.//h2/a`))
		if origTitle == "" {
			origTitle = e.ChildAttr(`.//h2/a`, "title")
		}
		nb, title, _ := strings.Cut(origTitle, " ")

		if !madouquNumberRE.MatchString(nb) {
			nb = parser.ParseIDToNumber(id)
			title = origTitle
		}
		// Some works are published without a title at all; the number is the
		// only thing left to show, and an empty title would fail validation.
		if title == "" {
			title = nb
		}

		thumb := ExtractImgSrc(e.ChildAttr(`.//div[@class="entry-media"]//a/img`, "data-src"))
		if thumb == "" {
			thumb = ExtractImgSrc(firstNonEmpty(
				e.ChildAttr(`.//a[contains(@class,"cover-link")]//img`, "data-src"),
				e.ChildAttr(`.//a[contains(@class,"cover-link")]//img`, "src"),
			))
		}
		release := e.ChildAttr(`.//li/time`, "datetime")
		if release == "" {
			release = e.ChildAttr(`.//div[@class="card-meta"]/time`, "datetime")
		}

		results = append(results, &model.MovieSearchResult{
			ID:          id,
			Number:      nb,
			Title:       title,
			Provider:    mdq.Name(),
			Homepage:    link,
			ThumbURL:    thumb,
			CoverURL:    thumb, // same as thumb
			ReleaseDate: parser.ParseDate(release),
		})
	}
	c.OnXML(cardArticleXPath, card)
	c.OnXML(`//article[starts-with(@id, 'post')]`, card)

	if vErr := c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); vErr != nil {
		err = vErr
	}
	return
}

var madouquNumberRE = regexp.MustCompile(`^(?i)[A-Z0-9_-]+$`)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// wrappedImageRE finds an absolute URL that the site wrapped in its own image
// proxy, e.g. https://madouqu.com/https://cdn.example/cover.jpg.
var wrappedImageRE = regexp.MustCompile(`(https?://.+$)`)

// ExtractImgSrc unwraps the site's image proxy. Lazy-loaded images often keep
// the real target in the query string rather than the path, so both are tried.
func ExtractImgSrc(src string) string {
	u, err := url.Parse(src)
	if err != nil {
		return src
	}
	if ss := wrappedImageRE.FindStringSubmatch(u.Path); len(ss) > 0 {
		return ss[1]
	}
	if ss := wrappedImageRE.FindStringSubmatch(u.RawQuery); len(ss) > 0 {
		return ss[1]
	}
	return src
}

// detailValue returns the text after a label separator. The site mixes the full
// width colon with the half width one, and every value used to be stored with
// its leading whitespace intact, so a number spelled " MD-0123" never matched a
// search for "MD-0123".
func detailValue(text string) string {
	for _, separator := range []string{"：", ":"} {
		if _, value, ok := strings.Cut(text, separator); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func init() {
	provider.Register(Name, New)
}
