package route

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/engine"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

const (
	imageRouteSmallProvider = "MetaXImageRouteSmall"
	imageRouteLargeProvider = "MetaXImageRouteLarge"
)

var (
	imageRouteSmallCover string
	imageRouteLargeCover string
)

type imageRouteMovieProvider struct{ name string }

func (p *imageRouteMovieProvider) Name() string         { return p.name }
func (*imageRouteMovieProvider) Priority() float64      { return 1 }
func (*imageRouteMovieProvider) SetPriority(float64)    {}
func (*imageRouteMovieProvider) Language() language.Tag { return language.English }
func (p *imageRouteMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*imageRouteMovieProvider) NormalizeMovieID(id string) string           { return id }
func (*imageRouteMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (*imageRouteMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	return rawURL, nil
}
func (p *imageRouteMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	cover := imageRouteSmallCover
	if p.name == imageRouteLargeProvider {
		cover = imageRouteLargeCover
	}
	return &model.MovieInfo{
		ID: id, Number: "SSIS-001", Title: "Image route title",
		Provider: p.name, Homepage: "https://" + p.name + ".invalid/" + id,
		CoverURL: cover, BigCoverURL: cover,
	}, nil
}
func (p *imageRouteMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	return p.GetMovieInfoByID(rawURL)
}
func (p *imageRouteMovieProvider) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	return []*model.MovieSearchResult{
		{Provider: p.name, ID: "ssis00001", Number: "SSIS-001", Title: "Image route title",
			Homepage: "https://" + p.name + ".invalid/ssis00001"},
	}, nil
}

func init() {
	for _, name := range []string{imageRouteSmallProvider, imageRouteLargeProvider} {
		name := name
		mt.Register(name, func() *imageRouteMovieProvider {
			return &imageRouteMovieProvider{name: name}
		})
	}
}

func imageRouteServer(t *testing.T, width, height int) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 60, B: 60, A: 255})
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, img); err != nil {
			t.Errorf("encode image: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// The Jellyfin plugin never reads image URLs: it requests the proxy path with
// the merged identity. The preferred provider wins when it has usable artwork,
// and a provider that only serves a thumbnail must not win over a sharper one.
func TestImageEndpointServesTheSharpestAggregatedArtwork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	small := imageRouteServer(t, 120, 180)
	large := imageRouteServer(t, 800, 538)
	imageRouteSmallCover = small.URL + "/cover.png"
	imageRouteLargeCover = large.URL + "/cover.png"
	t.Cleanup(func() {
		imageRouteSmallCover = ""
		imageRouteLargeCover = ""
	})
	t.Setenv(engine.EnvMergeImageProvider, imageRouteSmallProvider)

	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {imageRouteLargeProvider, imageRouteSmallProvider},
	}}
	previous := routing.ActiveConfig()
	if err := routing.SetActiveConfig(config); err != nil {
		t.Fatalf("install route table: %v", err)
	}
	t.Cleanup(func() {
		if err := routing.SetActiveConfig(previous); err != nil {
			t.Errorf("restore route table: %v", err)
		}
	})

	router := gin.New()
	router.GET("/v1/images/primary/:provider/:id", getImage(engine.New(openEndpointAggregateDB(t)), primaryImageType))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/images/primary/MetaX/SSIS-001?category=censored", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	width, err := strconv.Atoi(recorder.Header().Get("X-MetaTube-Image-Width"))
	if err != nil {
		t.Fatalf("width header = %q: %v", recorder.Header().Get("X-MetaTube-Image-Width"), err)
	}
	if width < 300 {
		t.Fatalf("width = %d, want the 800px source to win over the 120px one", width)
	}
}
