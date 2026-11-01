package engine

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

const errorShapeProvider = "MetaXErrorShape"

// errorShapeMovieProvider replays the three failure shapes a provider can hand
// back, so the engine's classification can be asserted without any network.
type errorShapeMovieProvider struct{ probe *errorShapeProbe }

type errorShapeProbe struct {
	info *model.MovieInfo
	err  error
}

func (*errorShapeMovieProvider) Name() string           { return errorShapeProvider }
func (*errorShapeMovieProvider) Priority() float64      { return 1 }
func (*errorShapeMovieProvider) SetPriority(float64)    {}
func (*errorShapeMovieProvider) Language() language.Tag { return language.English }
func (*errorShapeMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + errorShapeProvider + ".invalid")
	return u
}
func (*errorShapeMovieProvider) NormalizeMovieID(id string) string { return id }
func (*errorShapeMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	return rawURL, nil
}
func (p *errorShapeMovieProvider) GetMovieInfoByID(string) (*model.MovieInfo, error) {
	return p.probe.info, p.probe.err
}
func (p *errorShapeMovieProvider) GetMovieInfoByURL(string) (*model.MovieInfo, error) {
	return p.probe.info, p.probe.err
}

var activeErrorShapeProbe errorShapeProbe

func init() {
	mt.Register(errorShapeProvider, func() *errorShapeMovieProvider {
		return &errorShapeMovieProvider{probe: &activeErrorShapeProbe}
	})
}

func baseErrorShapeInfo() *model.MovieInfo {
	return &model.MovieInfo{
		ID: "ABP-123", Number: "ABP-123", Title: "Title",
		Provider: errorShapeProvider, Homepage: "https://" + errorShapeProvider + ".invalid/ABP-123",
		CoverURL: "https://" + errorShapeProvider + ".invalid/ABP-123.jpg",
	}
}

// TestGetMovieInfoByProviderIDClassifiesMissingRecords covers the two shapes a
// catalogue site uses for "no such work" and the one shape that is a real
// server-side problem. Before the classification existed, the first two
// answered 500, which told the client the server was broken and made it retry
// the same missing file on every scan.
func TestGetMovieInfoByProviderIDClassifiesMissingRecords(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
		want  error
	}{
		{
			// colly reports every non-2xx as errors.New(http.StatusText(code))
			// and providers return it verbatim.
			name:  "colly status text becomes not found",
			setup: func() { activeErrorShapeProbe = errorShapeProbe{err: errors.New(http.StatusText(http.StatusNotFound))} },
			want:  mt.ErrMovieNotFound,
		},
		{
			// A provider that ran to completion and returned nothing is how the
			// catalogue-backed sources spell "no such record".
			name:  "empty result becomes not found",
			setup: func() { activeErrorShapeProbe = errorShapeProbe{} },
			want:  mt.ErrMovieNotFound,
		},
		{
			// A record that parsed but lost every field is what a site rewrite
			// looks like. It must stay a 500: answering 404 would quietly make
			// the catalogue drop all of its titles.
			name: "unparsed record stays incomplete",
			setup: func() {
				activeErrorShapeProbe = errorShapeProbe{info: &model.MovieInfo{ID: "ABP-123", Provider: errorShapeProvider}}
			},
			want: mt.ErrIncompleteMetadata,
		},
		{
			// A record with a title but no artwork is a partial parse, not a
			// missing title: it stays visible as a 500 as well.
			name: "titled record without artwork stays incomplete",
			setup: func() {
				activeErrorShapeProbe = errorShapeProbe{info: &model.MovieInfo{ID: "ABP-123", Number: "ABP-123", Title: "Title", Provider: errorShapeProvider, Homepage: "https://x.invalid/"}}
			},
			want: mt.ErrIncompleteMetadata,
		},
		{
			name:  "usable record passes through",
			setup: func() { activeErrorShapeProbe = errorShapeProbe{info: baseErrorShapeInfo()} },
			want:  nil,
		},
		{
			// 403 means "blocked", not "missing": a client must not cache it.
			name: "blocked provider keeps its original error",
			setup: func() {
				activeErrorShapeProbe = errorShapeProbe{err: errors.New(http.StatusText(http.StatusForbidden))}
			},
			want: nil, // asserted separately below
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			t.Cleanup(func() { activeErrorShapeProbe = errorShapeProbe{} })
			engine := New(openAggregateTestDB(t))

			_, err := engine.GetMovieInfoByProviderID(providerid.ProviderID{Provider: errorShapeProvider, ID: "ABP-123"}, false)
			if tc.want == nil {
				if tc.name == "blocked provider keeps its original error" {
					if err == nil || err.Error() != http.StatusText(http.StatusForbidden) {
						t.Fatalf("err = %v, want the original Forbidden error", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
