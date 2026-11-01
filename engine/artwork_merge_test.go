package engine

import (
	"reflect"
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

	got := mergeMovieInfoWithPreference(identity, []*model.MovieInfo{identity, rich}, MergePreference{Images: []string{"ThumbOnly"}})
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

	got := mergeMovieInfoWithPreference(preferred, []*model.MovieInfo{other, preferred}, MergePreference{Images: []string{"Preferred"}})
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

	got := mergeMovieInfoWithPreference(preferred, []*model.MovieInfo{other, preferred}, MergePreference{Images: []string{"Preferred"}})
	if got.CoverURL != preferred.CoverURL {
		t.Fatalf("cover = %q, want the preferred source %q", got.CoverURL, preferred.CoverURL)
	}
}

// TestMergeUsesJavDBArtworkOnlyAsLastResort is the rule the deployment asked
// for: JavDB is the metadata preference and sits early in the route, and it has
// a distinct large variant too — so both ordering rules would hand it the cover.
// A sibling source with a usable image must still win; JavDB is the fallback,
// not a preference.
func TestMergeUsesJavDBArtworkOnlyAsLastResort(t *testing.T) {
	t.Setenv(EnvArtworkLastResort, "JavDB")

	javdb := &model.MovieInfo{
		ID: "javdb-1", Provider: "JavDB", Number: "ABP-123", Title: "从 JavDB 来的标题",
		Homepage:    "https://javdb.example/v/1",
		CoverURL:    "https://javdb.example/cover.jpg",
		BigCoverURL: "https://javdb.example/cover-big.jpg",
		ThumbURL:    "https://javdb.example/thumb.jpg",
	}
	other := &model.MovieInfo{
		ID: "other-1", Provider: "Other", Number: "ABP-123",
		CoverURL: "https://other.example/cover.jpg",
		ThumbURL: "https://other.example/thumb.jpg",
	}

	got := mergeMovieInfoWithPreference(javdb, []*model.MovieInfo{javdb, other}, MergePreference{
		Detail: "JavDB",
		Images: []string{"JavDB"},
	})
	if got.CoverURL != other.CoverURL {
		t.Fatalf("cover = %q, want the non-JavDB source %q", got.CoverURL, other.CoverURL)
	}
	if got.ThumbURL != other.ThumbURL {
		t.Fatalf("thumb = %q, want the non-JavDB source %q", got.ThumbURL, other.ThumbURL)
	}
	// The demotion is about artwork only: JavDB keeps winning the text.
	if got.Title != javdb.Title {
		t.Fatalf("title = %q, want the detail preference %q", got.Title, javdb.Title)
	}
}

// TestMergeFallsBackToJavDBArtworkWhenNoOtherSourceHasOne pins the other half of
// the rule. Demoted is not dropped: a title only JavDB can picture still gets a
// cover instead of an empty field.
func TestMergeFallsBackToJavDBArtworkWhenNoOtherSourceHasOne(t *testing.T) {
	t.Setenv(EnvArtworkLastResort, "JavDB")

	javdb := &model.MovieInfo{
		ID: "javdb-1", Provider: "JavDB", Number: "ABP-123", Title: "Title",
		Homepage:    "https://javdb.example/v/1",
		CoverURL:    "https://javdb.example/cover.jpg",
		BigCoverURL: "https://javdb.example/cover.jpg",
	}
	bare := &model.MovieInfo{ID: "bare-1", Provider: "Bare", Number: "ABP-123"}

	got := mergeMovieInfoWithPreference(javdb, []*model.MovieInfo{javdb, bare}, MergePreference{
		Detail: "JavDB",
		Images: []string{"Bare"},
	})
	if got.CoverURL != javdb.CoverURL {
		t.Fatalf("cover = %q, want the JavDB fallback %q", got.CoverURL, javdb.CoverURL)
	}
}

func TestDemoteProviders(t *testing.T) {
	name := func(s string) string { return s }
	cases := []struct {
		name      string
		items     []string
		providers []string
		want      []string
	}{
		{"no demotion list", []string{"A", "JavDB", "B"}, nil, []string{"A", "JavDB", "B"}},
		{"none present", []string{"A", "B"}, []string{"JavDB"}, []string{"A", "B"}},
		{"all present", []string{"JavDB", "JavDB"}, []string{"JavDB"}, []string{"JavDB", "JavDB"}},
		{"single item", []string{"JavDB"}, []string{"JavDB"}, []string{"JavDB"}},
		{"moves to back", []string{"JavDB", "A", "B"}, []string{"JavDB"}, []string{"A", "B", "JavDB"}},
		{"keeps relative order", []string{"A", "JavDB", "B", "javdb"}, []string{"javdb"}, []string{"A", "B", "JavDB", "javdb"}},
		{"case insensitive", []string{"javdb", "A"}, []string{"JavDB"}, []string{"A", "javdb"}},
	}
	for _, tc := range cases {
		got := DemoteProviders(tc.items, tc.providers, name)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: DemoteProviders = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestActiveArtworkLastResort(t *testing.T) {
	t.Setenv(EnvArtworkLastResort, "")
	if got := ActiveArtworkLastResort(); got != nil {
		t.Fatalf("empty value = %v, want nil (disabled)", got)
	}
	t.Setenv(EnvArtworkLastResort, "off")
	if got := ActiveArtworkLastResort(); got != nil {
		t.Fatalf("off = %v, want nil", got)
	}
	t.Setenv(EnvArtworkLastResort, " JavDB , Something ")
	got := ActiveArtworkLastResort()
	if want := []string{"JavDB", "Something"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	t.Setenv(EnvArtworkLastResort, "JavDB,off")
	if got := ActiveArtworkLastResort(); got != nil {
		t.Fatalf("list containing off = %v, want nil", got)
	}
}

func TestDemotedTail(t *testing.T) {
	name := func(s string) string { return s }
	ordered := []string{"A", "B", "JavDB"}
	if got, want := demotedTail(ordered, []string{"JavDB"}, name), []string{"JavDB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("demotedTail = %v, want %v", got, want)
	}
	// Nothing to demote: the tail must stay empty so the reserve lookup costs
	// nothing.
	if got := demotedTail(ordered, nil, name); got != nil {
		t.Fatalf("demotedTail(nil list) = %v, want nil", got)
	}
	if got := demotedTail([]string{"A", "B"}, []string{"JavDB"}, name); len(got) != 0 {
		t.Fatalf("demotedTail(absent) = %v, want empty", got)
	}
	if got, want := demotedTail([]string{"A", "JavDB", "javdb"}, []string{"JavDB"}, name), []string{"JavDB", "javdb"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("demotedTail = %v, want %v", got, want)
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
