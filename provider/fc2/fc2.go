package fc2

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

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

type FC2 struct {
	*scraper.Scraper
}

func New() *FC2 {
	return &FC2{scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese)}
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
		!strings.EqualFold(homepage.Hostname(), "adult.contents.fc2.com") ||
		(homepage.Port() != "" && homepage.Port() != "443") {
		return "", provider.ErrInvalidURL
	}
	match := regexp.MustCompile(`^/article/(\d+)/?$`).FindStringSubmatch(homepage.Path)
	if len(match) != 2 {
		return "", provider.ErrInvalidURL
	}
	return match[1], nil
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
	c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("fc2: stopped after 10 redirects")
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
		info.ThumbURL = e.Request.AbsoluteURL(e.Attr("src"))
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
			info.CoverURL = info.ThumbURL
		} else if len(info.PreviewImages) > 0 {
			// Use the first preview image as cover due to
			// thumb image's poor resolution.
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

	if vErr := c.Visit(info.Homepage); vErr != nil {
		err = vErr
	}
	return
}

func init() {
	provider.Register(Name, New)
}
