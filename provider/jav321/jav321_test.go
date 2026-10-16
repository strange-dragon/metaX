package jav321

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
<div class="panel-heading"><h3>SSIS-033  Sample Jav321 Title</h3></div>
<div class="panel-body">
  <img src="/images/ssis033.jpg">
  <div class="row"><div class="col-md-12">A short synopsis.</div></div>
</div>
<div><b>出演者</b><a>Actor One</a><a>Actor Two</a></div>
<div><b>メーカー</b><a>Studio One</a></div>
<div><b>配信開始日</b>2024-02-19</div>
<div><b>収録時間</b>120分</div>
<div><b>ジャンル</b><a>Drama</a><a>Romance</a></div>
</body></html>`

func TestNormalizeAndParseMovieID(t *testing.T) {
	p := New()
	require.Equal(t, "SSIS-033", p.NormalizeMovieID("　ssis-033 "))
	require.Equal(t, "SSIS-033", p.NormalizeMovieKeyword(" ssis-033 "))
	id, err := p.ParseMovieIDFromURL("https://www.jav321.com/video/ssis-033/")
	require.NoError(t, err)
	require.Equal(t, "SSIS-033", id)
	_, err = p.ParseMovieIDFromURL("https://example.invalid/video/SSIS-033")
	require.Error(t, err)
	_, err = p.ParseMovieIDFromURL("https://www.jav321.com/search?q=SSIS-033")
	require.Error(t, err)
}

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/video/SSIS-033", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ssis-033")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "SSIS-033", results[0].Number)
	require.Equal(t, "Sample Jav321 Title", results[0].Title)
	require.Equal(t, server.URL+"/video/SSIS-033", results[0].Homepage)
	require.Equal(t, server.URL+"/images/ssis033.jpg", results[0].CoverURL)

	info, err := p.GetMovieInfoByID("SSIS-033")
	require.NoError(t, err)
	require.Equal(t, "SSIS-033", info.ID)
	require.Equal(t, "A short synopsis.", info.Summary)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, 120, info.Runtime)
	require.Equal(t, "2024-02-19", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestSearchMovieIgnoresNonNumberKeywords(t *testing.T) {
	p := New()
	results, err := p.SearchMovie("actor name instead of number")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestNotFoundPageAndRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body>not found</body></html>")
	}))
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("ABP-123")
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
	require.True(t, found, "Jav321 should register with MetaTube's Provider factory")
}

func TestProxyAndTimeout(t *testing.T) {
	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}
