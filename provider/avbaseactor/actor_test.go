package avbaseactor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lib/pq"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const talentFixture = `<!doctype html><html><body><script id="__NEXT_DATA__" type="application/json">{
  "props":{"pageProps":{"talent":{
    "id":42,
    "primary":{"name":"Canonical Name","note":"女優","image_url":"/images/actor.jpg"},
    "actors":[{"name":"Canonical Name"},{"name":"Alias Name"},{"name":"Alias Name"}],
    "meta":{"basic_info":{
      "birthday":"1990-01-02","prefectures":"Tokyo","height":"165",
      "bust":"88","waist":"59","hip":"90","cup":"C"
    }}
  }}}
}</script></body></html>`

func testServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestSearchAndDetailParsingWithAliasRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/talents/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/talents/Alias Name":
			http.Redirect(w, r, "/talents/Canonical%20Name", http.StatusFound)
		case "/talents/Canonical Name":
			_, _ = fmt.Fprint(w, talentFixture)
		default:
			http.NotFound(w, r)
		}
	})
	server := testServer(t, mux)
	p := newForTest(server.URL, server.Client())

	results, err := p.SearchActor("Alias Name")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "Canonical Name", results[0].Name)
	require.Equal(t, "Canonical Name", results[0].ID)
	require.Equal(t, []string{"Alias Name"}, []string(results[0].Aliases))
	require.Equal(t, []string{server.URL + "/images/actor.jpg"}, []string(results[0].Images))
	require.Equal(t, server.URL+"/talents/Canonical%20Name", results[0].Homepage)

	info, err := p.GetActorInfoByURL(results[0].Homepage)
	require.NoError(t, err)
	require.Equal(t, Name, info.Provider)
	require.Equal(t, "1990-01-02", time.Time(info.Birthday).Format("2006-01-02"))
	require.Equal(t, 165, info.Height)
	require.Equal(t, "B:88 / W:59 / H:90", info.Measurements)
	require.Equal(t, "C", info.CupSize)
	require.Equal(t, pq.StringArray{"Alias Name"}, info.Aliases)
	require.Equal(t, pq.StringArray{server.URL + "/images/actor.jpg"}, info.Images)
}

func TestSearchActorReturnsMissFor404AndUnmatchedName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/talents/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/talents/Other Name" {
			_, _ = fmt.Fprint(w, talentFixture)
			return
		}
		http.NotFound(w, r)
	})
	server := testServer(t, mux)
	p := newForTest(server.URL, server.Client())

	results, err := p.SearchActor("Unknown Name")
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = p.SearchActor("Other Name")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestMissingNextDataIsNotFound(t *testing.T) {
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body>no talent data</body></html>")
	}))
	p := newForTest(server.URL, server.Client())

	info, err := p.GetActorInfoByID("Some Name")
	require.ErrorIs(t, err, ErrActorNotFound)
	require.Nil(t, info)
}

func TestParseActorIDFromURLRejectsInvalidOriginsAndPaths(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://www.avbase.net/talents/Name",
		"https://www.avbase.net:444/talents/Name",
		"https://user@www.avbase.net/talents/Name",
		"https://www.avbase.net.evil.invalid/talents/Name",
		"https://www.avbase.net/talents/Name?next=other",
		"https://www.avbase.net/talents/Name#fragment",
		"https://www.avbase.net/talents/Name/extra",
		"https://www.avbase.net/talents/../admin",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := p.ParseActorIDFromURL(raw)
			require.Error(t, err)
		})
	}
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalRequests int
	external := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, talentFixture)
	}))
	mux := http.NewServeMux()
	mux.HandleFunc("/talents/Name", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/talents/Name", http.StatusFound)
	})
	server := testServer(t, mux)
	p := newForTest(server.URL, server.Client())

	info, err := p.GetActorInfoByURL(server.URL + "/talents/Name")
	require.Error(t, err)
	require.Nil(t, info)
	require.Zero(t, externalRequests)
}

func TestRegistrationAndConfiguration(t *testing.T) {
	found := false
	mt.RangeActorFactory(func(name string, factory mt.ActorFactory) bool {
		if name == Name {
			found = true
			require.Equal(t, Name, factory().Name())
			return false
		}
		return true
	})
	require.True(t, found, "AVBase actor provider should register with MetaTube")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}
