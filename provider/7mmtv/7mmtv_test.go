package sevenmmtv

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

// The fixtures reproduce the markup the catalogue actually serves: cards carry
// the artwork in data-src, the category tag is prepended to the shared title,
// and the heading repeats the number and the performers.
const searchFixture = `<!doctype html><html><body>
<div class="row content">
  <div class='col-6 col-md-4 col-lg-3 col-item'>
    <div class='video'>
      <figure class='video-preview'>
        <a href="/zh/censored_content/123518/SSIS-001.html">
          <img class='lazyload' data-src='/assets/261483_SSIS-001.jpg' alt='SSIS-001 標題 乙白沙也加'>
        </a>
      </figure>
      <h3 class='video-title'><a href="/zh/censored_content/123518/SSIS-001.html">[中字]SSIS-001 標題 乙白沙也加</a></h3>
      <div class='video-info'>
        <div class='row justify-content-between'>
          <div class='col-auto'><div class='video-channel'>yangzhou@</div></div>
          <div class='col-auto'><span class='small text-muted'> 2022-04-23 </span></div>
        </div>
      </div>
    </div>
  </div>
  <div class='col-6 col-md-4 col-lg-3 col-item'>
    <div class='video video-related'>
      <figure class='video-preview'>
        <a href="/zh/censored_content/999999/OTHER-001.html">
          <img class='lazyload' data-src='/assets/other.jpg' alt='OTHER-001 別的'>
        </a>
      </figure>
      <h3 class='video-title'><a href="/zh/censored_content/999999/OTHER-001.html">OTHER-001 別的</a></h3>
    </div>
  </div>
</div>
</body></html>`

const detailFixture = `<!doctype html><html><body>
<div class="content_main_cover_row mb-3">
  <div class="content_main_cover"><a><img src="/assets/261483_SSIS-001_big.jpg"></a></div>
</div>
<div class="fullvideo-details mb-2">
  <h1 class="fullvideo-title h5 mb-2">SSIS-001 真實標題文字 葵つかさ 乙白沙也加</h1>
  <div class="d-flex mb-4">
    <span class='text-muted me-3'>SSIS-001</span>
    <span class='text-muted me-3'>2021-02-18</span>
    <span class='text-muted me-3'>147分</span>
  </div>
  <div class='fullvideo-attr row flex-lg-nowrap g-1 mb-4'>
    <div class='col-auto'><strong>影片類別</strong></div>
    <div class='col-auto flex-shrink-1 flex-grow-1'>
      <div class='d-flex flex-wrap categories'>
        <a href='/zh/censored_category/210/高畫質/1.html'>高畫質</a>
        <a href='/zh/censored_category/80/美少女/1.html'>美少女</a>
      </div>
    </div>
  </div>
  <div class='col-12 col-md-auto'>
    <div class='fullvideo-attr mb-4'>
      <div class='row flex-lg-nowrap g-1'>
        <div class='col-auto'><strong>發行商:</strong></div>
        <div class='col-auto'><a href='/zh/censored_issuer/195/x/1.html'>S1 NO.1 STYLE</a></div>
      </div>
      <div class='row flex-lg-nowrap g-1'>
        <div class='col-auto'><strong>製作商:</strong></div>
        <div class='col-auto'><a href='/zh/censored_makersr/196/y/1.html'>エスワン</a></div>
      </div>
      <div class='row flex-lg-nowrap g-1'>
        <div class='col-auto'><strong>導演:</strong></div>
        <div class='col-auto'><a href='/zh/censored_director/7/z/1.html'>監督名</a></div>
      </div>
    </div>
  </div>
  <a href='/zh/censored_avperformer/28856/乙白沙也加/1.html'>乙白沙也加</a>
  <a href='/zh/censored_avperformer/12345/葵つかさ/1.html'>葵つかさ</a>
</div>
</body></html>`

func newForTest(base string, client *http.Client) *SevenMMTV {
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	return newSevenMMTV(u, client)
}

func TestParseMovieIDFromURL(t *testing.T) {
	p := New()
	id, err := p.ParseMovieIDFromURL("https://7mmtv.sx/zh/censored_content/123518/SSIS-001.html")
	require.NoError(t, err)
	require.Equal(t, "censored:123518:SSIS-001", id)

	// Every language prefix and category addresses the same record.
	for _, raw := range []string{
		"https://7mmtv.sx/zh/uncensored_content/63131/FC2-PPV-5000423.html",
		"https://7mmtv.sx/en/uncensored_content/63131/FC2-PPV-5000423.html",
		"https://7mmtv.sx/ja/reducing-mosaic_content/11221/SSIS-001.html",
	} {
		id, err := p.ParseMovieIDFromURL(raw)
		require.NoError(t, err, raw)
		require.Contains(t, id, ":")
	}

	// A search URL is addressable too: it yields the keyword, not a record.
	keyword, err := p.ParseMovieIDFromURL("https://7mmtv.sx/zh/searchall_search/all/SSIS-001/1.html")
	require.NoError(t, err)
	require.Equal(t, "SSIS-001", keyword)

	for _, raw := range []string{
		"https://evil.invalid/zh/censored_content/1/X.html",
		"https://7mmtv.sx/zh/censored_category/210/高畫質/1.html",
		"https://7mmtv.sx/zh/censored_content/notanumber/X.html",
		"https://7mmtv.sx/",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		require.Error(t, err, raw)
	}
}

func TestSearchAndDetailParsing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zh/searchall_search/all/SSIS-001/1.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/zh/censored_content/123518/SSIS-001.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())

	results, err := p.SearchMovie("ssis-001")
	require.NoError(t, err)
	require.Len(t, results, 1, "the related-card block must not be read as a result")
	require.Equal(t, "SSIS-001", results[0].Number)
	require.Equal(t, "標題 乙白沙也加", results[0].Title, "the category tag is stripped")
	require.Equal(t, "censored:123518:SSIS-001", results[0].ID)
	require.NotEmpty(t, results[0].CoverURL)
	require.Equal(t, "2022-04-23", time.Time(results[0].ReleaseDate).Format("2006-01-02"))

	info, err := p.GetMovieInfoByID("censored:123518:SSIS-001")
	require.NoError(t, err)
	require.Equal(t, "SSIS-001", info.Number)
	require.Equal(t, "真實標題文字", info.Title, "the heading repeats the number and the performers")
	require.Equal(t, "S1 NO.1 STYLE", info.Maker)
	require.Equal(t, "エスワン", info.Label)
	require.Equal(t, "監督名", info.Director)
	require.Equal(t, []string{"乙白沙也加", "葵つかさ"}, []string(info.Actors))
	require.Equal(t, []string{"高畫質", "美少女"}, []string(info.Genres))
	require.NotEmpty(t, info.CoverURL)
	require.Equal(t, "2021-02-18", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestGetMovieInfoByIDResolvesBareNumberThroughSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zh/searchall_search/all/SSIS-001/1.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, searchFixture)
	})
	mux.HandleFunc("/zh/censored_content/123518/SSIS-001.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("ssis-001")
	require.NoError(t, err)
	require.Equal(t, "SSIS-001", info.Number)
}

func TestGetMovieInfoByIDRejectsForeignRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("censored:1:NOPE-001")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)
	require.ErrorIs(t, err, mt.ErrMovieNotFound)
}

func TestAssetAndOriginGuards(t *testing.T) {
	p := New()
	for raw, want := range map[string]bool{
		"https://n3.1024cdn.sx/censored/m/261483_SSIS-001.jpg": true,
		"https://n3.1026cdn.sx/censored/b/261483_SSIS-001.jpg": true,
		"https://n39s.1024cdn.sx/censored/261483_SSIS-001.mp4": true,
		"https://n3.1024cdn.sx.evil.invalid/x.jpg":             false,
		"https://evil.invalid/censored/m/261483_SSIS-001.jpg":  false,
		"http://n3.1024cdn.sx/censored/m/261483_SSIS-001.jpg":  false,
		"https://7mmtv.sx/zh/censored_content/1/A-001.html":    false,
	} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, want, isAssetURL(u), raw)
	}
	require.False(t, isAssetURL(nil))

	require.True(t, p.validPageOrigin(mustParseURL("https://7mmtv.sx/zh/")))
	require.True(t, p.validPageOrigin(mustParseURL("https://www.7mmtv.sx/zh/")))
	require.False(t, p.validPageOrigin(mustParseURL("https://7mmtv.sx.evil.invalid/zh/")))
	require.False(t, p.validPageOrigin(mustParseURL("http://7mmtv.sx/zh/")))
}

func TestFetchRejectsForeignHost(t *testing.T) {
	p := New()
	_, err := p.Fetch("https://evil.invalid/x.jpg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected origin")
}
