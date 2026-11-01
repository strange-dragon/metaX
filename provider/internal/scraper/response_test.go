package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"
)

func TestSniffMarkupContentTypeRelabelsPlainTextHTML(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        string
	}{
		{"doctype labelled plain", "text/plain", "<!DOCTYPE html><html><body>x</body></html>", "text/html; charset=utf-8"},
		{"html labelled plain", "text/plain; charset=utf-8", "<html><body>x</body></html>", "text/html; charset=utf-8"},
		{"bom and whitespace", "text/plain", "\xef\xbb\xbf\n  <html><body>x</body></html>", "text/html; charset=utf-8"},
		{"upper case prologue", "application/octet-stream", "<HTML><BODY>x</BODY></HTML>", "text/html; charset=utf-8"},
		{"already html", "text/html; charset=Shift_JIS", "<html><body>x</body></html>", "text/html; charset=Shift_JIS"},
		{"real plain text", "text/plain", "not found, please try again", "text/plain"},
		{"json", "application/json", `{"title":"x"}`, "application/json"},
		{"empty body", "text/plain", "", "text/plain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.contentType != "" {
				headers.Set("Content-Type", tc.contentType)
			}
			response := &colly.Response{Headers: &headers, Body: []byte(tc.body)}
			sniffMarkupContentType(response)
			if got := response.Headers.Get("Content-Type"); got != tc.want {
				t.Errorf("Content-Type = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDefaultScraperParsesMarkupLabelledAsPlainText is the regression guard for
// the silent-empty-record failure: colly only runs OnXML when the response
// Content-Type contains html or xml, so a body labelled text/plain used to leave
// every selector unmatched with no error at all.
func TestDefaultScraperParsesMarkupLabelledAsPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><div class="title">hello</div></body></html>`)
	}))
	defer server.Close()

	s := NewDefaultScraper("test", server.URL, 0.5, language.English)

	var title string
	collector := s.ClonedCollector()
	collector.OnXML(`//div[@class="title"]`, func(e *colly.XMLElement) {
		title = e.Text
	})
	if err := collector.Visit(server.URL); err != nil {
		t.Fatalf("visit: %v", err)
	}
	if title != "hello" {
		t.Errorf("title = %q, want hello (OnXML did not run)", title)
	}
}
