package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testCore(serverURL string) *Core {
	return (&Core{
		BaseURL:         serverURL + "/",
		MovieURL:        serverURL + "/works/%s/",
		SearchURL:       serverURL + "/?s=%s",
		DefaultName:     "test",
		DefaultPriority: 1,
	}).Init()
}

func TestGetMovieInfoByURLVerifiesExactSlug(t *testing.T) {
	tests := []struct {
		name     string
		restBody string
		redirect bool
		wantErr  bool
	}{
		{name: "exact slug", restBody: `[{"slug":"target"}]`},
		{name: "missing slug", restBody: `[]`, wantErr: true},
		{name: "mismatched slug", restBody: `[{"slug":"other"}]`, wantErr: true},
		{name: "redirect to another work", restBody: `[{"slug":"target"}]`, redirect: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var detailRequested bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/works"):
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, tt.restBody)
				case r.URL.Path == "/works/target/":
					detailRequested = true
					if tt.redirect {
						http.Redirect(w, r, "/works/other/", http.StatusFound)
						return
					}
					_, _ = fmt.Fprint(w, `<html><div class="bar02_works"><h1>Target title</h1></div></html>`)
				case r.URL.Path == "/works/other/":
					_, _ = fmt.Fprint(w, `<html><div class="bar02_works"><h1>Wrong title</h1></div></html>`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			info, err := testCore(server.URL).GetMovieInfoByURL(server.URL + "/works/target/")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetMovieInfoByURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if info != nil {
					t.Fatalf("expected nil info on identity failure, got %#v", info)
				}
				return
			}
			if !detailRequested {
				t.Fatal("detail page was not requested after exact slug match")
			}
			if info == nil || info.Title != "Target title" {
				t.Fatalf("unexpected movie info: %#v", info)
			}
		})
	}
}
