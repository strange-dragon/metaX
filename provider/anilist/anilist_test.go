package anilist

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testProvider(t *testing.T) *AniList {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/subject/") {
			require.Equal(t, http.MethodGet, r.Method)
			_, _ = fmt.Fprint(w, `{"results":1,"list":[{"id":38432,"name":"Bible Black","name_cn":"黑暗圣经"}]}`)
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		if strings.Contains(request.Query, "Page(") {
			search, _ := request.Variables["search"].(string)
			if search == "黑暗圣经" {
				_, _ = fmt.Fprint(w, `{"data":{"Page":{"media":[]}}}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"data":{"Page":{"media":[{"id":368,"title":{"romaji":"Bible Black","native":"バイブルブラック"},"description":"Based on the erotic game.<br><br>Minase finds a book.","coverImage":{"extraLarge":"https://s4.anilist.co/cover/large.jpg","large":"https://s4.anilist.co/cover/large.jpg"},"startDate":{"year":2001,"month":7,"day":21},"duration":30,"averageScore":72,"genres":["Hentai","Supernatural"],"studios":{"edges":[{"node":{"name":"Studio Jam"}}]}}]}}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"Media":{"id":368,"title":{"romaji":"Bible Black","english":null,"native":"バイブルブラック"},"description":"Based on the erotic game.<br><br>Minase finds a book.","coverImage":{"extraLarge":"https://s4.anilist.co/cover/large.jpg","large":"https://s4.anilist.co/cover/large.jpg"},"startDate":{"year":2001,"month":7,"day":21},"duration":30,"averageScore":72,"genres":["Hentai","Supernatural"],"studios":{"edges":[{"node":{"name":"Studio Jam"}}]},"staff":{"edges":[{"role":"Director","node":{"name":{"full":"Yoshiten"}}}]},"characters":{"edges":[{"role":"MAIN","node":{"name":{"full":"Kurumi Imari"}},"voiceActors":[{"name":{"full":"Kaori Nishijima"}},{"name":{"full":"Kaori Nishijima"}}]}]}}}}`)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse("https://anilist.co/")
	return newWithClient(base, server.URL, server.URL+"/search/subject/", server.Client())
}

func TestSearchMovieMapsAniListResult(t *testing.T) {
	results, err := testProvider(t).SearchMovie("Bible Black")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "368", results[0].ID)
	assert.Equal(t, "ANILIST-368", results[0].Number)
	assert.Equal(t, "Bible Black", results[0].Title)
	assert.Equal(t, "https://anilist.co/anime/368", results[0].Homepage)
	assert.Equal(t, "https://s4.anilist.co/cover/large.jpg", results[0].CoverURL)
}

func TestSearchMovieResolvesChineseTitleThroughBangumi(t *testing.T) {
	results, err := testProvider(t).SearchMovie("黑暗圣经")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "368", results[0].ID)
	assert.Equal(t, "バイブルブラック", results[0].Title)
}

func TestGetMovieInfoByIDMapsStaffAndCast(t *testing.T) {
	info, err := testProvider(t).GetMovieInfoByID("ANILIST-368")
	require.NoError(t, err)
	require.True(t, info.IsValid())
	assert.Equal(t, "Bible Black", info.Title)
	assert.Equal(t, "Yoshiten", info.Director)
	assert.Equal(t, []string{"Kaori Nishijima"}, []string(info.Actors))
	assert.Equal(t, "Studio Jam", info.Maker)
	assert.Equal(t, 3.6, info.Score)
	assert.Equal(t, 30, info.Runtime)
	assert.Equal(t, 2001, time.Time(info.ReleaseDate).Year())
}

func TestParseMovieIDFromURLValidation(t *testing.T) {
	p := New()
	for _, raw := range []string{
		"https://anilist.co/anime/368",
		"https://anilist.co/anime/368/",
		"https://www.anilist.co/anime/368/Bible-Black",
		"https://anilist.co/anime/368?foo=bar",
	} {
		id, err := p.ParseMovieIDFromURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "368", id)
	}
	for _, raw := range []string{
		"http://anilist.co/anime/368",
		"https://anilist.co:444/anime/368",
		"https://anilist.co.evil.invalid/anime/368",
		"https://user@anilist.co/anime/368",
		"https://anilist.co/manga/368",
		"https://anilist.co/anime/abc",
	} {
		_, err := p.ParseMovieIDFromURL(raw)
		assert.Error(t, err, raw)
	}
}
