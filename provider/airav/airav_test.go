package airav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const searchFixture = `<!doctype html><html><body>
<div class="oneVideo">
  <a href="/video/target"><img src="/images/poster.jpg"></a>
  <h5>ABC-123 Exact title</h5>
</div>
<div class="oneVideo">
  <a href="/video/near"><img src="/images/near.jpg"></a>
  <h5>ABC-1234 Wrong near match</h5>
</div>
<div class="oneVideo">
  <a href="https://evil.invalid/video/target"><img src="/images/evil.jpg"></a>
  <h5>ABC-123 External result</h5>
</div>
</body></html>`

const detailFixture = `<!doctype html><html><head>
<meta property="og:image" content="/images/poster.jpg">
</head><body>
<div class="video-title"><h1>ABC-123 Exact title</h1></div>
<ul><li>番号：<span>ABC-123</span></li></ul>
<div class="video-item"><i class="fa-clock"></i> 2024-05-16</div>
<a href="/actor?id=1">Actor One</a><a href="/actor?id=2">Actor Two</a>
<a href="/tag?fid=1">Studio One</a>
<a href="/tag?tid=1">Drama</a><a href="/tag?tid=2">Drama</a>
<div class="video-info"><p>Plot text</p></div>
</body></html>`

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search_result", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, _ *http.Request) {
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
	require.Equal(t, server.URL+"/video/target", results[0].Homepage)
	require.Equal(t, server.URL+"/images/poster.jpg", results[0].ThumbURL)

	info, err := p.GetMovieInfoByID("ABC-123")
	require.NoError(t, err)
	require.Equal(t, "target", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Exact title", info.Title)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama"}, []string(info.Genres))
	require.Equal(t, "Plot text", info.Summary)
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestGetMovieInfoRejectsMissingOrMismatchedIdentity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search_result", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><div class="video-title"><h1>Wrong title</h1></div></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("XYZ-9")
	require.NoError(t, err)
	require.Empty(t, results)

	info, err := p.GetMovieInfoByURL(server.URL + "/video/target")
	require.ErrorIs(t, err, ErrMovieNotFound)
	require.Nil(t, info)
}

func TestParseMovieIDFromURLRejectsUnsafeOrigins(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://airav.io/video/123",
		"https://airav.io:444/video/123",
		"https://user@airav.io/video/123",
		"https://airav.io.evil.invalid/video/123",
		"https://airav.io/not-video/123",
		"https://airav.io/video/123/extra",
		"https://airav.io/video/123?redirect=other",
		"https://airav.io/video/123#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := p.ParseMovieIDFromURL(raw)
			require.Error(t, err)
		})
	}
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalRequests int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer external.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/video/target", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/video/target")
	require.Nil(t, info)
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
	require.True(t, found, "Airav should register with MetaTube's Provider factory")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestDetailRedirectRejectsDifferentWork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/video/other", http.StatusFound)
	})
	mux.HandleFunc("/video/other", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/video/target")
	require.ErrorIs(t, err, ErrMovieNotFound)
	require.Nil(t, info)
}

func TestAbsoluteURLLeavesMissingValuesEmpty(t *testing.T) {
	base, err := url.Parse("https://airav.io")
	require.NoError(t, err)
	require.Empty(t, absoluteURL(base, ""))
}

func TestGetMovieInfoByIDAcceptsOpaqueDetailID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search_result", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>No results</body></html>`)
	})
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("target")
	require.NoError(t, err)
	require.Equal(t, "target", info.ID)
	require.Equal(t, "ABC-123", info.Number)
}

func TestDetailRedirectRejectsNoncanonicalRouteWithSameID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/video/target", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/not-video/target", http.StatusFound)
	})
	mux.HandleFunc("/not-video/target", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/video/target")
	require.ErrorIs(t, err, ErrMovieNotFound)
	require.Nil(t, info)
}

func TestSearchMovieFC2CanonicalVariantFallback(t *testing.T) {
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("/search_result", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("kw")
		queries = append(queries, query)
		if query == "FC2-PPV-12345" {
			_, _ = fmt.Fprint(w, "<html><body>No result for canonical query</body></html>")
			return
		}
		if query == "FC2-12345" {
			_, _ = fmt.Fprint(w, `<html><body><div class="oneVideo">
				<a href="/video/fc2-target"><img src="/images/fc2.jpg"></a>
				<h5>FC2-12345 Exact title</h5>
			</div></body></html>`)
			return
		}
		t.Errorf("unexpected search query %q", query)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("FC2-12345")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "FC2-12345", results[0].Number)
	require.Equal(t, []string{"FC2-PPV-12345", "FC2-12345"}, queries)
}

func TestSearchMovieFC2NumberVariantsMatchSameArticle(t *testing.T) {
	for _, query := range []string{"FC2-12345", "FC2PPV12345", "FC2_PPV_12345"} {
		t.Run(query, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/search_result", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `<html><body><div class="oneVideo">
					<a href="/video/fc2-target"><img src="/images/fc2.jpg"></a>
					<h5>FC2-PPV-12345 Exact title</h5>
				</div></body></html>`)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			p := newForTest(server.URL, server.Client())
			results, err := p.SearchMovie(query)
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, server.URL+"/video/fc2-target", results[0].Homepage)
		})
	}
}
