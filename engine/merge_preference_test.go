package engine

import (
	"os"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

const (
	mergeTestDetailProvider = "MetaXMergeDetailTest"
	mergeTestImageProvider  = "MetaXMergeImageTest"
)

func init() {
	for _, name := range []string{mergeTestDetailProvider, mergeTestImageProvider} {
		name := name
		mt.Register(name, func() *aggregateTestMovieProvider {
			return &aggregateTestMovieProvider{name: name}
		})
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	previous, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestActiveMergePreferenceDefaultsToJav321ArtworkAndJavDBMetadata(t *testing.T) {
	unsetEnvForTest(t, EnvMergeImageProvider)
	unsetEnvForTest(t, EnvMergeDetailProvider)

	got := ActiveMergePreference()
	if got.Image != "Jav321" || got.Detail != "JavDB" {
		t.Fatalf("defaults = %+v, want image Jav321 and detail JavDB", got)
	}
}

func TestActiveMergePreferenceAcceptsOverridesAndOff(t *testing.T) {
	cases := []struct {
		name       string
		image      string
		detail     string
		wantImage  string
		wantDetail string
	}{
		{name: "explicit providers", image: "JavBus", detail: "JavDb", wantImage: "JavBus", wantDetail: "JavDb"},
		{name: "route keyword disables", image: "route", detail: "default", wantImage: "", wantDetail: ""},
		{name: "off keyword disables", image: "off", detail: "none", wantImage: "", wantDetail: ""},
		{name: "empty value disables", image: "", detail: "", wantImage: "", wantDetail: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvMergeImageProvider, tc.image)
			t.Setenv(EnvMergeDetailProvider, tc.detail)

			got := ActiveMergePreference()
			if got.Image != tc.wantImage || got.Detail != tc.wantDetail {
				t.Fatalf("preference = %+v, want image %q detail %q", got, tc.wantImage, tc.wantDetail)
			}
		})
	}
}

func TestMergeMovieInfoWithPreferenceSplitsArtworkFromMetadata(t *testing.T) {
	identity := &model.MovieInfo{
		ID: "javdb-id", Provider: "JavDB", Number: "SSIS-001",
		Title: "JavDB title", Summary: "JavDB summary", Homepage: "https://javdb.invalid/movie/1",
		CoverURL: "https://javdb.invalid/cover.jpg",
	}
	routeOrder := []*model.MovieInfo{
		{
			ID: "fanza-id", Provider: "FANZA", Number: "SSIS-001",
			Title: "FANZA title", Summary: "FANZA summary",
			CoverURL: "https://fanza.invalid/cover.jpg", ThumbURL: "https://fanza.invalid/thumb.jpg",
			PreviewImages: pq.StringArray{"https://fanza.invalid/1.jpg"},
		},
		identity,
		{
			ID: "jav321-id", Provider: "Jav321", Number: "SSIS-001",
			Title: "Jav321 title", Summary: "Jav321 summary", Maker: "Jav321 maker",
			CoverURL: "https://jav321.invalid/cover.jpg", ThumbURL: "https://jav321.invalid/thumb.jpg",
			PreviewImages: pq.StringArray{"https://jav321.invalid/1.jpg", "https://jav321.invalid/2.jpg"},
		},
	}

	got := mergeMovieInfoWithPreference(identity, routeOrder, MergePreference{
		Image: "Jav321", Detail: "JavDB",
	})

	// Metadata follows the detail preference even though another provider
	// supplies the same field earlier in route order.
	if got.Title != "JavDB title" || got.Summary != "JavDB summary" {
		t.Fatalf("metadata = title %q summary %q, want JavDB", got.Title, got.Summary)
	}
	// Fields the preferred provider does not carry still fall back to route
	// order instead of staying empty.
	if got.Maker != "Jav321 maker" {
		t.Fatalf("maker = %q, want the only provider that carries one", got.Maker)
	}
	// Artwork follows the image preference.
	if got.CoverURL != "https://jav321.invalid/cover.jpg" || got.ThumbURL != "https://jav321.invalid/thumb.jpg" {
		t.Fatalf("artwork = %q / %q, want Jav321", got.CoverURL, got.ThumbURL)
	}
	if len(got.PreviewImages) != 3 ||
		got.PreviewImages[0] != "https://jav321.invalid/1.jpg" ||
		got.PreviewImages[1] != "https://jav321.invalid/2.jpg" ||
		got.PreviewImages[2] != "https://fanza.invalid/1.jpg" {
		t.Fatalf("preview images = %#v, want Jav321 first then route order", got.PreviewImages)
	}
	// Identity and catalog number are unaffected by the preferences.
	if got.ID != identity.ID || got.Provider != identity.Provider || got.Number != identity.Number {
		t.Fatalf("identity changed: %s/%s/%s", got.Provider, got.ID, got.Number)
	}
}

func TestMergeMovieInfoWithPreferenceFallsBackToRouteOrder(t *testing.T) {
	identity := &model.MovieInfo{
		ID: "javdb-id", Provider: "JavDB", Number: "SSIS-001",
		Title: "JavDB title", CoverURL: "https://javdb.invalid/cover.jpg",
	}
	routeOrder := []*model.MovieInfo{
		{
			Provider: "FANZA", Number: "SSIS-001",
			CoverURL: "https://fanza.invalid/cover.jpg", ThumbURL: "https://fanza.invalid/thumb.jpg",
		},
		identity,
	}

	// Jav321 did not answer for this title, so both passes must fall back to
	// route order instead of leaving the fields empty.
	got := mergeMovieInfoWithPreference(identity, routeOrder, MergePreference{
		Image: "Jav321", Detail: "Jav321",
	})
	if got.CoverURL != "https://fanza.invalid/cover.jpg" || got.ThumbURL != "https://fanza.invalid/thumb.jpg" {
		t.Fatalf("artwork = %q / %q, want route order fallback", got.CoverURL, got.ThumbURL)
	}
	if got.Title != "JavDB title" {
		t.Fatalf("title = %q, want route order fallback", got.Title)
	}
}

func TestResolveAggregatedProviderIDPrefersImageProvider(t *testing.T) {
	resetAggregateCaches(time.Minute)
	e := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {mergeTestDetailProvider, mergeTestImageProvider},
	}}
	t.Setenv(EnvMergeImageProvider, mergeTestImageProvider)

	resolved, err := e.ResolveAggregatedProviderID("FC2-PPV-123456", routing.CategoryUncensored, config)
	if err != nil {
		t.Fatalf("resolve aggregated provider: %v", err)
	}
	if resolved.Provider != mergeTestImageProvider {
		t.Fatalf("resolved provider = %q, want the image preference %q", resolved.Provider, mergeTestImageProvider)
	}
}

func TestResolveAggregatedProviderIDKeepsRouteOrderWhenPreferenceOff(t *testing.T) {
	resetAggregateCaches(time.Minute)
	e := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {mergeTestDetailProvider, mergeTestImageProvider},
	}}
	t.Setenv(EnvMergeImageProvider, "off")

	resolved, err := e.ResolveAggregatedProviderID("FC2-PPV-123456", routing.CategoryUncensored, config)
	if err != nil {
		t.Fatalf("resolve aggregated provider: %v", err)
	}
	if resolved.Provider != mergeTestDetailProvider {
		t.Fatalf("resolved provider = %q, want route order %q", resolved.Provider, mergeTestDetailProvider)
	}
}
