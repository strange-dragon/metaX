package minnano

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const profileFixture = `<!doctype html><html><head>
<link rel="canonical" href="/actress123.html">
<meta property="og:image" content="/images/actress123.jpg">
</head><body><div class="act-profile">
<h2>Example Name（えぐざんぷる / Example Name）</h2>
<table>
<tr><td><span>生年月日</span></td><td><p>1990年1月2日</p></td></tr>
<tr><td><span>身長</span></td><td><p>160cm</p></td></tr>
<tr><td><span>サイズ</span></td><td><p>T160 / B85(C) / W58 / H86</p></td></tr>
<tr><td><span>別名</span></td><td><p>Former Name（ふぉーまー / Former Name）</p></td></tr>
</table></div></body></html>`

func testActor(serverURL string, client *http.Client) *Actor {
	u, _ := url.Parse(serverURL)
	return &Actor{baseURL: u, client: client, priority: Priority}
}

func TestParseActorURLValidation(t *testing.T) {
	p := NewActor()
	id, err := p.ParseActorIDFromURL("https://www.minnano-av.com/actress123.html")
	require.NoError(t, err)
	require.Equal(t, "123", id)
	for _, raw := range []string{
		"https://example.invalid/actress123.html",
		"http://www.minnano-av.com/actress123.html",
		"https://www.minnano-av.com:8443/actress123.html",
		"https://user@www.minnano-av.com/actress123.html",
		"https://www.minnano-av.com/actress123.html?next=/",
		"https://www.minnano-av.com/actress123.html#fragment",
		"https://www.minnano-av.com/other/actress123.html",
		"https://www.minnano-av.com/actressabc.html",
	} {
		_, err := p.ParseActorIDFromURL(raw)
		require.Error(t, err, raw)
	}
}

func TestMinnanoActorSearchAndProfile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search_result.php", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "actress", r.URL.Query().Get("search_scope"))
		require.Equal(t, "Example Name", r.URL.Query().Get("search_word"))
		require.Equal(t, "Go", r.URL.Query().Get("search"))
		require.Equal(t, "1", cookieValue(r, "age_verified"))
		_, _ = fmt.Fprint(w, `<!doctype html><html><body>
<table class="tbllist actress"><tr><td><h2 class="ttl"><a href="/actress999.html">Wrong Name</a></h2></td></tr>
<tr><td><h2 class="ttl"><a href="/actress123.html">Example Name（えぐざんぷる / Example Name）</a></h2></td></tr></table>
</body></html>`)
	})
	mux.HandleFunc("/actress123.html", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "1", cookieValue(r, "age_verified"))
		_, _ = fmt.Fprint(w, profileFixture)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	p := testActor(server.URL, server.Client())

	results, err := p.SearchActor("Example Name")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "123", results[0].ID)
	require.Equal(t, "Example Name", results[0].Name)
	require.Equal(t, server.URL+"/actress123.html", results[0].Homepage)

	info, err := p.GetActorInfoByID("123")
	require.NoError(t, err)
	require.Equal(t, "123", info.ID)
	require.Equal(t, "Example Name", info.Name)
	require.Equal(t, ActorProviderName, info.Provider)
	require.Equal(t, server.URL+"/actress123.html", info.Homepage)
	require.Equal(t, []string{"えぐざんぷる", "Example Name", "Former Name", "ふぉーまー", "Former Name"}, []string(info.Aliases))
	require.Equal(t, 160, info.Height)
	require.Equal(t, "C", info.CupSize)
	require.Equal(t, "B:85 / W:58 / H:86", info.Measurements)
	require.Equal(t, []string{server.URL + "/images/actress123.jpg"}, []string(info.Images))
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func TestMinnanoActorRegistered(t *testing.T) {
	found := false
	mt.RangeActorFactory(func(name string, factory mt.ActorFactory) bool {
		if name == ActorProviderName {
			found = true
			require.Equal(t, ActorProviderName, factory().Name())
			return false
		}
		return true
	})
	require.True(t, found)
}
