package javdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const detailFixture = `<!doctype html>
<html><body>
  <h2 class="title is-4"><strong class="current-title">A Sample Film Title</strong></h2>
  <a class="button is-white copy-to-clipboard" data-clipboard-text="390JAC-079">copy</a>
  <div class="panel-block">
    <strong>片商:</strong><span class="value"><a>Studio Alpha</a></span>
  </div>
  <div class="panel-block">
    <strong>發行:</strong><span class="value"><a>Label Beta</a></span>
  </div>
  <div class="panel-block">
    <strong>日期:</strong><span class="value">2024-03-14</span>
  </div>
  <div class="panel-block">
    <strong>時長:</strong><span class="value">1小時 42分鐘</span>
  </div>
  <div class="panel-block">
    <strong>系列:</strong><span class="value"><a>Series Gamma</a></span>
  </div>
  <div class="panel-block">
    <strong>導演:</strong><span class="value"><a>Director One</a><a>Director Two</a></span>
  </div>
  <div class="panel-block">
    <strong>類別:</strong><span class="value"><a>Drama</a><a>Romance</a></span>
  </div>
  <div class="panel-block">
    <strong>演員:</strong><span class="value"><a class="actor-female">Actor One</a><a>Actor Two</a></span>
  </div>
  <img class="video-cover" data-src="/covers/sample.jpg">
  <span class="score-stars"></span><span>8.42</span>
  <div class="tile-images preview-images">
    <a class="tile-item" href="/samples/1.jpg">preview 1</a>
    <a class="tile-item" href="https://cdn.example.test/samples/2.jpg">preview 2</a>
  </div>
  <video id="preview-video"><source src="/trailers/sample.mp4"></video>
</body></html>`

func TestNormalizeAndParseMovieID(t *testing.T) {
	p := New()
	require.Equal(t, "SSIS-033", p.NormalizeMovieKeyword("　ssis-033 "))
	id, err := p.ParseMovieIDFromURL("https://javdb.com/v/AbCdE123?locale=zh")
	require.NoError(t, err)
	require.Equal(t, "AbCdE123", id)
	require.Error(t, func() error {
		_, err := p.ParseMovieIDFromURL("https://javdb.com/search?q=SSIS-033")
		return err
	}())
}

func TestNumberMatchAndVariants(t *testing.T) {
	require.Equal(t, 2, numberMatch("SSIS-033", "ssis033"))
	require.Equal(t, 1, numberMatch("JAC-079", "390JAC-079"))
	require.Equal(t, 0, numberMatch("ABF-35", "ABF-375"))
	require.Equal(t, 0, numberMatch("200MIUM-1000", "300MIUM-1000"))
	require.Equal(t, 2, numberMatch("Studio.2023.05.14", "Studio.23.05.14"))
	require.Equal(t, -1, selectNumberIndexFromNumbers([]string{"JAC-079", "JAC-079"}, "390JAC-079"))
	require.Equal(t, 1, selectNumberIndexFromNumbers([]string{"ABP-123", "JAC-079"}, "390JAC-079"))

	terms := searchTerms("390JAC-079")
	require.Equal(t, []string{"390JAC-079", "JAC-079"}, terms)
}

func TestParseRuntimeMinutes(t *testing.T) {
	require.Equal(t, 102, parseRuntimeMinutes("1小時 42分鐘"))
	require.Equal(t, 120, parseRuntimeMinutes("120 分"))
	require.Equal(t, 0, parseRuntimeMinutes(""))
}

func TestSearchMovieAndFetchDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "zh", r.URL.Query().Get("locale"))
		_, _ = fmt.Fprint(w, `<html><body>
<a class="box" href="/v/wrongid"><div class="video-title"><strong>ABP-123</strong> Another Film</div><img src="/covers/other.jpg"></a>
<a class="box" href="/v/sampleid"><div class="video-title"><strong>JAC-079</strong> Sample Film Title</div><img data-src="/covers/sample.jpg"></a>
</body></html>`)
	})
	mux.HandleFunc("/v/sampleid", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("390JAC-079")
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "JAC-079", results[1].Number)
	require.Equal(t, "Sample Film Title", results[1].Title)
	require.Equal(t, server.URL+"/covers/sample.jpg", results[1].ThumbURL)

	info, err := p.GetMovieInfoByID("390JAC-079")
	require.NoError(t, err)
	require.Equal(t, "sampleid", info.ID)
	require.Equal(t, "390JAC-079", info.Number)
	require.Equal(t, "A Sample Film Title", info.Title)
	require.Equal(t, "Studio Alpha", info.Maker)
	require.Equal(t, "Label Beta", info.Label)
	require.Equal(t, "Series Gamma", info.Series)
	require.Equal(t, "Director One, Director Two", info.Director)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, server.URL+"/covers/sample.jpg", info.CoverURL)
	require.Equal(t, []string{server.URL + "/samples/1.jpg", "https://cdn.example.test/samples/2.jpg"}, []string(info.PreviewImages))
	require.Equal(t, server.URL+"/trailers/sample.mp4", info.PreviewVideoURL)
	require.InDelta(t, 8.42, info.Score, 0.0001)
	require.Equal(t, 102, info.Runtime)
	require.Equal(t, "2024-03-14", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestGetMovieInfoByURLReturnsNotFoundForNonDetailPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body>verification page</body></html>")
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/v/challenge")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)
}

func TestProxyAndTimeoutValidation(t *testing.T) {
	p := New()
	require.Error(t, p.SetProxy("not a URL"))
	require.Error(t, p.SetProxy("ftp://proxy.example.test:21"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}
