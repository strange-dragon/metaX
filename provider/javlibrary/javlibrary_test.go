package javlibrary

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const searchHTML = `<!doctype html><html><body>
<div class="video"><a href="/cn/?v=abc123" title="Exact title ABC-123"><img src="/images/cover.jpg"></a></div>
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
<img id="video_jacket_img" src="/images/cover.jpg">
<span class="score">(7.8)</span>
</body></html>`

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
	require.Equal(t, server.URL+"/images/cover.jpg", info.CoverURL)
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
		"http://www.javlibrary.com/cn/?v=abc123",
		"https://www.javlibrary.com:444/cn/?v=abc123",
		"https://user@www.javlibrary.com/cn/?v=abc123",
		"https://www.javlibrary.com.evil.invalid/cn/?v=abc123",
		"https://www.javlibrary.com/cn/vl_searchbyid.php?keyword=ABC-123",
		"https://www.javlibrary.com/cn/?v=../evil",
		"/cn/?v=abc123",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		require.Error(t, err, raw)
	}
	id, err := p.ParseMovieIDFromURL("https://www.javlibrary.com/cn/?v=abc123")
	require.NoError(t, err)
	require.Equal(t, "abc123", id)
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
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestFetchRetries403WithBrowserFingerprint(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.Contains(r.UserAgent(), "Chrome/131.0.0.0") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "fingerprint accepted")
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	resp, err := p.Fetch(server.URL + "/cn/vl_searchbyid.php?keyword=ABC-123")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "fingerprint accepted", string(body))
	require.Equal(t, 2, requests, "should try standard net/http once and rotate to Chrome 131")
}
