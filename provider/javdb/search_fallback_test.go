package javdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetMovieInfoByIDFallsBackToExactSearchCardWhenDetailNumberMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			_, _ = fmt.Fprint(w, `<!doctype html><html><body>
<a class="box" href="/v/Wwm5kp">
  <div class="video-title"><strong>101926_01</strong> 秘蔵マンコセレクション</div>
  <img src="https://images.example/101926.jpg">
</a>
</body></html>`)
			return
		}
		if r.URL.Path == "/v/Wwm5kp" {
			// Simulate a detail response where the number marker is missing.
			_, _ = fmt.Fprint(w, `<!doctype html><html><body><h2 class="title is-4"><strong class="current-title">Detail title</strong></h2></body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("101926_01")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "101926_01", info.Number)
	require.Equal(t, "秘蔵マンコセレクション", info.Title)
	require.Equal(t, "JavDB", info.Provider)
	require.Equal(t, server.URL+"/v/Wwm5kp", info.Homepage)
	require.Equal(t, "https://images.example/101926.jpg", info.CoverURL)
}

func TestGetMovieInfoByIDTreatsFC2PPVAsSearchNumber(t *testing.T) {
	directSlugHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = fmt.Fprint(w, `<!doctype html><html><body>
<a class="box" href="/v/QVP7P8">
  <div class="video-title"><strong>FC2-3954834</strong> Sample FC2 Film</div>
  <img src="https://images.example/fc2.jpg">
</a>
</body></html>`)
		case "/v/fc2-ppv3954834":
			directSlugHits++
			http.NotFound(w, r)
		case "/v/QVP7P8":
			// The exact search card remains usable if the live detail page omits
			// the clipboard-number marker.
			_, _ = fmt.Fprint(w, `<!doctype html><html><body><h2 class="title is-4"><strong class="current-title">Sample FC2 Film</strong></h2></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("fc2-ppv3954834")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "FC2-3954834", info.Number)
	require.Equal(t, "Sample FC2 Film", info.Title)
	require.Equal(t, server.URL+"/v/QVP7P8", info.Homepage)
	require.Equal(t, "https://images.example/fc2.jpg", info.CoverURL)
	require.Zero(t, directSlugHits, "FC2 work numbers must use search rather than an invented detail slug")
}
