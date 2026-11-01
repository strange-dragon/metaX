package scraper

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gocolly/colly/v2"

	"github.com/strange-dragon/metaX/provider"
)

// goneMarkers are the phrases a catalogue site puts on a "no such product" page
// that still answers 200. The list is deliberately narrow: a generic "error" or
// "404" match would turn a transient upstream failure into a permanent 404,
// which a client library then remembers as "this title does not exist".
var goneMarkers = []string{
	// Japanese catalogues (DMM, MGS, SOD, Heyzo, ...).
	"お探しの商品が見つかりません",
	"お探しのページが見つかりません",
	"ページが見つかりません",
	"指定されたページは存在しません",
	"作品が見つかりません",
	"この作品は削除されました",
	"このページは存在しません",
	"コンテンツがありません",
	// English. A bare "not found" is left out on purpose: it appears in inline
	// scripts and CSS class names of otherwise healthy pages.
	"page not found",
	"movie not found",
	"video not found",
	// Simplified / traditional Chinese.
	"頁面不存在",
	"頁面未找到",
	"页面不存在",
	"页面未找到",
	"找不到该页面",
	"找不到此頁面",
}

// LooksGone reports whether a response body is a "no such work" page.
func LooksGone(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	text := strings.ToLower(string(body))
	for _, marker := range goneMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// NotFound records whether a detail scrape proved that the source has no such
// record, so the provider can answer 404 instead of 500.
//
// Both failure shapes used to surface as a 500. colly reports a 404 as a bare
// "Not Found" string that the HTTP layer cannot classify, and a site that
// answers 200 with an empty page produces a structurally valid but empty
// MovieInfo that the engine turns into ErrIncompleteMetadata. A 500 tells the
// client "the server is broken", so Jellyfin retries the same file on every
// library scan, forever. A 404 says the truth: the source does not carry it.
type NotFound struct {
	status atomic.Bool
	gone   atomic.Bool
}

// TrackNotFound installs the observer on a collector clone.
func TrackNotFound(c *colly.Collector) *NotFound {
	n := &NotFound{}
	c.OnResponse(func(r *colly.Response) {
		if LooksGone(r.Body) {
			n.gone.Store(true)
		}
	})
	c.OnError(func(r *colly.Response, _ error) {
		if r != nil && r.StatusCode == http.StatusNotFound {
			n.status.Store(true)
		}
	})
	return n
}

// Hit reports whether the scrape proved there is nothing to return.
func (n *NotFound) Hit() bool {
	return n != nil && (n.status.Load() || n.gone.Load())
}

// Mark records that the caller's own parse established there is no record — for
// example an API that answers 200 with an empty object. It is the one case the
// marker scan cannot detect, because there is no page text to read.
func (n *NotFound) Mark() {
	if n != nil {
		n.gone.Store(true)
	}
}

// Resolve turns the outcome of a detail scrape into the error the provider
// should return. A nil error means the visit succeeded and the provider's own
// return value stands.
//
// Only conclusive evidence counts: a 404 status, or a page that says the record
// does not exist. A visit that succeeded but parsed nothing is deliberately NOT
// turned into a 404 — that is what a site rewrite looks like, and reporting it as
// "does not exist" would quietly turn a broken parser into a catalogue that
// lost every title. The engine keeps it as a 500 for the same reason.
func (n *NotFound) Resolve(name string, visitErr error) error {
	if n.Hit() {
		return fmt.Errorf("%s: %w", name, provider.ErrMovieNotFound)
	}
	return visitErr
}
