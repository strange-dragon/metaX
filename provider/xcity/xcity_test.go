package xcity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const searchHTML = `<!doctype html><html><body><table class="resultList">
<tr><td><a href="/titles/exact">Exact result</a></td></tr>
<tr><td><a href="/titles/near">Near-number result</a></td></tr>
<tr><td><a href="https://evil.invalid/titles/exact">External result</a></td></tr>
</table></body></html>`

func detailHTML(number, title string) string {
	return fmt.Sprintf(`<!doctype html><html><body>
<h1>%s</h1><span class="kopin">%s</span>
<img class="photo" src="/images/cover.jpg">
<ul><li><span>発売日</span>2024/05/16</li><li><span>収録時間</span>120分</li></ul>
<a href="/idol/1">Actor One</a><a href="/idol/2">Actor Two</a>
<a href="/studios/1">Studio One</a><a href="/genres/1">Drama</a><a href="/genres/2">Drama</a>
</body></html>`, title, number)
}

func TestSearchMovieRequiresExactCatalogNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/result_published/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "ABC123", r.URL.Query().Get("q"))
		require.Equal(t, "1", r.Header.Get("Cookie"))
		_, _ = fmt.Fprint(w, searchHTML)
	})
	mux.HandleFunc("/titles/exact", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailHTML("ABC-123", "Exact title"))
	})
	mux.HandleFunc("/titles/near", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailHTML("ABC-1234", "Near title"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "ABC-123", results[0].Number)
	require.Equal(t, "Exact title", results[0].Title)
	require.Equal(t, server.URL+"/titles/exact", results[0].Homepage)
	require.Equal(t, server.URL+"/images/cover.jpg", results[0].CoverURL)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(results[0].Actors))
}

func TestGetMovieInfoByURLMapsMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/titles/exact", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, detailHTML("ABC-123", "Exact title"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByURL(server.URL + "/titles/exact")
	require.NoError(t, err)
	require.Equal(t, "exact", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Exact title", info.Title)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama"}, []string(info.Genres))
	require.Equal(t, 120, info.Runtime)
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestParseMovieIDRejectsUnsafeURLs(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://xcity.jp/titles/exact",
		"https://xcity.jp:444/titles/exact",
		"https://user@xcity.jp/titles/exact",
		"https://xcity.jp.evil.invalid/titles/exact",
		"https://xcity.jp/search/exact",
		"https://xcity.jp/titles/exact?next=https://evil.invalid",
		"https://xcity.jp/titles/exact#fragment",
		"/titles/exact",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		require.Error(t, err, raw)
	}
	id, err := p.ParseMovieIDFromURL("https://xcity.jp/titles/exact")
	require.NoError(t, err)
	require.Equal(t, "exact", id)
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalHits int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalHits++
		_, _ = fmt.Fprint(w, detailHTML("ABC-123", "Exact title"))
	}))
	defer external.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/titles/exact", http.StatusFound)
	}))
	defer source.Close()

	p := newForTest(source.URL, source.Client())
	resp, err := p.Fetch(source.URL + "/titles/exact")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, externalHits)
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
	require.True(t, found)
	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}
