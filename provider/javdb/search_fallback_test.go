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
