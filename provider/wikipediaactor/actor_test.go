package wikipediaactor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lib/pq"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

const wikiHTML = `<!doctype html><html><body><div class="mw-parser-output">
<p>Example Name is an example subject used only by deterministic tests.[1]</p>
<h2><span class="mw-headline" id="Early_life">人物</span></h2>
<p>She began performing in 2010 and is known for example roles.[注 1]</p>
<h2>作品</h2>
<p>This work list must not appear in the biography.</p>
</div></body></html>`

func entityJSON(id, label, description, alias string) string {
	return fmt.Sprintf(`{
  "entities": {
    %q: {
      "labels": {
        "ja": {"language":"ja","value":%q},
        "zh": {"language":"zh","value":"中文名"},
        "en": {"language":"en","value":%q}
      },
      "aliases": {"ja":[{"language":"ja","value":%q}]},
      "descriptions": {"en":{"language":"en","value":%q}},
      "claims": {
        "P106": [{"mainsnak":{"datavalue":{"value":{"id":"Q1079215"}}}}],
        "P569": [{"mainsnak":{"datavalue":{"value":{"time":"+1990-01-02T00:00:00Z","precision":11}}}}],
        "P18": [{"mainsnak":{"datavalue":{"value":"Example Name.jpg"}}}]
      },
      "sitelinks": {"jawiki":{"site":"jawiki","title":"Example Name"}}
    }
  }
}`, id, label, label, alias, description)
}

func testActor(t *testing.T, handler http.Handler) *Actor {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newForTest(server.URL+"/wd", map[string]string{
		"ja": server.URL + "/ja",
		"zh": server.URL + "/zh",
		"en": server.URL + "/en",
	}, server.Client())
}

func TestSearchAndProfileFromWikidataAndWikipedia(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wd/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "wbsearchentities", r.URL.Query().Get("action"))
		require.Equal(t, "Alias Name", r.URL.Query().Get("search"))
		_, _ = fmt.Fprint(w, `{"search":[{"id":"Q123","description":"Japanese AV actress"},{"id":"Q999","description":"Japanese politician"}]}`)
	})
	mux.HandleFunc("/wd/wiki/Special:EntityData/Q123.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, entityJSON("Q123", "Example Name", "Japanese AV actress", "Alias Name"))
	})
	mux.HandleFunc("/wd/wiki/Special:EntityData/Q999.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"entities":{"Q999":{"labels":{"ja":{"value":"Example Name"}},"descriptions":{"en":{"value":"Japanese politician"}}}}}`)
	})
	mux.HandleFunc("/ja/wiki/Example_Name", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, wikiHTML)
	})
	actor := testActor(t, mux)

	results, err := actor.SearchActor("Alias Name")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "Q123", results[0].ID)
	require.Equal(t, "Example Name", results[0].Name)
	require.Equal(t, ActorProviderName, results[0].Provider)
	require.Equal(t, actor.actorURL("Q123"), results[0].Homepage)
	require.Equal(t, pq.StringArray{"中文名"}, results[0].Aliases)
	require.Equal(t, pq.StringArray{"https://commons.wikimedia.org/wiki/Special:FilePath/Example%20Name.jpg"}, results[0].Images)

	info, err := actor.GetActorInfoByID("q123")
	require.NoError(t, err)
	require.Equal(t, "Q123", info.ID)
	require.Equal(t, "Example Name", info.Name)
	require.Equal(t, "1990-01-02", time.Time(info.Birthday).Format("2006-01-02"))
	require.Equal(t, "Example Name is an example subject used only by deterministic tests.\n\nShe began performing in 2010 and is known for example roles.", info.Summary)
	require.NotContains(t, info.Summary, "work list")
}

func TestSearchRejectsNonAVAndDifferentName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wd/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"search":[{"id":"Q200","description":"AV actress"},{"id":"Q201","description":"AV actress"}]}`)
	})
	mux.HandleFunc("/wd/wiki/Special:EntityData/Q200.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"entities":{"Q200":{"labels":{"ja":{"value":"Example Name"}},"descriptions":{"en":{"value":"Japanese politician"}}}}}`)
	})
	mux.HandleFunc("/wd/wiki/Special:EntityData/Q201.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, entityJSON("Q201", "Other Name", "Japanese AV actress", "Different Alias"))
	})
	actor := testActor(t, mux)

	results, err := actor.SearchActor("Example Name")
	require.NoError(t, err)
	require.Empty(t, results)

	_, err = actor.GetActorInfoByID("Q200")
	require.ErrorIs(t, err, ErrActorNotFound)
}

func TestParseActorURLValidation(t *testing.T) {
	p := New()
	id, err := p.ParseActorIDFromURL("https://www.wikidata.org/wiki/Q123")
	require.NoError(t, err)
	require.Equal(t, "Q123", id)

	for _, raw := range []string{
		"https://example.invalid/wiki/Q123",
		"http://www.wikidata.org/wiki/Q123",
		"https://www.wikidata.org:8443/wiki/Q123",
		"https://user@www.wikidata.org/wiki/Q123",
		"https://www.wikidata.org/wiki/Q123?x=1",
		"https://www.wikidata.org/wiki/Q123#fragment",
		"https://www.wikidata.org/wiki/Example",
		"https://www.wikidata.org/other/Q123",
	} {
		_, err := p.ParseActorIDFromURL(raw)
		require.Error(t, err, raw)
	}

	for _, id := range []string{"", "Q", "Qabc", "123"} {
		_, err := p.GetActorInfoByID(id)
		require.Error(t, err, id)
	}
}

func TestMalformedSearchResponseIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wd/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{not json`)
	})
	actor := testActor(t, mux)

	_, err := actor.SearchActor("Example Name")
	require.Error(t, err)
}

func TestProviderConfigurationAndRegistration(t *testing.T) {
	p := New()
	require.Equal(t, ActorProviderName, p.Name())
	require.Equal(t, float64(Priority), p.Priority())
	p.SetPriority(42)
	require.Equal(t, float64(42), p.Priority())
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
	require.Error(t, p.SetProxy("ftp://127.0.0.1:21"))
	require.NoError(t, p.SetProxy("http://127.0.0.1:8080"))
	require.NoError(t, p.SetProxy(""))

	found := false
	mt.RangeActorFactory(func(name string, factory mt.ActorFactory) bool {
		if name != ActorProviderName {
			return true
		}
		found = true
		require.Equal(t, ActorProviderName, factory().Name())
		return false
	})
	require.True(t, found)
}

func TestWikiPageBirthdayFallbackAndURLBase(t *testing.T) {
	body := []byte(`<!doctype html><html><body><div class="mw-parser-output">
<table class="infobox"><tr><th>生年月日</th><td>1991年2月3日</td></tr></table>
<p>Intro.</p></div></body></html>`)
	page, err := parseWikiPage(body)
	require.NoError(t, err)
	require.Equal(t, "1991-02-03", page.birthday)
	require.Equal(t, "Intro.", page.overview)

	u, err := url.Parse("https://www.wikidata.org/wiki/Q1")
	require.NoError(t, err)
	require.True(t, New().validOrigin(u, New().wikidataURL))
}
