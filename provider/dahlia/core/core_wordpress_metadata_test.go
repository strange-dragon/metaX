package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/strange-dragon/metaX/model"
)

func TestWordPressMetadataMapping(t *testing.T) {
	var detailRequested bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/top/wp-json/wp/v2/works":
			if got := r.URL.Query().Get("slug"); got != "mgold123" {
				t.Errorf("REST slug = %q, want mgold123", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{
				"slug":"mgold123",
				"title":{"rendered":"Fallback title"},
				"acf":{
					"作品名":"Sample Title 葵",
					"出演女優":"葵 / 乙",
					"作品紹介文":"A plot summary",
					"収録時間":"120分",
					"発売日（表示用）":"2024-01-02"
				},
				"acf_custom_images":{
					"main_photo_url":"https://img.example/thumb.jpg",
					"sub_photo_url":"https://img.example/poster.jpg",
					"gallery_urls":["https://img.example/1.jpg",null,""]
				}
			}]`)
		case r.URL.Path == "/top/works/mgold123/":
			detailRequested = true
			_, _ = fmt.Fprint(w, `<html><div class="bar02_works"><h1>Legacy title</h1></div></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	core := (&Core{
		BaseURL:         server.URL + "/top/",
		MovieURL:        server.URL + "/top/works/%s/",
		SearchURL:       server.URL + "/top/?s=%s",
		DefaultName:     "FALENO",
		DefaultPriority: 1,
	}).Init()
	info, err := core.GetMovieInfoByURL(server.URL + "/top/works/mgold123/")
	if err != nil {
		t.Fatalf("GetMovieInfoByURL() error = %v", err)
	}
	if !detailRequested {
		t.Fatal("expected detail page request")
	}
	if info.Title != "Sample Title" {
		t.Errorf("Title = %q, want Sample Title", info.Title)
	}
	if got := strings.Join(info.Actors, ","); got != "葵,乙" {
		t.Errorf("Actors = %q, want 葵,乙", got)
	}
	if info.Summary != "A plot summary" {
		t.Errorf("Summary = %q", info.Summary)
	}
	if info.CoverURL != "https://img.example/poster.jpg" {
		t.Errorf("CoverURL = %q", info.CoverURL)
	}
	if info.ThumbURL != "https://img.example/thumb.jpg" {
		t.Errorf("ThumbURL = %q", info.ThumbURL)
	}
	if got := strings.Join(info.PreviewImages, ","); got != "https://img.example/1.jpg" {
		t.Errorf("PreviewImages = %q", got)
	}
	if info.Label != "maryGOLD" {
		t.Errorf("Label = %q, want maryGOLD", info.Label)
	}
	if info.Runtime != 120 {
		t.Errorf("Runtime = %d, want 120", info.Runtime)
	}
}

func TestWordPressMetadataRejectsNonExactSlug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wp-json/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{"slug":"mgold1234"}]`)
			return
		}
		t.Errorf("detail page should not be requested for non-exact slug: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	core := testCore(server.URL)
	core.BaseURL = server.URL + "/"
	_, err := core.GetMovieInfoByURL(server.URL + "/works/mgold123/")
	if err == nil {
		t.Fatal("expected non-exact slug to be rejected")
	}
}
func TestFalenoPublisherRouting(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want string
	}{
		{name: "maryGOLD prefix", slug: "mgold123", want: "maryGOLD"},
		{name: "JimmyScandal prefix", slug: "jimmy456", want: "JimmyScandal"},
		{name: "default FALENOstar", slug: "fns257", want: "FALENOstar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := (&Core{DefaultName: "FALENO", DefaultPriority: 1}).Init()
			info := &model.MovieInfo{}
			core.applyWordPressWork(info, &wpWork{Slug: tt.slug})
			if info.Label != tt.want {
				t.Fatalf("Label = %q, want %q", info.Label, tt.want)
			}
		})
	}
}
