package iqqtv

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

const searchFixture = `<!doctype html><html><body>
<span class="title"><a href="/cn/detail/near" title="Near title ABC-1234"><img src="/images/near.jpg"></a></span>
<span class="title"><a href="/detail/abc123" title="Exact title ABC-123"><img src="/images/poster.jpg"></a></span>
<span class="title"><a href="/detail/broken" title="ABC-123 无码破解"></a></span>
<span class="title"><a href="https://evil.invalid/detail/abc123" title="Exact title ABC-123"></a></span>
</body></html>`

const detailFixture = `<!doctype html><html><head>
<meta property="og:image" content="/images/poster.jpg">
<meta itemprop="duration" content="01:59:40">
</head><body>
<h1 class="h4 b">Exact title ABC-123</h1>
<a href="/actor/1"><span>Actor One</span></a><a href="/actor/2"><span>Actor Two</span></a>
<a href="/fac/1"><div itemprop="name">Studio One</div></a>
<div class="date">2024/05/16</div>
<a href="/series/1">Series One</a>
<div class="tag-info"><a href="/tag/1">Drama</a><a href="/tag/2">Drama</a><a href="/tag/3">Romance</a></div>
<p>简介：Plot text*根据分发信息后缀</p>
<div class="cover"><img data-src="/images/sample-1.jpg"><img data-src="/images/sample-2.jpg"></div>
</body></html>`

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/search.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/cn/detail/abc123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	mux.HandleFunc("/detail/abc123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	mux.HandleFunc("/cn/detail/near", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<h1 class="h4 b">Near title ABC-1234</h1><div class="date">2024/05/16</div>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "ABC-123", results[0].Number)
	require.Equal(t, "Exact title", results[0].Title)
	require.Equal(t, server.URL+"/cn/detail/abc123", results[0].Homepage)
	require.Equal(t, server.URL+"/images/poster.jpg", results[0].ThumbURL)

	info, err := p.GetMovieInfoByURL(results[0].Homepage)
	require.NoError(t, err)
	require.Equal(t, "cn/detail/abc123", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Exact title", info.Title)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, "Studio One", info.Label)
	require.Equal(t, "Series One", info.Series)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, "Plot text", info.Summary)
	require.Equal(t, server.URL+"/images/poster.jpg", info.CoverURL)
	require.Equal(t, []string{server.URL + "/images/sample-1.jpg", server.URL + "/images/sample-2.jpg"}, []string(info.PreviewImages))
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, 120, info.Runtime)

	byID, err := p.GetMovieInfoByID("cn/detail/abc123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", byID.Number)
}

func TestSearchRejectsNearMatchesExcludedTitlesAndForeignHosts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/search.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>
<span class="title"><a href="/detail/near" title="Near title ABC-1234"></a></span>
<span class="title"><a href="/detail/broken" title="ABC-123 无码破解"></a></span>
<span class="title"><a href="https://evil.invalid/detail/abc123" title="Exact title ABC-123"></a></span>
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
	for _, rawURL := range []string{
		"http://iqq5.xyz/cn/detail/abc123",
		"https://iqq5.xyz:444/cn/detail/abc123",
		"https://user@iqq5.xyz/cn/detail/abc123",
		"https://iqq5.xyz.evil.invalid/cn/detail/abc123",
		"https://iqq5.xyz/cn/search.php?kw=ABC-123",
		"https://iqq5.xyz/cn/../detail/abc123",
		"/cn/detail/abc123",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := p.ParseMovieIDFromURL(rawURL)
			require.Error(t, err)
		})
	}
	id, err := p.ParseMovieIDFromURL("https://iqq5.xyz/cn/detail/abc123")
	require.NoError(t, err)
	require.Equal(t, "cn/detail/abc123", id)
}

func TestDetailRejectsRedirectToDifferentWork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/detail/abc123", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cn/detail/other", http.StatusFound)
	})
	mux.HandleFunc("/cn/detail/other", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByURL(server.URL + "/cn/detail/abc123")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)
}

func TestParseMovieIDFromURLAllowsOnlyDocumentedRotatingHosts(t *testing.T) {
	p := New()

	for _, rawURL := range []string{
		"https://iqqk4.quest/cn/detail/abc123",
		"https://iqqq3.quest/cn/detail/abc123",
	} {
		id, err := p.ParseMovieIDFromURL(rawURL)
		require.NoError(t, err, rawURL)
		require.Equal(t, "cn/detail/abc123", id)
	}

	for _, rawURL := range []string{
		"https://unrelated.quest/cn/detail/abc123",
		"https://iqqq3.quest.evil.invalid/cn/detail/abc123",
		"https://iqqq3.quest:444/cn/detail/abc123",
		"http://iqqq3.quest/cn/detail/abc123",
		"https://user@iqqq3.quest/cn/detail/abc123",
	} {
		_, err := p.ParseMovieIDFromURL(rawURL)
		require.Error(t, err, rawURL)
	}
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalRequests int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer external.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/cn/detail/abc123", http.StatusFound)
	}))
	defer source.Close()

	p := newForTest(source.URL, source.Client())
	resp, err := p.Fetch(source.URL + "/cn/detail/abc123")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, ErrUnsafeRedirect)
	require.Zero(t, externalRequests)
}

func TestFetchRejectsUnexpectedInitialOrigin(t *testing.T) {
	var requests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer target.Close()

	p := New()
	resp, err := p.Fetch(target.URL + "/cn/detail/abc123")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing request to unexpected origin")
	require.Zero(t, requests, "unexpected origin must be rejected before network access")
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
	require.True(t, found, "IQQTV should register with metaX's Provider factory")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestNormalizeAndGetMovieInfoByIDRejectInvalidPaths(t *testing.T) {
	p := New()
	require.Equal(t, "ABC-123", p.NormalizeMovieKeyword(" ＡＢＣ-123 "))
	_, err := p.GetMovieInfoByID("../detail/abc123")
	require.Error(t, err)
	require.True(t, strings.EqualFold(p.NormalizeMovieID("cn/detail/abc123"), "cn/detail/abc123"))
}
