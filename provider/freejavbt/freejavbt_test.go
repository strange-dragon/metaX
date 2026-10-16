package freejavbt

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const searchFixture = `<!doctype html><html><body>
<div class="category-page video-list-item">
  <a href="/ABC-123/"><img src="/images/poster.jpg"></a>
  <h2>ABC-123 Exact title</h2>
</div>
<div class="category-page video-list-item">
  <a href="/ABC-1234/"><img src="/images/near.jpg"></a>
  <h2>ABC-1234 Near match</h2>
</div>
<div class="category-page video-list-item">
  <a href="https://evil.invalid/ABC-123/"><img src="/images/evil.jpg"></a>
  <h2>ABC-123 External result</h2>
</div>
</body></html>`

const detailFixture = `<!doctype html><html><head>
<meta property="og:image" content="/images/poster.jpg">
</head><body>
<article>
  <h1 class="entry-title">ABC-123 Exact title</h1>
  <p><strong>出演者</strong><a>Actor One</a><a>Actor Two</a></p>
  <p><strong>メーカー</strong><a>Studio One</a></p>
  <p><strong>レーベル</strong><a>Label One</a></p>
  <p><strong>シリーズ</strong><a>Series One</a></p>
  <p><strong>配信日</strong>2024-05-16</p>
  <p><strong>収録時間</strong>120分</p>
  <p><strong>ジャンル</strong><a>Drama</a><a>Drama</a><a>Romance</a></p>
  <img class="alignnone" src="/images/poster.jpg">
  <a href="/images/sample-1.jpg">sample one</a>
  <a href="/images/sample-2.jpg">sample two</a>
</article>
</body></html>`

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/ABC-123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/ABC-123/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "ABC-123", results[0].Number)
	require.Equal(t, "Exact title", results[0].Title)
	require.Equal(t, server.URL+"/ABC-123/", results[0].Homepage)
	require.Equal(t, server.URL+"/images/poster.jpg", results[0].ThumbURL)

	info, err := p.GetMovieInfoByID("ABC-123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "ABC-123 Exact title", info.Title)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, "Label One", info.Label)
	require.Equal(t, "Series One", info.Series)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, server.URL+"/images/poster.jpg", info.CoverURL)
	require.Equal(t, []string{server.URL + "/images/sample-1.jpg", server.URL + "/images/sample-2.jpg"}, []string(info.PreviewImages))
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, 120, info.Runtime)
}

func TestSearchRejectsNearNumberAndExternalResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/ABC-123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>
<div class="category-page video-list-item"><a href="/ABC-1234/"><h2>ABC-1234 near</h2></a></div>
<div class="category-page video-list-item"><a href="https://evil.invalid/ABC-123/"><h2>ABC-123 external</h2></a></div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.Client()).SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestParseMovieIDFromURLRejectsUnsafeOriginsAndPaths(t *testing.T) {
	p := New()
	id, err := p.ParseMovieIDFromURL("https://freejavbt.com/category/ABC-123/")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", id)
	for _, rawURL := range []string{
		"http://freejavbt.com/ABC-123/",
		"https://freejavbt.com:444/ABC-123/",
		"https://user@freejavbt.com/ABC-123/",
		"https://freejavbt.com.evil.invalid/ABC-123/",
		"https://freejavbt.com/search/ABC-123/",
		"/ABC-123/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := p.ParseMovieIDFromURL(rawURL)
			require.Error(t, err)
		})
	}
}

func TestDetailRejectsRedirectToDifferentWorkAndExternalOrigin(t *testing.T) {
	var externalRequests int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer external.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/ABC-123/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ABC-999/", http.StatusFound)
	})
	mux.HandleFunc("/ABC-999/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	mux.HandleFunc("/external", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/ABC-123/", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/ABC-123/")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)

	resp, err := p.Fetch(server.URL + "/external")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, externalRequests)
}

func TestRegistrationAndConfiguration(t *testing.T) {
	found := false
	for name, factory := range mt.RangeMovieFactory {
		if name != Name {
			continue
		}
		found = true
		instance := factory()
		require.Equal(t, Name, instance.Name())
		require.Equal(t, float64(Priority), instance.Priority())
		break
	}
	require.True(t, found, "Freejavbt should register with MetaTube's Provider factory")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	p := New()
	_, err := p.GetMovieInfoByID("../ABC-123")
	require.Error(t, err)
}

func TestGetMovieInfoByIDFallsBackToDirectDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/ABC-123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body>No results</body></html>")
	})
	mux.HandleFunc("/ABC-123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByID("ABC-123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", info.Number)
}
