package scraper

import (
	"bytes"
	"strings"

	"github.com/gocolly/colly/v2"
)

// markupProbes are the byte prefixes that identify a markup document. They are
// matched case-insensitively against the first bytes of the body, after a UTF-8
// BOM and leading whitespace are removed.
var markupProbes = [][]byte{
	[]byte("<!doctype"),
	[]byte("<html"),
	[]byte("<head"),
	[]byte("<?xml"),
	[]byte("<!--"),
}

// sniffMarkupContentType relabels a response that claims to be plain text while
// its body is really a markup document.
//
// The label is not always the site's fault. A reader proxy, a caching CDN or a
// misconfigured origin can serve the same HTML as text/plain, and colly decides
// whether to run OnHTML/OnXML from the Content-Type alone: anything that is not
// html/xml is skipped in silence, with no error and no log line, so every
// selector matches nothing and the caller sees an empty record. FC2 hit exactly
// this through r.jina.ai and had to force the header per provider; the rewrite
// belongs here so no provider rediscovers it.
func sniffMarkupContentType(r *colly.Response) {
	if r == nil || len(r.Body) == 0 || r.Headers == nil {
		return
	}
	contentType := strings.ToLower(r.Headers.Get("Content-Type"))
	if strings.Contains(contentType, "html") || strings.Contains(contentType, "xml") {
		return
	}
	if !looksLikeMarkup(r.Body) {
		return
	}
	r.Headers.Set("Content-Type", "text/html; charset=utf-8")
}

// looksLikeMarkup reports whether body opens with a markup prologue.
func looksLikeMarkup(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimSpace(head)
	head = bytes.ToLower(head)
	for _, probe := range markupProbes {
		if bytes.HasPrefix(head, probe) {
			return true
		}
	}
	return false
}
