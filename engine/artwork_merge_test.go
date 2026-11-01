package engine

import (
	"testing"

	"github.com/strange-dragon/metaX/model"
)

// TestMergePrefersArtworkFromASourceThatHasALargeVariant covers the case that
// motivated the reorder: the preferred image source publishes one 356 px
// thumbnail, and the next source holds the same work at full size. Merging used
// to take the first non-empty cover in preference order, so the thumbnail became
// the cover of a record that had a better image available.
func TestMergePrefersArtworkFromASourceThatHasALargeVariant(t *testing.T) {
	identity := &model.MovieInfo{
		ID: "thumb-only-1", Provider: "ThumbOnly", Number: "ABP-123",
		Title: "Title", Homepage: "https://thumb.example/movie/1",
		CoverURL:    "https://thumb.example/356x356.jpg",
		BigCoverURL: "https://thumb.example/356x356.jpg", // nothing better upstream
	}
	rich := &model.MovieInfo{
		ID: "rich-1", Provider: "Rich", Number: "ABP-123",
		CoverURL:    "https://rich.example/cover-800.jpg",
		BigCoverURL: "https://rich.example/cover-1600.jpg",
	}

	got := mergeMovieInfoWithPreference(identity, []*model.MovieInfo{identity, rich}, MergePreference{Image: "ThumbOnly"})
	if got.CoverURL != rich.CoverURL {
		t.Fatalf("cover = %q, want the source with a distinct large variant (%q)", got.CoverURL, rich.CoverURL)
	}
	if got.BigCoverURL != rich.BigCoverURL {
		t.Fatalf("big cover = %q, want %q", got.BigCoverURL, rich.BigCoverURL)
	}
}

// TestMergeKeepsPreferenceOrderWhenNoSourceHasALargerCopy is the guard on the
// reorder: when every candidate only has one image there is no quality signal,
// so the configured preference must still decide.
func TestMergeKeepsPreferenceOrderWhenNoSourceHasALargerCopy(t *testing.T) {
	preferred := &model.MovieInfo{
		ID: "a-1", Provider: "Preferred", Number: "ABP-123", Title: "A",
		Homepage:    "https://preferred.example/movie/1",
		CoverURL:    "https://preferred.example/cover.jpg",
		BigCoverURL: "https://preferred.example/cover.jpg",
	}
	other := &model.MovieInfo{
		ID: "b-1", Provider: "Other", Number: "ABP-123",
		CoverURL:    "https://other.example/cover.jpg",
		BigCoverURL: "https://other.example/cover.jpg",
	}

	got := mergeMovieInfoWithPreference(preferred, []*model.MovieInfo{other, preferred}, MergePreference{Image: "Preferred"})
	if got.CoverURL != preferred.CoverURL {
		t.Fatalf("cover = %q, want the preferred source %q", got.CoverURL, preferred.CoverURL)
	}
}

// TestMergeKeepsPreferredSourceWhenItAlsoHasALargeVariant makes sure the reorder
// never demotes a source that already satisfies both rules.
func TestMergeKeepsPreferredSourceWhenItAlsoHasALargeVariant(t *testing.T) {
	preferred := &model.MovieInfo{
		ID: "a-1", Provider: "Preferred", Number: "ABP-123", Title: "A",
		Homepage:    "https://preferred.example/movie/1",
		CoverURL:    "https://preferred.example/cover.jpg",
		BigCoverURL: "https://preferred.example/big.jpg",
	}
	other := &model.MovieInfo{
		ID: "b-1", Provider: "Other", Number: "ABP-123",
		CoverURL:    "https://other.example/cover.jpg",
		BigCoverURL: "https://other.example/big.jpg",
	}

	got := mergeMovieInfoWithPreference(preferred, []*model.MovieInfo{other, preferred}, MergePreference{Image: "Preferred"})
	if got.CoverURL != preferred.CoverURL {
		t.Fatalf("cover = %q, want the preferred source %q", got.CoverURL, preferred.CoverURL)
	}
}

func TestHasDistinctLargeArtwork(t *testing.T) {
	cases := []struct {
		name string
		info *model.MovieInfo
		want bool
	}{
		{"nil", nil, false},
		{"empty", &model.MovieInfo{}, false},
		{"repeated cover", &model.MovieInfo{CoverURL: "u", BigCoverURL: "u"}, false},
		{"distinct cover", &model.MovieInfo{CoverURL: "u", BigCoverURL: "v"}, true},
		{"big cover only", &model.MovieInfo{BigCoverURL: "v"}, true},
		{"distinct thumb", &model.MovieInfo{ThumbURL: "u", BigThumbURL: "v"}, true},
		{"repeated thumb", &model.MovieInfo{ThumbURL: "u", BigThumbURL: "u"}, false},
	}
	for _, tc := range cases {
		if got := hasDistinctLargeArtwork(tc.info); got != tc.want {
			t.Errorf("%s: hasDistinctLargeArtwork = %v, want %v", tc.name, got, tc.want)
		}
	}
}
