package scraper

import (
	"path"
	"strings"
)

// LastPathSegment returns the URL segment that identifies the record, ignoring
// a trailing slash and the site's index file name.
//
// path.Base(path.Dir(p)) — the idiom four providers wrote by hand — returns the
// section name instead of the record ID whenever the URL has no index file and
// no trailing slash: ".../moviepages/0123" has a directory of ".../moviepages",
// so the record was stored under "moviepages". Trimming first and taking the
// last segment is the same answer for the canonical URL and the correct one for
// the short form.
func LastPathSegment(rawPath, indexFile string) string {
	trimmed := strings.TrimSpace(rawPath)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if indexFile != "" {
		trimmed = strings.TrimSuffix(trimmed, "/"+strings.TrimPrefix(indexFile, "/"))
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || trimmed == "." {
		return ""
	}
	segment := path.Base(trimmed)
	if segment == "/" || segment == "." || segment == ".." {
		return ""
	}
	return segment
}
