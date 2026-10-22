package prestige

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

const (
	testUUID  = "11111111-2222-3333-4444-555555555555"
	testUUID2 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// These are synthetic schema fixtures written from the field contract observed
// in Amane. They are not captured site responses and do not contain real work
// metadata.
func productJSON(uuid, number, title string) string {
	return fmt.Sprintf(`{
		"uuid": %q,
		"title": %q,
		"mgsStartAt": "2024-05-16T10:00:00+09:00",
		"createdAt": "2024-05-01T00:00:00+09:00",
		"playTime": 120,
		"actress": [{"name":"Actor One"},{"name":"Actor Two"},{"name":"Actor One"}],
		"directors": [{"name":"Director One"},{"name":"Director Two"}],
		"genre": [{"name":"Drama"},{"name":"Drama"},{"name":"Romance"}],
		"maker": {"name":"Maker Name"},
		"label": [{"name":"Label Name"}],
		"series": "Series Name",
		"body": "Synthetic plot",
		"thumbnail": {"path":"front/cover.jpg"},
		"packageImage": {"path":"spread/cover.jpg"},
		"media": [{"path":"gallery/1.jpg"},{"path":"gallery/2.jpg"},{"path":"https://evil.invalid/x.jpg"}],
		"sku": [{"deliveryItemId":"GOOE%s"},{"deliveryItemId":"GOO%s"},{"deliveryItemId":%q}]
	}`, uuid, title, number, number, number)
}

func skuJSON(uuid string) string {
	return fmt.Sprintf(`{"parentProduct":{"uuid":%q}}`, uuid)
}

func TestSearchMovieFallsBackToCompactSKUAndMapsMetadata(t *testing.T) {
	mux := http.NewServeMux()
	var requests []string
	mux.HandleFunc("/api/sku/item/", func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		require.Equal(t, "application/json,text/plain,*/*", r.Header.Get("Accept"))
		switch r.URL.Path {
		case "/api/sku/item/ABW-350":
			http.NotFound(w, r)
		case "/api/sku/item/ABW350":
			_, _ = fmt.Fprint(w, skuJSON(testUUID))
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/product/"+testUUID, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, productJSON(testUUID, "ABW-350", "Synthetic Title"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("abw-350")
	require.NoError(t, err)
	require.Len(t, results, 1)
	result := results[0]
	require.Equal(t, "ABW-350", result.ID)
	require.Equal(t, "ABW-350", result.Number)
	require.Equal(t, "Synthetic Title", result.Title)
	require.Equal(t, Name, result.Provider)
	require.Equal(t, server.URL+"/api/product/"+testUUID, result.Homepage)
	require.Equal(t, server.URL+"/api/media/front/cover.jpg", result.CoverURL)
	require.Equal(t, server.URL+"/api/media/spread/cover.jpg", result.ThumbURL)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(result.Actors))
	require.Equal(t, "2024-05-16", time.Time(result.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, []string{"/api/sku/item/ABW-350", "/api/sku/item/ABW350"}, requests)

	info, err := p.GetMovieInfoByURL(result.Homepage)
	require.NoError(t, err)
	require.Equal(t, "ABW-350", info.ID)
	require.Equal(t, "プレステージ", info.Maker)
	require.Equal(t, "Label Name", info.Label)
	require.Equal(t, "Series Name", info.Series)
	require.Equal(t, []string{"Drama", "Romance"}, []string(info.Genres))
	require.Equal(t, "Director One, Director Two", info.Director)
	require.Equal(t, 120, info.Runtime)
	require.Equal(t, "Synthetic plot", info.Summary)
	require.Equal(t, []string{
		server.URL + "/api/media/gallery/1.jpg",
		server.URL + "/api/media/gallery/2.jpg",
	}, []string(info.PreviewImages))
	require.Equal(t, server.URL+"/api/media/front/cover.jpg", info.CoverURL)
}

func TestSearchMovieRejectsNonExactProductNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sku/item/ABW-350", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, skuJSON(testUUID))
	})
	mux.HandleFunc("/api/product/"+testUUID, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, productJSON(testUUID, "ABW-351", "Different work"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	results, err := newForTest(server.URL, server.Client()).SearchMovie("ABW-350")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearchMovieReturnsErrorWhenAllCandidatesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	results, err := newForTest(server.URL, server.Client()).SearchMovie("ABW-350")
	require.Error(t, err)
	require.Empty(t, results)
}

func TestParseMovieIDRejectsUnsafeProductURLs(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://www.prestige-av.com/api/product/" + testUUID,
		"https://www.prestige-av.com:444/api/product/" + testUUID,
		"https://user@www.prestige-av.com/api/product/" + testUUID,
		"https://www.prestige-av.com.evil.invalid/api/product/" + testUUID,
		"https://www.prestige-av.com/api/sku/item/ABW-350",
		"https://www.prestige-av.com/api/product/" + testUUID + "?x=1",
		"https://www.prestige-av.com/api/product/" + testUUID + "#fragment",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		require.Error(t, err, raw)
	}
	id, err := p.ParseMovieIDFromURL("https://www.prestige-av.com/api/product/" + testUUID)
	require.NoError(t, err)
	require.Equal(t, testUUID, id)
}

func TestGetMovieInfoByURLRejectsProductIdentityChange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/product/"+testUUID, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/product/"+testUUID2, http.StatusFound)
	})
	mux.HandleFunc("/api/product/"+testUUID2, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, productJSON(testUUID2, "ABW-350", "Other product"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByURL(server.URL + "/api/product/" + testUUID)
	require.Error(t, err)
	require.Nil(t, info)
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalHits int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalHits++
		_, _ = fmt.Fprint(w, skuJSON(testUUID))
	}))
	defer external.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/api/sku/item/ABW-350", http.StatusFound)
	}))
	defer source.Close()

	resp, err := newForTest(source.URL, source.Client()).Fetch(source.URL + "/api/sku/item/ABW-350")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, externalHits)
}

func TestGetMovieInfoByIDUsesExactSKULookup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sku/item/ABW-350", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, skuJSON(testUUID))
	})
	mux.HandleFunc("/api/product/"+testUUID, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, productJSON(testUUID, "ABW-350", "Synthetic Title"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	info, err := newForTest(server.URL, server.Client()).GetMovieInfoByID("ABW-350")
	require.NoError(t, err)
	require.Equal(t, "ABW-350", info.Number)
	require.Equal(t, server.URL+"/api/product/"+testUUID, info.Homepage)
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

func TestSKUCandidatesFollowAmaneVariants(t *testing.T) {
	candidates, err := skuCandidates("abw-350")
	require.NoError(t, err)
	require.Equal(t, []string{"ABW-350", "ABW350", "GOOEABW-350", "GOOEABW350"}, candidates)

	_, err = skuCandidates("ABW/350")
	require.Error(t, err)

	var payload productPayload
	require.NoError(t, json.Unmarshal([]byte(productJSON(testUUID, "ABW-350", "Title")), &payload))
	require.Equal(t, "ABW-350", payload.SKU[2].DeliveryItemID)
	require.True(t, strings.HasPrefix(payload.SKU[0].DeliveryItemID, "GOOE"))
	require.True(t, sameCatalogNumber("ABW-350", "GOOEABW-350"))
	require.False(t, sameCatalogNumber("ABW-350", "GOOEABW-351"))
}
