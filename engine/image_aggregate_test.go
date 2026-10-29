package engine

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

const (
	imageAggregateSmallProvider   = "MetaXImageAggregateSmall"
	imageAggregateLargeProvider   = "MetaXImageAggregateLarge"
	imageAggregateMissingProvider = "MetaXImageAggregateMissing"
)

// Covers are served from an httptest server, so these hold the URL handed out
// by the provider that claims to own them.
var (
	imageAggregateSmallCover   string
	imageAggregateLargeCover   string
	imageAggregateLargeFetches atomic.Int32
)

type imageAggregateMovieProvider struct{ name string }

func (p *imageAggregateMovieProvider) Name() string         { return p.name }
func (*imageAggregateMovieProvider) Priority() float64      { return 1 }
func (*imageAggregateMovieProvider) SetPriority(float64)    {}
func (*imageAggregateMovieProvider) Language() language.Tag { return language.English }
func (p *imageAggregateMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*imageAggregateMovieProvider) NormalizeMovieID(id string) string { return id }
func (*imageAggregateMovieProvider) NormalizeMovieKeyword(keyword string) string {
	return keyword
}
func (*imageAggregateMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	return rawURL, nil
}
func (p *imageAggregateMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	switch p.name {
	case imageAggregateSmallProvider:
		return imageAggregateInfo(p.name, id, imageAggregateSmallCover), nil
	case imageAggregateLargeProvider:
		return imageAggregateInfo(p.name, id, imageAggregateLargeCover), nil
	}
	// The provider answers the search but has no detail: the image chain must
	// skip it instead of failing the request.
	return nil, mt.ErrInfoNotFound
}
func (p *imageAggregateMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	return p.GetMovieInfoByID(rawURL)
}
func (p *imageAggregateMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	return []*model.MovieSearchResult{imageAggregateInfo(p.name, "abp-123", "").ToSearchResult()}, nil
}

func imageAggregateInfo(provider, id, cover string) *model.MovieInfo {
	return &model.MovieInfo{
		ID: id, Number: "ABP-123", Title: "Image title",
		Provider: provider, Homepage: "https://" + provider + ".invalid/" + id,
		CoverURL: cover, BigCoverURL: cover,
	}
}

func init() {
	for _, name := range []string{
		imageAggregateSmallProvider,
		imageAggregateLargeProvider,
		imageAggregateMissingProvider,
	} {
		name := name
		mt.Register(name, func() *imageAggregateMovieProvider {
			return &imageAggregateMovieProvider{name: name}
		})
	}
}

// imageServer serves a solid PNG of the requested size and counts requests.
func imageServer(t *testing.T, width, height int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 20, G: 90, B: 160, A: 255})
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, img); err != nil {
			t.Errorf("encode image: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGetAggregatedMovieImagePicksTheSharpestCandidate(t *testing.T) {
	small := imageServer(t, 120, 180, nil)
	large := imageServer(t, 800, 538, nil)
	imageAggregateSmallCover = small.URL + "/cover.png"
	imageAggregateLargeCover = large.URL + "/cover.png"
	t.Cleanup(func() {
		imageAggregateSmallCover = ""
		imageAggregateLargeCover = ""
	})

	e := New(openAggregateTestDB(t))
	got, err := e.GetAggregatedMovieImage([]providerid.ProviderID{
		{Provider: imageAggregateSmallProvider, ID: "abp-image-1"},
		{Provider: imageAggregateLargeProvider, ID: "abp-image-1"},
	}, MovieImagePrimary, -1, -1)
	if err != nil {
		t.Fatalf("aggregated image: %v", err)
	}
	bounds := got.Bounds()
	if bounds.Dx() < 300 {
		t.Fatalf("width = %d, want the 800px source to win over the 120px one", bounds.Dx())
	}
}

func TestGetAggregatedMovieImageStopsAtTheFirstSharpEnoughCandidate(t *testing.T) {
	imageAggregateLargeFetches.Store(0)
	large := imageServer(t, 800, 538, &imageAggregateLargeFetches)
	small := imageServer(t, 120, 180, nil)
	imageAggregateLargeCover = large.URL + "/cover.png"
	imageAggregateSmallCover = small.URL + "/cover.png"
	t.Cleanup(func() {
		imageAggregateSmallCover = ""
		imageAggregateLargeCover = ""
	})

	e := New(openAggregateTestDB(t))
	if _, err := e.GetAggregatedMovieImage([]providerid.ProviderID{
		{Provider: imageAggregateLargeProvider, ID: "abp-image-2"},
		{Provider: imageAggregateSmallProvider, ID: "abp-image-2"},
	}, MovieImagePrimary, -1, -1); err != nil {
		t.Fatalf("aggregated image: %v", err)
	}
	if got := imageAggregateLargeFetches.Load(); got != 1 {
		t.Fatalf("sharp candidate was downloaded %d times, want exactly 1 (no needless fallback)", got)
	}
}

func TestGetAggregatedMovieImageSkipsCandidatesWithoutArtwork(t *testing.T) {
	large := imageServer(t, 800, 538, nil)
	imageAggregateLargeCover = large.URL + "/cover.png"
	t.Cleanup(func() { imageAggregateLargeCover = "" })

	e := New(openAggregateTestDB(t))
	got, err := e.GetAggregatedMovieImage([]providerid.ProviderID{
		{Provider: imageAggregateMissingProvider, ID: "abp-image-3"},
		{Provider: imageAggregateLargeProvider, ID: "abp-image-3"},
	}, MovieImagePrimary, -1, -1)
	if err != nil {
		t.Fatalf("aggregated image: %v", err)
	}
	if got.Bounds().Dx() < 300 {
		t.Fatalf("width = %d, want the remaining provider's artwork", got.Bounds().Dx())
	}
}

func TestGetAggregatedMovieImageReportsThePreferredProvidersFailure(t *testing.T) {
	e := New(openAggregateTestDB(t))
	_, err := e.GetAggregatedMovieImage([]providerid.ProviderID{
		{Provider: imageAggregateMissingProvider, ID: "abp-image-4"},
		{Provider: "NotARegisteredProvider", ID: "abp-image-4"},
	}, MovieImagePrimary, -1, -1)
	if err == nil {
		t.Fatal("expected an error when no candidate can serve artwork")
	}
	// The first candidate is the one the caller expected artwork from, so its
	// error has to survive instead of the last nothing-provider's.
	if !strings.Contains(err.Error(), imageAggregateMissingProvider) {
		t.Fatalf("error = %v, want it to name the preferred provider", err)
	}
}
