package giga

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const searchFixture = `<!doctype html><html><body>
<div class="thumBox"><a href="/product/index.php?product_id=1002">ABC-1234 Near match</a></div>
<div class="thumBox"><a href="/product/index.php?product_id=1001">ABC-123 Exact title</a></div>
</body></html>`

func detailFixture(number, title string) string {
	return fmt.Sprintf(`<!doctype html><html><body>
<div id="works_pic"><h5>%s</h5><img src="/images/poster.jpg"></div>
<div id="works_txt">
  <dl><dt>作品番号</dt><dd>%s</dd></dl>
  <dl><dt>DVDリリース日</dt><dd>2024/05/16</dd></dl>
  <dl><dt>収録時間</dt><dd>120分</dd></dl>
  <a href="/actor.php?actor_id=1">Actor One</a>
  <a href="/actor.php?actor_id=2">Actor Two</a>
</div>
<div class="gasatsu_images_pc"><img data-src="/images/sample-1.jpg"><img src="/images/sample-2.jpg"></div>
</body></html>`, title, number)
}

func ageGateServer(t *testing.T, handler http.Handler) (*httptest.Server, *int) {
	t.Helper()
	gateRequests := 0
	mux := http.NewServeMux()
	mux.HandleFunc(ageGatePath, func(w http.ResponseWriter, r *http.Request) {
		gateRequests++
		if gateRequests == 1 {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "deleted", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "age_confirmed", Value: "1", Path: "/"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "active-session", Path: "/"})
	})
	mux.Handle("/", handler)
	server := httptest.NewServer(mux)
	return server, &gateRequests
}

func TestSearchAndDetailParsingWithAgeGate(t *testing.T) {
	var detailRequests int
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/" {
			require.Contains(t, r.Header.Get("Cookie"), "PHPSESSID=active-session")
			_, _ = fmt.Fprint(w, searchFixture)
			return
		}
		if r.URL.Path == "/product/index.php" {
			require.Contains(t, r.Header.Get("Cookie"), "PHPSESSID=active-session")
			detailRequests++
			if r.URL.Query().Get("product_id") == "1002" {
				_, _ = fmt.Fprint(w, detailFixture("ABC-1234", "ABC-1234 Near match"))
				return
			}
			_, _ = fmt.Fprint(w, detailFixture("ABC-123", "ABC-123 Exact title"))
			return
		}
		http.NotFound(w, r)
	})
	server, gateRequests := ageGateServer(t, mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "1001", results[0].ID)
	require.Equal(t, "ABC-123", results[0].Number)
	require.Equal(t, "ABC-123 Exact title", results[0].Title)
	require.Equal(t, server.URL+"/product/index.php?product_id=1001", results[0].Homepage)
	require.Equal(t, server.URL+"/images/poster.jpg", results[0].ThumbURL)
	require.Equal(t, 2, *gateRequests)
	require.Equal(t, 2, detailRequests)

	info, err := p.GetMovieInfoByID("1001")
	require.NoError(t, err)
	require.Equal(t, "1001", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "GIGA", info.Maker)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{server.URL + "/images/sample-1.jpg", server.URL + "/images/sample-2.jpg"}, []string(info.PreviewImages))
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, 120, info.Runtime)
}

func TestParseMovieIDFromURLRejectsUnsafeURLs(t *testing.T) {
	p := New()
	for _, rawURL := range []string{
		"http://www.giga-web.jp/product/index.php?product_id=1001",
		"https://www.giga-web.jp:444/product/index.php?product_id=1001",
		"https://user@www.giga-web.jp/product/index.php?product_id=1001",
		"https://www.giga-web.jp.evil.invalid/product/index.php?product_id=1001",
		"https://www.giga-web.jp/search/?keyword=ABC-123",
		"https://www.giga-web.jp/product/index.php?product_id=../1001",
		"https://www.giga-web.jp/product/index.php",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := p.ParseMovieIDFromURL(rawURL)
			require.Error(t, err)
		})
	}
	id, err := p.ParseMovieIDFromURL("https://www.giga-web.jp/product/index.php?product_id=1001")
	require.NoError(t, err)
	require.Equal(t, "1001", id)
}

func TestDetailRejectsRedirectToDifferentProduct(t *testing.T) {
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ageGatePath {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "active-session", Path: "/"})
			return
		}
		http.Redirect(w, r, "/product/index.php?product_id=1002", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/product/index.php?product_id=1001")
	require.Nil(t, info)
	require.ErrorIs(t, err, ErrMovieNotFound)
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalRequests int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, detailFixture("ABC-123", "ABC-123 title"))
	}))
	defer external.Close()

	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ageGatePath {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "active-session", Path: "/"})
			return
		}
		http.Redirect(w, r, external.URL+"/product/index.php?product_id=1001", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	resp, err := p.Fetch(server.URL + "/product/index.php?product_id=1001")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, externalRequests)
}

func TestAgeGateFailureIsNotReportedAsNotFound(t *testing.T) {
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ageGatePath {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "active-session", Path: "/"})
			return
		}
		_, _ = fmt.Fprint(w, `<html><body><a href="/cookie_set.php">はい</a></body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	_, err := p.GetMovieInfoByURL(server.URL + "/product/index.php?product_id=1001")
	require.ErrorIs(t, err, ErrAgeVerification)
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
	require.True(t, found, "GIGA should register with MetaTube's Provider factory")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.client.Timeout)
}

func TestGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	p := New()
	_, err := p.GetMovieInfoByID("../1001")
	require.Error(t, err)
}

func TestNormalizeMovieKeyword(t *testing.T) {
	p := New()
	require.Equal(t, "ABC-123", p.NormalizeMovieKeyword(" ＡＢＣ-123 "))
	require.True(t, strings.EqualFold(p.NormalizeMovieID("1001"), "1001"))
}
