package fc2

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"
	"gorm.io/datatypes"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/fc2/fc2util"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var _ provider.MovieProvider = (*FC2)(nil)

const (
	Name     = "FC2"
	Priority = 1000
)

const (
	baseURL   = "https://adult.contents.fc2.com/"
	movieURL  = "https://adult.contents.fc2.com/article/%s/"
	sampleURL = "https://adult.contents.fc2.com/api/v2/videos/%s/sample"
)

// FC2 answers requests for the article page from some regions with a 302 to
// redirect.fc2.com/ekyc_auth — its identity-document age check — while the same
// page renders normally from others and the artwork CDN hosts are never gated.
// A container in Frankfurt or Washington therefore cannot read a single FC2
// article, and no header, cookie or path variation changes that: the gate is
// decided per egress region, not per request.
//
// readerPrefix fronts the page through a fetch service that uses its own
// egress. The document comes back unchanged, so the selectors, the artwork
// URLs and the absolute-URL resolution below all keep working as they are.
const (
	readerPrefix = "https://r.jina.ai/"
	readerEnv    = "MT_MOVIE_PROVIDER_FC2__READER"
)

var errEKYCBlocked = errors.New("fc2: article page is gated by eKYC age verification")

// readerMode decides when the reader is used. The default asks for it only
// after FC2 has actually blocked the request, so an egress that can read the
// page never sends anything to a third party.
type readerMode uint8

const (
	readerOnReaderBlock readerMode = iota // default: only when FC2 blocks us
	readerOff
	readerAlways
)

func activeReaderMode() readerMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(readerEnv))) {
	case "0", "off", "false", "no", "disable", "disabled":
		return readerOff
	case "force", "always":
		return readerAlways
	default:
		return readerOnReaderBlock
	}
}

func isEKYCURL(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), "redirect.fc2.com") ||
		strings.Contains(u.Path, "/ekyc_auth")
}

func isReaderURL(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), "r.jina.ai")
}

type FC2 struct {
	*scraper.Scraper
}

func New() *FC2 {
	ageCookie := []*http.Cookie{{Name: "age_check_done", Value: "1"}}
	return &FC2{scraper.NewDefaultScraper(
		Name, baseURL, Priority, language.Japanese,
		scraper.WithCookies(baseURL, ageCookie),
		scraper.WithCookies("https://contents.fc2.com/", ageCookie),
	)}
}

func (fc2 *FC2) NormalizeMovieID(id string) string {
	return fc2util.ParseNumber(id)
}

func (fc2 *FC2) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	number := fc2util.ParseNumber(strings.TrimSpace(id))
	if number == "" {
		return nil, provider.ErrInvalidID
	}
	return fc2.GetMovieInfoByURL(fmt.Sprintf(movieURL, number))
}

func (fc2 *FC2) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !homepage.IsAbs() || homepage.User != nil ||
		!strings.EqualFold(homepage.Scheme, "https") ||
		!isFC2ArticleHost(homepage.Hostname()) ||
		(homepage.Port() != "" && homepage.Port() != "443") {
		return "", provider.ErrInvalidURL
	}
	match := regexp.MustCompile(`^/article/(\d+)/?$`).FindStringSubmatch(homepage.Path)
	if len(match) != 2 {
		return "", provider.ErrInvalidURL
	}
	return match[1], nil
}

func isFC2ArticleHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "adult.contents.fc2.com", "contents.fc2.com":
		return true
	default:
		return false
	}
}

// fc2ThumbHost is the resizing front for FC2's artwork. A path segment in
// front of the original file selects the rendition, so /w262h262/<file URL
// without scheme> is the 262x262 list thumbnail of that exact file.
const fc2ThumbHost = "contents-thumbnail2.fc2.com"

// upgradeFC2Artwork swaps the 262x262 list thumbnail the detail page embeds for
// the artwork it was derived from. Jellyfin upscales a cover to the poster
// size, so serving the thumbnail turns every FC2 entry into a blurry one — and
// at 69k pixels it also loses the sharpness comparison the aggregated image
// walk runs against JavDB's 128k cover, which then decides the picture comes
// from another site. The suffix after the rendition segment is the original
// file's own URL, so dropping the segment is enough to get it back; anything
// that does not look like an FC2 storage host is returned untouched.
func upgradeFC2Artwork(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), fc2ThumbHost) {
		return rawURL
	}
	_, original, ok := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !ok {
		return rawURL
	}
	upgraded, err := url.Parse("https://" + original)
	if err != nil || upgraded.User != nil ||
		!strings.HasSuffix(strings.ToLower(upgraded.Hostname()), ".contents.fc2.com") {
		return rawURL
	}
	if u.RawQuery != "" {
		upgraded.RawQuery = u.RawQuery
	}
	return upgraded.String()
}

func (fc2 *FC2) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := fc2.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("FC2-%s", id),
		Provider:      fc2.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := fc2.ClonedCollector()
	var blocked bool
	c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("fc2: stopped after 10 redirects")
		}
		if isReaderURL(req.URL) {
			return nil // the reader is a plain hop, not a work URL
		}
		if isEKYCURL(req.URL) {
			blocked = true
			return errEKYCBlocked
		}
		redirectID, redirectErr := fc2.ParseMovieIDFromURL(req.URL.String())
		if redirectErr != nil || redirectID != id {
			return fmt.Errorf("fc2: refusing redirect to unexpected work URL %q", req.URL)
		}
		return nil
	})

	// Headers
	c.OnXML(`//div[@class="items_article_headerInfo"]`, func(e *colly.XMLElement) {
		// Modified title extraction
		rawTitle := e.ChildText(`./h3`) // Get all text content from h3 element

		// Process as HTML
		doc, err := goquery.NewDocumentFromReader(strings.NewReader("<h3>" + rawTitle + "</h3>"))
		if err == nil {
			// Remove spam-like spans
			doc.Find("span").Each(func(i int, s *goquery.Selection) {
				style, exists := s.Attr("style")
				if exists && (strings.Contains(style, "zoom:0.01") ||
					strings.Contains(style, "display:none") ||
					strings.Contains(style, "overflow:hidden")) {
					s.Remove()
				}
			})

			// Get clean text
			info.Title = strings.TrimSpace(doc.Text())
		} else {
			// Fallback: Remove spam patterns using regex
			pattern := regexp.MustCompile(`\*+[a-z0-9*]+\s*`)
			cleanTitle := pattern.ReplaceAllString(rawTitle, "")
			info.Title = strings.TrimSpace(cleanTitle)
		}
		info.Genres = e.ChildTexts(`.//section[@class="items_article_TagArea"]/div/a`)
		info.Maker = e.ChildText(`.//ul/li[last()]/a`)
		{ /* score */
			class := e.ChildAttr(`.//li[@class="items_article_StarA"]/a/p/span`, "class")
			info.Score = parser.ParseScore(regexp.MustCompile(`(\d+)$`).FindString(class))
		}
		{ /* release date */
			ss := strings.Split(e.ChildText(`.//div[@class="items_article_Releasedate"]/p`), ":")
			info.ReleaseDate = parser.ParseDate(ss[len(ss)-1])
		}
	})

	// Extra Info
	c.OnXML(`//div[@class="items_article_headerInfo"]/div[@class="items_article_softDevice"]/p`, func(e *colly.XMLElement) {
		key, value, found := strings.Cut(e.Text, ":")
		if !found {
			return
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Sale Day", "販売日":
			info.ReleaseDate = parser.ParseDate(value)
		case "Product ID", "商品ID":
			// Fallback only:
			if productID := fc2util.ParseNumber(value); productID != id {
				// info.ID = productID
				// info.Number = fmt.Sprintf("FC2-%s", productID)
				err = fmt.Errorf("ID mismatch: FC2-%s != FC2-%s", id, productID)
			}
		}
	})

	// Summary
	c.OnXML(`//section[@class="items_article_Contents"]/iframe`, func(e *colly.XMLElement) {
		d := c.Clone()
		d.OnXML(`//html/body/div`, func(e *colly.XMLElement) {
			info.Summary = strings.TrimSpace(e.Text)
		})
		d.Visit(e.Request.AbsoluteURL(e.Attr("src")))
	})

	// Thumb
	c.OnXML(`//div[@class="items_article_MainitemThumb"]/span/img`, func(e *colly.XMLElement) {
		info.ThumbURL = upgradeFC2Artwork(e.Request.AbsoluteURL(e.Attr("src")))
	})

	// Runtime
	c.OnXML(`//div[@class="items_article_MainitemThumb"]//p[@class="items_article_info"]`, func(e *colly.XMLElement) {
		info.Runtime = parser.ParseRuntime(e.Text)
	})

	// Preview Images
	c.OnXML(`//section[@class="items_article_SampleImages"]/ul/li`, func(e *colly.XMLElement) {
		info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(e.ChildAttr(`.//a`, "href")))
	})

	// Cover (fallbacks)
	c.OnScraped(func(_ *colly.Response) {
		if info.ThumbURL != "" {
			// ThumbURL is the full-resolution file, see upgradeFC2Artwork.
			info.CoverURL = info.ThumbURL
		} else if len(info.PreviewImages) > 0 {
			info.CoverURL = info.PreviewImages[0]
		}
	})

	// Preview Video
	//c.OnScraped(func(r *colly.Response) {
	//	d := c.Clone()
	//	d.OnResponse(func(r *colly.Response) {
	//		data := struct {
	//			Path string `json:"path"`
	//			Code int    `json:"code"`
	//		}{}
	//		if err := json.Unmarshal(r.Body, &data); err == nil && data.Code == http.StatusOK {
	//			info.PreviewVideoURL = data.Path
	//		}
	//	})
	//	d.Visit(fmt.Sprintf(sampleURL, info.ID))
	//})

	mode := activeReaderMode()
	var vErr error
	if mode != readerAlways {
		vErr = c.Visit(info.Homepage)
	}
	if mode == readerAlways || (blocked && mode == readerOnReaderBlock) {
		// The reader serves the same document, so the handlers above fill this
		// struct a second time. Drop what the blocked attempt collected or the
		// list fields end up with every entry twice.
		resetMovieInfo(info)
		vErr = fc2.visitThroughReader(c, info)
	}
	if vErr != nil {
		err = vErr
	}
	return
}

// visitThroughReader fetches the article through the reader service.
//
// The reader hands back the page with absolute artwork URLs, so nothing needs
// rewriting; only the document's own <base> is irrelevant because the handlers
// never rely on a relative path.
func (fc2 *FC2) visitThroughReader(c *colly.Collector, info *model.MovieInfo) error {
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("x-respond-with", "html")
		r.Headers.Set("x-timeout", "30")
	})
	// The reader returns the document labelled text/plain, and colly only runs
	// the XML handlers for html/xml content types — without this the page parses
	// to nothing and the provider reports incomplete metadata.
	c.OnResponse(func(r *colly.Response) {
		r.Headers.Set("Content-Type", "text/html; charset=utf-8")
	})
	return c.Visit(readerPrefix + info.Homepage)
}

// resetMovieInfo clears the scraped fields so a second pass starts clean.
func resetMovieInfo(info *model.MovieInfo) {
	info.Title = ""
	info.Summary = ""
	info.Maker = ""
	info.Score = 0
	info.Runtime = 0
	info.ReleaseDate = datatypes.Date{}
	info.ThumbURL = ""
	info.CoverURL = ""
	info.Genres = nil
	info.Actors = nil
	info.PreviewImages = nil
}

func init() {
	provider.Register(Name, New)
}
