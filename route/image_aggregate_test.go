package route

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

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

// openImageRouteDB gives every image test its own in-memory database. The
// package helper shares one DSN, so a row cached by an earlier test — with the
// artwork URL of a test server that has since been closed — would be served to
// the next test and fail there for reasons that have nothing to do with it.
func openImageRouteDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:metax-image-route-" +
		strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()) +
		"?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	if err := db.AutoMigrate(&model.MovieInfo{}); err != nil {
		t.Fatalf("migrate movie metadata table: %v", err)
	}
	return db
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
	router.GET("/v1/images/primary/:provider/:id", getImage(engine.New(openImageRouteDB(t)), primaryImageType))

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

// The plugin sends the merged cover_url as ?url=. When that host is unreachable
// from this deployment — JavDB's image CDN answers 403 to datacenter IPs, which
// is why Jav321 is the default artwork source — the endpoint must still serve
// the chain's own artwork instead of failing the image outright.
func TestImageEndpointFallsBackWhenTheSuppliedURLCannotBeFetched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	large := imageRouteServer(t, 800, 538)
	imageRouteSmallCover = large.URL + "/cover.png"
	imageRouteLargeCover = large.URL + "/cover.png"
	t.Cleanup(func() {
		imageRouteSmallCover = ""
		imageRouteLargeCover = ""
	})
	t.Setenv(engine.EnvMergeImageProvider, imageRouteLargeProvider)

	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(blocked.Close)

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
	router.GET("/v1/images/primary/:provider/:id", getImage(engine.New(openImageRouteDB(t)), primaryImageType))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/images/primary/MetaX/SSIS-001?category=censored&url="+url.QueryEscape(blocked.URL+"/cover.png"), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	width, err := strconv.Atoi(recorder.Header().Get("X-MetaTube-Image-Width"))
	if err != nil {
		t.Fatalf("width header = %q: %v", recorder.Header().Get("X-MetaTube-Image-Width"), err)
	}
	if width == 0 {
		t.Fatal("fallback artwork has no width")
	}
}

// Cached metadata keeps the artwork URL the provider served at scrape time.
// When that URL dies — a moved CDN, or a host that answers 403 to datacenter
// IPs — the endpoint must ask the provider again instead of replaying the
// stored failure until the row ages out.
func TestImageEndpointRefreshesStaleCachedArtwork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	live := imageRouteServer(t, 800, 538)
	imageRouteSmallCover = live.URL + "/cover.png"
	imageRouteLargeCover = live.URL + "/cover.png"
	t.Cleanup(func() {
		imageRouteSmallCover = ""
		imageRouteLargeCover = ""
	})

	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(stale.Close)

	db := openImageRouteDB(t)
	if err := db.Create(&model.MovieInfo{
		ID:          "ssis00001",
		Number:      "SSIS-001",
		Title:       "Image route title",
		Provider:    imageRouteSmallProvider,
		Homepage:    "https://" + imageRouteSmallProvider + ".invalid/ssis00001",
		CoverURL:    stale.URL + "/cover.png",
		BigCoverURL: stale.URL + "/cover.png",
	}).Error; err != nil {
		t.Fatalf("seed cached metadata: %v", err)
	}

	router := gin.New()
	router.GET("/v1/images/primary/:provider/:id", getImage(engine.New(db), primaryImageType))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/images/primary/"+imageRouteSmallProvider+"/ssis00001", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if width := recorder.Header().Get("X-MetaTube-Image-Width"); width == "" {
		t.Fatal("refreshed artwork has no width")
	}
}
