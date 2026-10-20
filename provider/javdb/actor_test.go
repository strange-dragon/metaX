package javdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/stretchr/testify/require"
)

const actorDetailFixture = `<!doctype html><html><head><link rel="canonical" href="/actors/Actor123"></head><body>
<span class="actor-section-name">Example Name, 別名</span>
<span class="section-meta">Female</span>
<span class="section-meta">3 movie(s)</span>
<span class="section-meta">Other Alias</span>
<div class="actor-avatar"><span class="avatar" style="background-image: url('https://images.example.test/actor.jpg')"></span></div>
</body></html>`

func TestJavDBActorParseURLAndOriginValidation(t *testing.T) {
	p := NewActor()
	id, err := p.ParseActorIDFromURL("https://javdb.com/actors/AbC123")
	require.NoError(t, err)
	require.Equal(t, "AbC123", id)

	for _, raw := range []string{
		"https://example.invalid/actors/AbC123",
		"http://javdb.com/actors/AbC123",
		"https://javdb.com:8443/actors/AbC123",
		"https://user@javdb.com/actors/AbC123",
		"https://javdb.com/actors/AbC123#fragment",
		"https://javdb.com/actors/AbC123?next=/",
		"https://javdb.com/search?q=actor",
	} {
		_, err := p.ParseActorIDFromURL(raw)
		require.Error(t, err, raw)
	}
}

func TestJavDBActorSearchAndDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Example Name", r.URL.Query().Get("q"))
		require.Equal(t, "actor", r.URL.Query().Get("f"))
		require.Equal(t, "zh", r.URL.Query().Get("locale"))
		_, _ = fmt.Fprint(w, `<html><body>
<div class="actor-box"><a href="/actors/Other123" title="Unrelated Name"><strong>Unrelated Name</strong><img src="/other.jpg"></a></div>
<div class="actor-box"><a href="/actors/Actor123" title="Example Name, 別名"><strong>Example Name</strong><img data-src="/actor.jpg"></a></div>
</body></html>`)
	})
	mux.HandleFunc("/actors/Actor123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, actorDetailFixture)
	})
	mux.HandleFunc("/actors/Other123", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Replace(actorDetailFixture, "Example Name", "Unrelated Name", 1))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &JavDBActor{JavDB: newForTest(server.URL, nil)}
	results, err := p.SearchActor("Example Name")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "Actor123", results[0].ID)
	require.Equal(t, "Example Name", results[0].Name)
	require.Equal(t, server.URL+"/actor.jpg", results[0].Images[0])

	info, err := p.GetActorInfoByID("Actor123")
	require.NoError(t, err)
	require.Equal(t, "Actor123", info.ID)
	require.Equal(t, "Example Name", info.Name)
	require.Equal(t, ActorProviderName, info.Provider)
	require.Equal(t, []string{"別名", "Other Alias"}, []string(info.Aliases))
	require.Equal(t, []string{"https://images.example.test/actor.jpg"}, []string(info.Images))
}

func TestJavDBActorRegistered(t *testing.T) {
	found := false
	mt.RangeActorFactory(func(name string, factory mt.ActorFactory) bool {
		if name == ActorProviderName {
			found = true
			provider := factory()
			require.Equal(t, ActorProviderName, provider.Name())
			require.Equal(t, float64(Priority), provider.Priority())
			return false
		}
		return true
	})
	require.True(t, found, "JavDB actor provider should be registered")
}

func TestSplitActorNamesRemovesUncensoredMarkers(t *testing.T) {
	got := splitActorNames("Example Name (Uncensored), 葵（無碼）, 乙 (无码), Example Name")
	require.Equal(t, []string{"Example Name", "葵", "乙"}, got)
}
