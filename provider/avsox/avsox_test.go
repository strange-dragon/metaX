package avsox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mt "github.com/strange-dragon/metaX/provider"
	"github.com/stretchr/testify/require"
)

const csrfPage = `<!doctype html><html><head><meta name="csrf-token" content="csrf-test"></head></html>`

func apiServer(t *testing.T, searchData, detailData string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "avsox_session", Value: "session-test", Path: "/"})
		_, _ = fmt.Fprint(w, csrfPage)
	})
	mux.HandleFunc("/javu/data/api/search", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		cookie, err := r.Cookie("avsox_session")
		require.NoError(t, err, "session cookie from the CSRF bootstrap must be retained")
		require.Equal(t, "session-test", cookie.Value)
		require.Equal(t, "csrf-test", r.Header.Get("X-CSRF-Token"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, searchData)
	})
	mux.HandleFunc("/javu/data/api/getMovie", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "csrf-test", r.Header.Get("X-CSRF-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, detailData)
	})
	return httptest.NewServer(mux)
}

func TestSearchAndDetailParsing(t *testing.T) {
	search := `{"code":200,"data":[
		{"movieId":"movie1","movieFanHao":"ABC-1234","title":"Near match"},
		{"movieId":"movie2","movieFanHao":"abc123","title":"Exact title","posterSmall":"/poster.jpg","posterLarge":"/thumb.jpg"}
	]}`
	detail := `{"code":200,"data":{
		"movieId":"movie2","movieFanHao":"ABC-123","title":"Exact title",
		"releaseDate":"2024-05-16","length":121,"description_cn":"Plot text",
		"posterSmall":"/poster.jpg","posterLarge":"/thumb.jpg","sampleLarge":["/sample/1.jpg",""],
		"studio":{"studioName":"Studio One"},
		"star":[{"starName":"Actor One"},{"starName_ja":"Actor Two"},{"starName":"Actor One"}],
		"genre":[{"genreName_cn":"Drama"},{"genreName":"Drama"}],
		"director":{"directorName":"Director One"},
		"label":{"labelName":"Label One"},"series":{"seriesName":"Series One"}
	}}`
	server := apiServer(t, search, detail)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "movie2", results[0].ID)
	require.Equal(t, "abc123", results[0].Number)
	require.Equal(t, "Exact title", results[0].Title)
	require.Equal(t, server.URL+"/cn/movies/movie2", results[0].Homepage)

	info, err := p.GetMovieInfoByURL(results[0].Homepage)
	require.NoError(t, err)
	require.Equal(t, "movie2", info.ID)
	require.Equal(t, "ABC-123", info.Number)
	require.Equal(t, "Exact title", info.Title)
	require.Equal(t, server.URL+"/poster.jpg", info.CoverURL)
	require.Equal(t, server.URL+"/thumb.jpg", info.ThumbURL)
	require.Equal(t, []string{"Actor One", "Actor Two"}, []string(info.Actors))
	require.Equal(t, []string{"Drama"}, []string(info.Genres))
	require.Equal(t, "Director One", info.Director)
	require.Equal(t, "Studio One", info.Maker)
	require.Equal(t, "Label One", info.Label)
	require.Equal(t, "Series One", info.Series)
	require.Equal(t, "Plot text", info.Summary)
	require.Equal(t, 121, info.Runtime)
	require.Equal(t, []string{server.URL + "/sample/1.jpg"}, []string(info.PreviewImages))
	require.Equal(t, "2024-05-16", time.Time(info.ReleaseDate).Format("2006-01-02"))
}

func TestSearchRejectsNearAndUnderscoreVariant(t *testing.T) {
	search := `{"code":200,"data":[{"movieId":"m1","movieFanHao":"ABC-1234","title":"Wrong"}]}`
	detail := `{"code":200,"data":{}}`
	server := apiServer(t, search, detail)
	defer server.Close()
	p := newForTest(server.URL, server.Client())

	results, err := p.SearchMovie("ABC-123")
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = p.SearchMovie("ABC_123")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestDetailRejectsMismatchedAPIID(t *testing.T) {
	search := `{"code":200,"data":[]}`
	detail := `{"code":200,"data":{"movieId":"other","movieFanHao":"ABC-123","title":"Wrong","posterSmall":"/poster.jpg"}}`
	server := apiServer(t, search, detail)
	defer server.Close()
	p := newForTest(server.URL, server.Client())

	info, err := p.GetMovieInfoByURL(server.URL + "/cn/movies/requested")
	require.ErrorIs(t, err, ErrMovieNotFound)
	require.Nil(t, info)
}

func TestParseMovieIDFromURLRejectsUnsafeOriginsAndPaths(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"http://avsox.click/cn/movies/123",
		"https://avsox.click:444/cn/movies/123",
		"https://user@avsox.click/cn/movies/123",
		"https://avsox.click.evil.invalid/cn/movies/123",
		"https://avsox.click/cn/search/123",
		"https://avsox.click/cn/movies/has-dash",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := p.ParseMovieIDFromURL(raw)
			require.Error(t, err)
		})
	}
}

func TestFetchRejectsCrossOriginRedirect(t *testing.T) {
	var externalRequests int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalRequests++
		_, _ = fmt.Fprint(w, csrfPage)
	}))
	defer external.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/cn/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/stolen", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	resp, err := p.Fetch(server.URL + "/cn/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, externalRequests)
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
	require.True(t, found, "AVSOX should register with metaX's Provider factory")

	p := New()
	require.Error(t, p.SetProxy("invalid"))
	require.NoError(t, p.SetProxy(""))
	p.SetRequestTimeout(3 * time.Second)
	require.Equal(t, 3*time.Second, p.httpClient().Timeout)
}

func TestEnvelopeDecoding(t *testing.T) {
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"code":200,"data":[]}`), &envelope))
	require.Equal(t, 200, envelope.Code)
}

func TestFetchAllowsOnlyKnownAssetOrigin(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}
	p := newForTest("https://avsox.click", client)

	for _, raw := range []string{
		"https://file.netcdn.space/storage/example.jpg",
		"https://file.netcdn.space:443/storage/example.jpg",
	} {
		resp, err := p.Fetch(raw)
		require.NoError(t, err, raw)
		resp.Body.Close()
	}

	for _, raw := range []string{
		"http://file.netcdn.space/storage/example.jpg",
		"https://file.netcdn.space:444/storage/example.jpg",
		"https://file.netcdn.space.evil.invalid/storage/example.jpg",
		"https://other.netcdn.space/storage/example.jpg",
		"https://user@file.netcdn.space/storage/example.jpg",
	} {
		_, err := p.Fetch(raw)
		require.Error(t, err, raw)
	}

	// Asset origins are only valid for Fetch. Detail parsing must stay on the
	// AVSOX site origin.
	_, err := p.ParseMovieIDFromURL("https://file.netcdn.space/movies/movie2")
	require.Error(t, err)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
