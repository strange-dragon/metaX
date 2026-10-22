package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWordPressSearchUsesExactRESTSlug(t *testing.T) {
	var detailRequested bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/top/wp-json/wp/v2/works" {
			detailRequested = true
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("slug"); got != "fns257" {
			t.Errorf("REST slug = %q, want fns257", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{
			"slug":"fns257",
			"title":{"rendered":"Fallback title"},
			"acf":{"作品名":"","出演女優":"葵 / 乙","発売日（表示用）":"2024-01-02"},
			"acf_custom_images":{
				"main_photo_url":"https://img.example/thumb.jpg",
				"sub_photo_url":"https://img.example/poster.jpg",
				"gallery_urls":[]
			}
		}]`)
	}))
	defer server.Close()

	core := (&Core{
		BaseURL:         server.URL + "/top/",
		MovieURL:        server.URL + "/top/works/%s/",
		SearchURL:       server.URL + "/top/?s=%s",
		DefaultName:     "FALENO",
		DefaultPriority: 1,
	}).Init()
	work, err := core.fetchWordPressWork("fns257")
	if err != nil {
		t.Fatalf("fetchWordPressWork() error = %v", err)
	}
	if work.ACF.Release != "2024-01-02" {
		t.Fatalf("REST release field = %q, want 2024-01-02", work.ACF.Release)
	}
	results, err := core.SearchMovie("FNS-257")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if detailRequested {
		t.Fatal("search should use the REST record without requesting the HTML detail page")
	}
	if len(results) != 1 {
		t.Fatalf("SearchMovie() returned %d results, want 1", len(results))
	}
	result := results[0]
	if result.ID != "fns257" || result.Number != "FNS-257" || result.Title != "Fallback title" {
		t.Errorf("unexpected identity/title: %#v", result)
	}
	if result.Provider != "FALENO" || result.Homepage != server.URL+"/top/works/fns257/" {
		t.Errorf("provider/homepage mismatch: %#v", result)
	}
	if result.CoverURL != "https://img.example/poster.jpg" || result.ThumbURL != "https://img.example/thumb.jpg" {
		t.Errorf("image mapping mismatch: %#v", result)
	}
	if got := time.Time(result.ReleaseDate).Format("2006-01-02"); got != "2024-01-02" {
		t.Errorf("ReleaseDate = %q, want 2024-01-02", got)
	}
}

func TestWordPressSearchRejectsNonExactSlug(t *testing.T) {
	var detailRequested bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wp-json/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{"slug":"fns257sp"}]`)
			return
		}
		detailRequested = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	core := testCore(server.URL)
	results, err := core.SearchMovie("FNS-257")
	if err != nil {
		t.Fatalf("SearchMovie() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("SearchMovie() returned a fuzzy match: %#v", results)
	}
	if detailRequested {
		t.Fatal("HTML detail page should not be requested for a non-exact slug")
	}
}

func TestWordPressSearchPropagatesMalformedRESTResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"slug":"fns257"}`)
	}))
	defer server.Close()

	core := testCore(server.URL)
	results, err := core.SearchMovie("FNS-257")
	if err == nil {
		t.Fatal("expected malformed REST response error")
	}
	if results != nil {
		t.Fatalf("results = %#v, want nil on REST parse error", results)
	}
}
