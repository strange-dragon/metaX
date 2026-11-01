// Package fc2ppvdb scrapes the FC2 PPV catalogue at ppvdatabank.com.
//
// The provider keeps its historical name and provider key ("FC2PPVDB") because
// route lists and stored records refer to it, but the catalogue it reads moved:
// fc2ppvdb.com now answers 526 on every request, while ppvdatabank.com serves
// the same articles at /article/<id>/ under a different markup. That catalogue
// is the only one left that still has entries FC2 has delisted from
// adult.contents.fc2.com, which is what makes it worth keeping as the FC2
// fallback — the publisher's own page is authoritative for everything still on
// sale, but a removed title has nothing to scrape there.
//
// What it can supply is narrower than the publisher's page: title, artwork,
// sample images, release date, runtime and the seller. There are no genres and
// no performers, so the aggregation layer keeps looking for those elsewhere.
//
// Its artwork is the weak part. The detail page offers a single thumbnail with
// 356px on the long edge — square for some articles, 356x236 for others — which
// is below the 180k-pixel bar the aggregated image walk uses before it stops
// asking the other providers. A title covered by both sources therefore still
// takes its poster from FC2. That is the intended order: this catalogue is a
// fallback for what the publisher no longer lists, not a replacement.
package fc2ppvdb

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/fc2/fc2util"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var _ provider.MovieProvider = (*FC2PPVDB)(nil)

const (
	Name     = "FC2PPVDB"
	Priority = 1000 - 2
)

const catalogueHost = "ppvdatabank.com"

const (
	baseURL  = "https://" + catalogueHost + "/"
	movieURL = "https://" + catalogueHost + "/article/%s/"
)

// ErrMovieNotFound answers 404 for an ID this catalogue does not carry. It
// covers most of the catalogue's own gaps — new releases take a while to be
// archived — and must not be reported as a scrape failure.
var ErrMovieNotFound = fmt.Errorf("fc2ppvdb: %w", provider.ErrMovieNotFound)

// notFoundMarkers are the phrases the catalogue answers with for an article it
// does not have. The server uses a real 404 status, but the body is checked as
// well so a rewrite that starts answering 200 still resolves to "not found".
var notFoundMarkers = []string{
	"404 File Not Found",
	"お探しのページは見つかりませんでした",
}

type FC2PPVDB struct {
	*scraper.Scraper
}

func New() *FC2PPVDB {
	return &FC2PPVDB{scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese)}
}

func (fc2ppvdb *FC2PPVDB) NormalizeMovieID(id string) string {
	return fc2util.ParseNumber(strings.TrimSpace(id))
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	id = fc2ppvdb.NormalizeMovieID(id)
	if id == "" {
		return nil, provider.ErrInvalidID
	}
	return fc2ppvdb.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

var detailPathRE = regexp.MustCompile(`^/article/([0-9]+)/?$`)

func (fc2ppvdb *FC2PPVDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !homepage.IsAbs() || homepage.Host == "" || !strings.EqualFold(homepage.Scheme, "https") {
		return "", provider.ErrInvalidURL
	}
	if homepage.User != nil {
		return "", provider.ErrInvalidURL
	}
	if host := strings.TrimSuffix(strings.ToLower(homepage.Hostname()), "."); host != catalogueHost {
		return "", provider.ErrInvalidURL
	}
	if port := homepage.Port(); port != "" && port != "443" {
		return "", provider.ErrInvalidURL
	}
	matches := detailPathRE.FindStringSubmatch(homepage.Path)
	if len(matches) != 2 {
		return "", provider.ErrInvalidURL
	}
	return matches[1], nil
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := fc2ppvdb.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("FC2-%s", id),
		Provider:      fc2ppvdb.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := fc2ppvdb.ClonedCollector()
	var gone bool
	// A missing article answers 404. colly reports a non-2xx status as a bare
	// "Not Found" error with no status attached, so the code has to be read
	// here rather than from the returned error.
	c.OnError(func(r *colly.Response, _ error) {
		if r != nil && r.StatusCode == http.StatusNotFound {
			gone = true
		}
	})
	c.OnResponse(func(r *colly.Response) {
		if r.StatusCode == http.StatusNotFound {
			gone = true
			return
		}
		body := string(r.Body)
		for _, marker := range notFoundMarkers {
			if strings.Contains(body, marker) {
				gone = true
				return
			}
		}
	})

	// Cover image. The catalogue renders the same file at three sizes; the
	// detail page embeds the 356x356 one, which is the largest square it has.
	c.OnXML(`//div[@class="thumb"]/img`, func(e *colly.XMLElement) {
		info.CoverURL = e.Request.AbsoluteURL(e.Attr("src"))
		if info.ThumbURL == "" {
			info.ThumbURL = info.CoverURL
		}
	})

	// Title. The catalogue repeats it in a meta tag and in the page title, so
	// both are kept as fallbacks for a markup change in the heading block.
	c.OnXML(`//div[@class="article_title"]//a`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})
	c.OnXML(`//meta[@name="title"]`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Attr("content"))
		}
	})

	// Metadata rows: "発売日 : 2026/3/11", "再生時間 : 01:32:30", "販売者 : <a>".
	// The label follows the page language, so both spellings are accepted and
	// the value is parsed by shape rather than by position.
	c.OnXML(`//ul[@class="meta"]/li`, func(e *colly.XMLElement) {
		key, value, found := strings.Cut(e.Text, ":")
		if !found {
			return
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "発売日", "販売日", "Sale Day", "Release Date":
			info.ReleaseDate = parser.ParseDate(value)
		case "再生時間", "収録時間", "Runtime":
			info.Runtime = parser.ParseRuntime(value)
		case "販売者", "Seller":
			info.Maker = value
		}
	})

	// Sample images: the anchor points at the full-size file, the thumbnail
	// sits inside it.
	c.OnXML(`//ul[@class="sample_image_area"]//a`, func(e *colly.XMLElement) {
		if href := e.Attr("href"); href != "" {
			info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(href))
		}
	})

	if vErr := c.Visit(info.Homepage); vErr != nil {
		if gone {
			return nil, ErrMovieNotFound
		}
		return nil, vErr
	}
	if gone || strings.TrimSpace(info.Title) == "" {
		return nil, ErrMovieNotFound
	}
	return
}

func init() {
	provider.Register(Name, New)
}
