package jav321

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/strange-dragon/metaX/internal/antibot"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

// detailFixture mirrors the live Jav321 detail layout captured on 2026-10-28:
// a panel heading whose title is followed by a nested <small> repeating the
// compressed number and the cast, a metadata block of <b>label</b> pairs, a
// video panel that also carries the package cover as its poster, one free-text
// description panel, and a series panel.
const detailFixture = `<!doctype html><html><body>
<div class="panel panel-info"><div class="panel-heading"><h3>Sample Jav321 Title 七ツ森りり <small>ssis-033 七ツ森りり</small></h3></div>
<div class="panel-body">
<div class="row"><div class="col-md-3"><img class="img-responsive" src="http://pics.dmm.co.jp/digital/video/ssis00033/ssis00033ps.jpg"></div>
<div class="col-md-9"><b>出演者</b>: <a href="/star/1063214/1">七ツ森りり</a> &nbsp; <a href="/star/2/1">Actor Two</a><br>
<b>メーカー</b>: <a href="/company/x/1">エスワン ナンバーワンスタイル</a><br>
<b>品番</b>: ssis-033<br>
<b>配信開始日</b>: 2021-04-07<br>
<b>収録時間</b>: 169 minutes<br>
<b>お気に入り登録数</b>: 4717<br>
<b>平均評価</b>: 4.5<br></div></div>
<div class="row"><div class="col-md-12"><video poster="http://pics.dmm.co.jp//digital/video/ssis00033/ssis00033pl.jpg"><source src="https://example.invalid/ssis00033_dmb_w.mp4" type="video/mp4"></video>
<div style="position:absolute"><ins id="542694"></ins><script>(adsbyjuicy = window.adsbyjuicy || []).push({'adzone':542694});</script></div></div></div>
<div class="row"><div class="col-md-12">A short synopsis of the sample work, long enough to be treated as the description.</div></div>
<div class="row"><div class="col-md-12"><a href="/star/1063214/1"><img src="http://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-2.jpg"></a>
<a href="/star/1063214/1"><img src="http://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-1.jpg"></a>
<a href="/video/ssni00985"><img src="http://pics.dmm.co.jp/digital/video/ssni00985/ssni00985ps.jpg"></a></div></div>
</div></div>
<div class="panel panel-info"><div class="panel-heading"><a href="/snp/ssis/1">SSIS</a>シリーズ</div><div class="panel-body"></div></div>
</body></html>`

// genreFixture adds the genre block that Jav321 renders on other categories.
const genreFixture = `<!doctype html><html><body>
<div class="panel-heading"><h3>Amateur Sample <small>abc-123</small></h3></div>
<div class="panel-body"><div class="row"><div class="col-md-3"><img src="https://pics.dmm.co.jp/digital/video/abc00123/abc00123ps.jpg"></div>
<div class="col-md-9"><b>出演者</b>: <a>Actor One</a> &nbsp; <br><b>メーカー</b>: <a>Studio One</a><br><b>品番</b>: abc-123<br>
<b>配信開始日</b>: 2024-02-19<br><b>収録時間</b>: 120分<br><b>ジャンル</b>: <a>Drama</a> &nbsp; <a>Romance</a><br></div></div>
<div class="row"><div class="col-md-12">Second synopsis, also long enough to be the description text of the work.</div></div></div>
</body></html>`

// newTestProvider builds a provider whose fetch layer cannot launch Chromium, so
// unit tests stay deterministic.
func newTestProvider(t *testing.T, base string) *Jav321 {
	t.Helper()
	p := newForTest(base, nil)
	p.stateMu.Lock()
	p.fetcher.Close()
	p.fetcher = antibot.New(antibot.Options{
		Timeout:        10 * time.Second,
		AllowURL:       p.allowedURL,
		DisableBrowser: true,
	})
	p.stateMu.Unlock()
	return p
}

func TestNormalizeAndParseMovieID(t *testing.T) {
	p := New()
	require.Equal(t, "SSIS-033", p.NormalizeMovieID("　ssis-033 "))
	require.Equal(t, "SSIS-033", p.NormalizeMovieKeyword(" ssis-033 "))
	id, err := p.ParseMovieIDFromURL("https://www.jav321.com/video/ssis00033/")
	require.NoError(t, err)
	require.Equal(t, "SSIS00033", id)
	// Some labels get a digit prefix in the slug to avoid a routing collision
	// on the site (SDMM-031 -> /video/1sdmm00031). Rejecting the prefix made
	// the provider answer 404 for works the site clearly has.
	id, err = p.ParseMovieIDFromURL("https://www.jav321.com/video/1sdmm00031")
	require.NoError(t, err)
	require.Equal(t, "1SDMM00031", id)
	_, err = p.ParseMovieIDFromURL("https://example.invalid/video/SSIS-033")
	require.Error(t, err)
	_, err = p.ParseMovieIDFromURL("https://www.jav321.com/search?q=SSIS-033")
	require.Error(t, err)
}

func TestDetailParsingMapsLiveLayout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/video/ssis00033", r.URL.Path)
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer server.Close()

	info, err := newTestProvider(t, server.URL).GetMovieInfoByURL(server.URL + "/video/ssis00033")
	require.NoError(t, err)

	require.Equal(t, "SSIS-033", info.Number)
	require.Equal(t, "SSIS-033", info.ID)
	// The nested <small> must not leak into the title.
	require.Equal(t, "Sample Jav321 Title 七ツ森りり", info.Title)
	require.Equal(t, "A short synopsis of the sample work, long enough to be treated as the description.", info.Summary)
	require.NotContains(t, info.Summary, "adsbyjuicy")
	require.Equal(t, []string{"七ツ森りり", "Actor Two"}, []string(info.Actors))
	require.Equal(t, "エスワン ナンバーワンスタイル", info.Maker)
	require.Equal(t, "SSIS", info.Series)
	require.Equal(t, 169, info.Runtime)
	require.Equal(t, 4.5, info.Score)
	require.Equal(t, "2021-04-07", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, "https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033pl.jpg", info.CoverURL)
	require.Equal(t, "https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033ps.jpg", info.ThumbURL)
	require.Equal(t, "https://example.invalid/ssis00033_dmb_w.mp4", info.PreviewVideoURL)
	require.Equal(t, []string{
		"https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-1.jpg",
		"https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-2.jpg",
	}, []string(info.PreviewImages))
}

func TestDetailParsingKeepsGenresWhenPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, genreFixture)
	}))
	defer server.Close()

	info, err := newTestProvider(t, server.URL).GetMovieInfoByURL(server.URL + "/video/abc00123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Amateur Sample", info.Title)
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, 120, info.Runtime)
	require.Equal(t, "https://pics.dmm.co.jp/digital/video/abc00123/abc00123pl.jpg", info.CoverURL)
}

func TestSearchRedirectsToCompactSlugThenParsesNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "SSIS-033", r.Form.Get("sn"))
		http.Redirect(w, r, "/video/ssis00033", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/video/ssis00033", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newTestProvider(t, server.URL).SearchMovie("ssis-033")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "SSIS-033", results[0].Number)
	require.Equal(t, server.URL+"/video/ssis00033", results[0].Homepage)
}

func TestSearchMismatchedNumberIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/video/ssis00033", http.StatusFound)
	})
	mux.HandleFunc("/video/ssis00033", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newTestProvider(t, server.URL).SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearchMovieIgnoresNonNumberKeywords(t *testing.T) {
	results, err := newTestProvider(t, "https://www.jav321.com").SearchMovie("七ツ森りり")
	require.NoError(t, err)
	require.Nil(t, results)
}

func TestChallengePageIsReportedAsBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>Just a moment...</title></head><body>cf-chl</body></html>`)
	}))
	defer server.Close()

	_, err := newTestProvider(t, server.URL).GetMovieInfoByURL(server.URL + "/video/ssis00033")
	require.Error(t, err)
	require.Contains(t, err.Error(), "edge protection")
}

func TestNotFoundPageAndRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := newTestProvider(t, server.URL).GetMovieInfoByURL(server.URL + "/video/ssis00033")
	require.ErrorIs(t, err, ErrMovieNotFound)

	found := false
	mt.RangeMovieFactory(func(name string, factory mt.MovieFactory) bool {
		if name == Name {
			found = true
			instance := factory()
			require.Equal(t, Name, instance.Name())
			require.Equal(t, float64(Priority), instance.Priority())
			return false
		}
		return true
	})
	require.True(t, found)
}

func TestFetchRejectsUnexpectedOrigin(t *testing.T) {
	_, err := New().Fetch("https://example.invalid/video/ssis00033")
	require.Error(t, err)
}

// Artwork is hosted on the DMM picture CDN, so the origin guard has to admit it
// while still refusing unrelated hosts.
func TestFetchAllowsEmbeddedImageCDN(t *testing.T) {
	p := New()
	for _, rawURL := range []string{
		"https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033pl.jpg",
		"https://p.dmm.co.jp/mono/actjpgs/example.jpg",
		"https://www.jav321.com/digital/video/ssis00033/ssis00033pl.jpg",
	} {
		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		require.True(t, p.allowedURL(u), rawURL)
	}
	for _, rawURL := range []string{
		"https://evil.invalid/cover.jpg",
		"https://c0.jdbstatic.com/covers/ss/SSIS-001.jpg",
		"http://pics.dmm.co.jp/digital/video/ssis00033/ssis00033pl.jpg",
	} {
		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		require.False(t, p.allowedURL(u), rawURL)
	}
}

func TestSetConfigAppliesBaseURLAndCookies(t *testing.T) {
	p := New()
	require.NoError(t, p.SetConfig(testConfig{"base_url": "https://jav321.com/", "cookies": "cf_clearance=token"}))
	require.Equal(t, "https://jav321.com", p.URL().String())
	require.Equal(t, "cf_clearance=token", p.currentFetcher().Options().Cookies)
	require.Error(t, p.SetConfig(testConfig{"cookies": "a=b\r\nInjected: yes"}))
	for _, invalid := range []string{"http://www.jav321.com", "https://user@www.jav321.com", "https://www.jav321.com/search"} {
		require.Error(t, p.SetConfig(testConfig{"base_url": invalid}), invalid)
	}
}

func TestProxyAndTimeout(t *testing.T) {
	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy("socks5://127.0.0.1:1080"))
	require.Equal(t, "socks5://127.0.0.1:1080", p.currentFetcher().Options().ProxyURL)
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.currentFetcher().Options().Timeout)
}

type testConfig map[string]string

func (c testConfig) Has(key string) bool { _, ok := c[key]; return ok }
func (c testConfig) GetString(key string) (string, error) {
	value, ok := c[key]
	if !ok {
		return "", fmt.Errorf("missing config key %q", key)
	}
	return value, nil
}
func (testConfig) GetBool(string) (bool, error)              { return false, nil }
func (testConfig) GetInt64(string) (int64, error)            { return 0, nil }
func (testConfig) GetFloat64(string) (float64, error)        { return 0, nil }
func (testConfig) GetDuration(string) (time.Duration, error) { return 0, nil }
