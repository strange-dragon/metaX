package getchu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/strange-dragon/metaX/provider"
)

// Synthetic HTML fixtures exercise the selectors inherited from the pinned
// digital-download detail provider. They are not captured Getchu pages.
func detailPage(title string) string {
	return fmt.Sprintf(`<!doctype html><html><head>
<meta property="og:title" content="%s">
<meta property="og:image" content="/img/og.jpg">
</head><body>
<table>
<tr><td><div style="color: #333333; padding: 3px 0px 0px 5px;">%s</div></td></tr>
<tr><td bgcolor="#ffffff"><img src="/img/cover.jpg"></td></tr>
<tr><td>サークル</td><td>Test Circle</td></tr>
<tr><td>配信開始日</td><td>2024/05/16</td></tr>
<tr><td>趣向</td><td><a href="/genre/1">Drama</a> <a href="/genre/2">Romance</a></td></tr>
<tr><td>作品内容</td><td>Synthetic summary</td></tr>
</table>
</body></html>`, title, title)
}

func searchPage(links ...string) string {
	var body strings.Builder
	body.WriteString("<!doctype html><html><body>")
	for _, link := range links {
		body.WriteString(`<a href="`)
		body.WriteString(link)
		body.WriteString(`">result</a>`)
	}
	body.WriteString("</body></html>")
	return body.String()
}

func TestSearchMovieRequiresExactIdentity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/php/search.phtml", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search_keyword") != "ABC-123" {
			http.Error(w, "unexpected keyword", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("gc") != "gc" {
			http.Error(w, "missing gc", http.StatusBadRequest)
			return
		}
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "getchu_adalt_flag=getchu.com") || !strings.Contains(cookie, "gc=gc") {
			http.Error(w, "missing age cookies", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, searchPage(
			"/soft.phtml?id=9999999",
			"/soft.phtml?id=4042392",
			"https://evil.invalid/soft.phtml?id=1",
		))
	})
	mux.HandleFunc("/i/item9999999", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("ABC-124 Near Match"))
	})
	mux.HandleFunc("/i/item4042392", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("ABC-123 Exact Match"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.URL).SearchMovie("ABC-123")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchMovie() returned %d results, want 1: %#v", len(results), results)
	}
	result := results[0]
	if result.ID != "4042392" || result.Number != "GETCHU-4042392" {
		t.Fatalf("unexpected identity: %#v", result)
	}
	if result.Title != "ABC-123 Exact Match" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if result.Homepage != server.URL+"/i/item4042392" {
		t.Fatalf("unexpected homepage: %q", result.Homepage)
	}
	if result.CoverURL != server.URL+"/img/cover.jpg" {
		t.Fatalf("unexpected cover: %q", result.CoverURL)
	}
	if time.Time(result.ReleaseDate).Format("2006-01-02") != "2024-05-16" {
		t.Fatalf("unexpected release date: %v", time.Time(result.ReleaseDate))
	}
}

func TestSearchMovieRejectsFuzzyOnlyResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/php/search.phtml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchPage("/soft.phtml?id=4042392"))
	})
	mux.HandleFunc("/i/item4042392", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("ABC-124 Near Match"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.URL).SearchMovie("ABC-123")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fuzzy result was returned: %#v", results)
	}
}

func TestSearchMovieNumericIDUsesExactCatalogID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/php/search.phtml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchPage(
			"/soft.phtml?id=9999999",
			"/soft.phtml?id=4042392",
		))
	})
	mux.HandleFunc("/i/item9999999", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("Other work"))
	})
	mux.HandleFunc("/i/item4042392", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("Numeric work"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.URL).SearchMovie("GETCHU-4042392")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "4042392" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestSearchMovieFallsBackToExactNumericDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/php/search.phtml", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	})
	mux.HandleFunc("/i/item4042392", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("Numeric work"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.URL).SearchMovie("GETCHU-4042392")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "4042392" {
		t.Fatalf("unexpected fallback results: %#v", results)
	}
}

func TestSearchMoviePropagatesFailureForTextQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := newForTest(server.URL, server.URL).SearchMovie("Some Title"); err == nil {
		t.Fatal("expected search failure to be returned for a text query")
	}
}

type testConfig map[string]string

func (c testConfig) Has(key string) bool { _, ok := c[key]; return ok }
func (c testConfig) GetString(key string) (string, error) {
	value, ok := c[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}
func (c testConfig) GetBool(string) (bool, error)              { return false, nil }
func (c testConfig) GetInt64(string) (int64, error)            { return 0, nil }
func (c testConfig) GetFloat64(string) (float64, error)        { return 0, nil }
func (c testConfig) GetDuration(string) (time.Duration, error) { return 0, nil }

func TestSetConfigAppliesCookiesToCatalogSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/php/search.phtml", func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "custom=value") {
			http.Error(w, "missing custom cookie", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, searchPage("/soft.phtml?id=4042392"))
	})
	mux.HandleFunc("/i/item4042392", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailPage("ABC-123 Exact Match"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	gcu := newForTest(server.URL, server.URL)
	if err := gcu.SetConfig(testConfig{"cookies": "custom=value"}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	results, err := gcu.SearchMovie("ABC-123")
	if err != nil || len(results) != 1 {
		t.Fatalf("SearchMovie() = %#v, %v", results, err)
	}
}

func TestSoftResultIDRejectsForeignAndMalformedLinks(t *testing.T) {
	gcu := New()
	for _, raw := range []string{
		"https://evil.invalid/soft.phtml?id=1",
		"https://www.getchu.com/php/search.phtml?id=1",
		"/soft.phtml?id=abc",
		"/soft.phtml",
	} {
		if _, ok := gcu.softResultID(raw); ok {
			t.Errorf("softResultID(%q) unexpectedly accepted", raw)
		}
	}
	if id, ok := gcu.softResultID("/soft.phtml?id=4042392"); !ok || id != "4042392" {
		t.Fatalf("valid soft link rejected: id=%q ok=%v", id, ok)
	}
}

var _ provider.Config = testConfig{}

func TestSearchMovieUnencodableTitleIsEmpty(t *testing.T) {
	results, err := New().SearchMovie("黑暗圣经")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("SearchMovie() returned %d results, want 0", len(results))
	}
}

func TestFetchUsesCookiesAndItemReferer(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != server.URL+"/item/1081116/" {
			http.Error(w, "unexpected referer: "+r.Header.Get("Referer"), http.StatusForbidden)
			return
		}
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "getchu_adalt_flag=getchu.com") || !strings.Contains(cookie, "gc=gc") {
			http.Error(w, "missing age cookies", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	defer server.Close()

	gcu := newForTest(server.URL, server.URL)
	resp, err := gcu.Fetch(server.URL + "/brandnew/1081116/c1081116package.jpg")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	defer resp.Body.Close()

	if _, err := gcu.Fetch("https://evil.invalid/image.jpg"); err == nil {
		t.Fatal("Fetch() accepted an unexpected origin")
	}
}
