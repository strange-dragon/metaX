package fc2club

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const detailFixture = `<!doctype html><html><body>
<div class="show-top-grids">
  <h3>FC2-123456 - Sample FC2 Title</h3>
  <img src="/images/fc2.jpg">
</div>
<ul class="slides">
  <li><h5 class="title">Actor One</h5></li>
  <li><h5 class="title">Actor Two</h5></li>
</ul>
<a href="/tag/drama">Drama</a><a href="/tag/short">Short</a>
<p>發售日期：2024/05/16</p>
</body></html>`

func TestNormalizeAndParseMovieID(t *testing.T) {
	p := New()
	require.Equal(t, "FC2-123456", p.NormalizeMovieID("FC2-PPV-123456"))
	require.Equal(t, "FC2-123456", p.NormalizeMovieKeyword("123456"))
	id, err := p.ParseMovieIDFromURL("https://fc2club.top/html/FC2-123456.html")
	require.NoError(t, err)
	require.Equal(t, "FC2-123456", id)
	_, err = p.ParseMovieIDFromURL("https://example.invalid/html/FC2-123456.html")
	require.Error(t, err)
	_, err = p.ParseMovieIDFromURL("https://fc2club.top/video/123456")
	require.Error(t, err)
}

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/html/FC2-123456.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("FC2-PPV-123456")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "FC2-123456", results[0].Number)
	require.Equal(t, "Sample FC2 Title", results[0].Title)
	require.Equal(t, server.URL+"/images/fc2.jpg", results[0].CoverURL)

	info, err := p.GetMovieInfoByID("123456")
	require.NoError(t, err)
	require.Equal(t, "FC2-123456", info.ID)
	require.Equal(t, "Sample FC2 Title", info.Title)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Short"}, []string(info.Genres))
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestNoResultAndRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body>not found</body></html>")
	}))
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/html/FC2-123456.html")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)

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
	require.True(t, found, "FC2Club should register with MetaTube's Provider factory")
}

func TestProxyAndTimeout(t *testing.T) {
	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}
