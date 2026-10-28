package javlibrary

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

const searchHTML = `<!doctype html><html><body>
<div class="video"><a href="/cn/?v=abc123" title="Exact title ABC-123"><img src="/images/cover.jpg"></a></div>
<div class="video"><a href="./javme5adzi.html" title="SSIS-033 Exact title"><div class="id">SSIS-033</div><img src="https://pics.dmm.co.jp/mono/movie/adult/ssis033/ssis033ps.jpg"></a></div>
<div class="video"><a href="/cn/?v=near1234" title="Near title ABC-1234"></a></div>
<div class="video"><a href="https://evil.invalid/cn/?v=abc123" title="Exact title ABC-123"></a></div>
</body></html>`

const detailHTML = `<!doctype html><html><head><meta property="og:image" content="/images/cover.jpg"></head><body>
<h3 class="post-title"><a>Exact title ABC-123</a></h3>
<div id="video_id"><table><tr><td class="text">ABC-123</td></tr></table></div>
<div id="video_date"><table><tr><td class="text">2024-05-16</td></tr></table></div>
<div id="video_length"><table><tr><td class="text">120 min</td></tr></table></div>
<div id="video_director"><table><tr><td class="text"><a>Director One</a></td></tr></table></div>
<div id="video_maker"><table><tr><td class="text"><a>Studio One</a></td></tr></table></div>
<div id="video_label"><table><tr><td class="text"><a>Label One</a></td></tr></table></div>
<div id="video_genres"><table><tr><td class="text"><span class="genre"><a>Drama</a></span><span class="genre"><a>Drama</a></span><span class="genre"><a>Romance</a></span></td></tr></table></div>
<span class="cast"><span class="star"><a>Actor One</a></span><span class="star"><a>Actor Two</a></span></span>
<img id="video_jacket_img" src="https://pics.dmm.co.jp/mono/movie/adult/ssis033/ssis033pl.jpg">
<div class="previewthumbs"><a href="https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-1.jpg"><img src="https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033-1.jpg"></a></div>
<span class="score">(7.8)</span>
</body></html>`

type testConfig map[string]string

func (c testConfig) Has(key string) bool { _, ok := c[strings.ToLower(key)]; return ok }
func (c testConfig) GetString(key string) (string, error) {
	value, ok := c[strings.ToLower(key)]
	if !ok {
		return "", fmt.Errorf("missing config key %q", key)
	}
	return value, nil
}
func (testConfig) GetBool(string) (bool, error)              { return false, nil }
func (testConfig) GetInt64(string) (int64, error)            { return 0, nil }
func (testConfig) GetFloat64(string) (float64, error)        { return 0, nil }
func (testConfig) GetDuration(string) (time.Duration, error) { return 0, nil }

func TestSetConfigAcceptsAlternateHTTPSOrigin(t *testing.T) {
	p := New()
	require.NoError(t, p.SetConfig(testConfig{"base_url": "https://www.h103n.com/"}))
	require.Equal(t, "https://www.h103n.com", p.URL().String())
	require.NoError(t, p.SetConfig(testConfig{"cookies": "cf_clearance=clearance-token; session=session-token"}))
	jarCookies := p.cookieJar.Cookies(p.URL())
	require.Len(t, jarCookies, 2)
	require.Equal(t, "cf_clearance", jarCookies[0].Name)
	require.Equal(t, "clearance-token", jarCookies[0].Value)
	require.Error(t, p.SetConfig(testConfig{"cookies": "session=abc\r\nInjected: yes"}))
	for _, invalid := range []string{
		"http://www.h103n.com", "https://user@www.h103n.com", "https://www.h103n.com/cn",
		"https://www.h103n.com?next=evil", "https://www.h103n.com#fragment",
	} {
		require.Error(t, p.SetConfig(testConfig{"base_url": invalid}), invalid)
	}
}

func TestSearchMovieRequiresExactCatalogNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/vl_searchbyid.php", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, searchHTML) })
	server := httptest.NewServer(mux)
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "ABC-123", results[0].Number)
	require.Equal(t, "Exact title", results[0].Title)
	require.Equal(t, server.URL+"/cn/?v=abc123", results[0].Homepage)
	require.Equal(t, server.URL+"/images/cover.jpg", results[0].ThumbURL)
}

func TestSearchMovieResolvesCurrentRelativeSlug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/cn/vl_searchbyid.php", r.URL.Path)
		_, _ = fmt.Fprint(w, `<div class="video"><a href="./javme5adzi.html" title="SSIS-033 Current title"><div class="id">SSIS-033</div><img src="https://pics.dmm.co.jp/ssis033ps.jpg"></a></div>`)
	}))
	defer server.Close()
	results, err := newForTest(server.URL, server.Client()).SearchMovie("SSIS-033")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "javme5adzi", results[0].ID)
	require.Equal(t, "SSIS-033", results[0].Number)
	require.Equal(t, "Current title", results[0].Title)
	require.Equal(t, server.URL+"/cn/javme5adzi.html", results[0].Homepage)
}

func TestGetMovieInfoByIDUsesOpaqueDetailSlug(t *testing.T) {
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		_, _ = fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByID("javme5adzi")
	require.NoError(t, err)
	require.Equal(t, "/cn/javme5adzi.html", requested)
	require.Equal(t, "javme5adzi", info.ID)
}

func TestGetMovieInfoByURLMapsMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, detailHTML) })
	server := httptest.NewServer(mux)
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/cn/?v=abc123")
	require.NoError(t, err)
	require.Equal(t, "abc123", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Exact title ABC-123", info.Title)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, "Label One", info.Label)
	require.Equal(t, "Director One", info.Director)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, "https://pics.dmm.co.jp/mono/movie/adult/ssis033/ssis033pl.jpg", info.CoverURL)
	require.Equal(t, "https://pics.dmm.co.jp/mono/movie/adult/ssis033/ssis033pl.jpg", info.BigCoverURL)
	require.Equal(t, "https://pics.dmm.co.jp/mono/movie/adult/ssis033/ssis033ps.jpg", info.ThumbURL)
	require.Equal(t, []string{"https://pics.dmm.co.jp/digital/video/ssis00033/ssis00033jp-1.jpg"}, []string(info.PreviewImages))
	require.Equal(t, 7.8, info.Score)
	require.Equal(t, 120, info.Runtime)
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestGetMovieInfoByIDSearchesByCatalogNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/vl_searchbyid.php", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, searchHTML) })
	mux.HandleFunc("/cn/", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, detailHTML) })
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByID("ABC-123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "abc123", info.ID)
}

func TestParseMovieIDRejectsUnexpectedURL(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://www.f101w.com/cn/?v=abc123",
		"https://www.f101w.com:444/cn/?v=abc123",
		"https://user@www.f101w.com/cn/?v=abc123",
		"https://www.f101w.com.evil.invalid/cn/?v=abc123",
		"https://www.f101w.com/cn/vl_searchbyid.php?keyword=ABC-123",
		"https://www.f101w.com/cn/?v=../evil",
		"/cn/?v=abc123",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		require.Error(t, err, raw)
	}
	id, err := p.ParseMovieIDFromURL("https://www.f101w.com/cn/?v=abc123")
	require.NoError(t, err)
	require.Equal(t, "abc123", id)
	id, err = p.ParseMovieIDFromURL("https://www.f101w.com/cn/javme5adzi.html")
	require.NoError(t, err)
	require.Equal(t, "javme5adzi", id)
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var hit int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit++; _, _ = fmt.Fprint(w, detailHTML) }))
	defer external.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/cn/?v=abc123", http.StatusFound)
	}))
	defer source.Close()
	p := newForTest(source.URL, source.Client())
	resp, err := p.Fetch(source.URL + "/cn/?v=abc123")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, hit)
}

func TestRegistrationAndSettings(t *testing.T) {
	found := false
	for name, factory := range mt.RangeMovieFactory {
		if name == Name {
			found = true
			instance := factory()
			require.Equal(t, Name, instance.Name())
			require.Equal(t, float64(Priority), instance.Priority())
			break
		}
	}
	require.True(t, found)
	p := New()
	require.Equal(t, "https://www.f101w.com", p.URL().String())
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestFetchRetries403WithBrowserFingerprint(t *testing.T) {
	var requests int
	var cookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		cookies = append(cookies, r.Header.Get("Cookie"))
		if strings.Contains(r.UserAgent(), "Chrome/131.0.0.0") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "fingerprint accepted")
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	require.NoError(t, p.SetConfig(testConfig{"cookies": "cf_clearance=test-token"}))
	resp, err := p.Fetch(server.URL + "/cn/vl_searchbyid.php?keyword=ABC-123")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "fingerprint accepted", string(body))
	require.Equal(t, 2, requests, "should try the current Chrome profile before rotating to Chrome 131")
	require.Equal(t, []string{"cf_clearance=test-token", "cf_clearance=test-token"}, cookies)
}

func TestIsCloudflareChallengeRecognizesHTMLChallengeTitle(t *testing.T) {
	err := fmt.Errorf(`browser fingerprint attempts exhausted: Chrome 146=403 body="<!DOCTYPE html><title>Just a moment...</title>"`)
	require.True(t, isCloudflareChallenge(err))
	require.True(t, isCloudflareChallenge(fmt.Errorf("页面标题：请稍候…")))
	require.True(t, isCloudflareChallenge(fmt.Errorf("Cloudflare response contains cf-chl-widget")))
	require.False(t, isCloudflareChallenge(fmt.Errorf("CloudFront returned 403 Forbidden")))
	require.False(t, isCloudflareChallenge(nil))
}

func TestChromiumFallbackLoadsLocalFixtureWhenAvailable(t *testing.T) {
	if os.Getenv("METAX_CHROMIUM_BIN") == "" {
		if _, err := exec.LookPath("chromium"); err != nil {
			t.Skip("Chromium is not installed; set METAX_CHROMIUM_BIN to run this integration test")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>local fixture</title></head><body>browser fallback fixture</body></html>`)
	}))
	defer server.Close()
	p := newForTest(server.URL, server.Client())
	request, err := http.NewRequest(http.MethodGet, server.URL+"/fixture", nil)
	require.NoError(t, err)
	resp, err := p.fetchWithChromium(request, time.Now().Add(time.Minute))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "browser fallback fixture")
}
