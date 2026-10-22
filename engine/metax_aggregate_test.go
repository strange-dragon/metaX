package engine

import (
	"encoding/json"
	"testing"

	"github.com/lib/pq"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/routing"
)

func TestMergeMovieInfoFillsMissingFieldsAndKeepsRouteOrder(t *testing.T) {
	identity := &model.MovieInfo{
		ID: "selected-1", Provider: "Selected", Number: "FC2-PPV3954834",
		Title: "Selected title", Homepage: "https://selected.example/movie/1",
		Summary: "", Actors: pq.StringArray{"Alice"}, Genres: pq.StringArray{"Drama"},
		Score: 8.2, CoverURL: "https://selected.example/cover.jpg",
	}
	first := &model.MovieInfo{
		ID: "first-1", Provider: "First", Number: "FC2PPV3954834",
		Title: "Different title", Summary: "Preferred summary",
		Actors: pq.StringArray{"Alice", "Bob"}, Genres: pq.StringArray{"Drama", "Amateur"},
		Score: 4.1, Runtime: 120, CoverURL: "https://first.example/cover.jpg",
	}
	second := &model.MovieInfo{
		ID: "second-1", Provider: "Second", Number: "FC2 PPV 3954834",
		Summary: "Lower-priority summary", Director: "Director",
		Actors: pq.StringArray{"Carol"}, Genres: pq.StringArray{"Amateur", "Comedy"},
		Score: 9.9, Runtime: 90,
	}

	got := mergeMovieInfoInRouteOrder(identity, []*model.MovieInfo{first, second, identity})
	if got.ID != identity.ID || got.Provider != identity.Provider || got.Number != identity.Number || got.Homepage != identity.Homepage {
		t.Fatalf("selected provider identity changed: %#v", got)
	}
	if got.Title != first.Title || got.Summary != "Preferred summary" || got.Director != "Director" {
		t.Fatalf("scalar fields were not filled in route order: title=%q summary=%q director=%q", got.Title, got.Summary, got.Director)
	}
	if got.Runtime != 120 {
		t.Fatalf("runtime=%d want first non-zero route value 120", got.Runtime)
	}
	if got.Score != identity.Score {
		t.Fatalf("score=%v want selected-source score %v; scores must not be averaged", got.Score, identity.Score)
	}
	if len(got.Actors) != 3 || got.Actors[0] != "Alice" || got.Actors[1] != "Bob" || got.Actors[2] != "Carol" {
		t.Fatalf("actors were not stably deduplicated: %#v", got.Actors)
	}
	if len(got.Genres) != 3 || got.Genres[0] != "Drama" || got.Genres[1] != "Amateur" || got.Genres[2] != "Comedy" {
		t.Fatalf("genres were not stably deduplicated: %#v", got.Genres)
	}
	if got.CoverURL != first.CoverURL {
		t.Fatalf("first route source should win field conflict: %q", got.CoverURL)
	}
}

func TestNormalizeAggregationNumberFollowsAmaneFoldRules(t *testing.T) {
	// Case, spaces and hyphens are insignificant.
	if !routing.SameNumber(" FC2-PPV3954834 ", "fc2 ppv 3954834") {
		t.Fatal("expected case, spaces, and hyphens to be normalized")
	}
	if routing.SameNumber("FC2-PPV3954834", "FC2-PPV3954835") {
		t.Fatal("different catalog numbers must not match")
	}
	// Amane keeps underscores significant: 010115_001 and 010115-001 are two
	// different titles.
	if routing.SameNumber("101926_01", "10192601") || routing.SameNumber("101926_01", "101926-01") {
		t.Fatal("underscores are significant in catalog numbers and must not be removed")
	}
	// The FC2 family is canonicalized because providers spell the same product
	// differently and metaX merges FC2 into the uncensored route.
	for _, equivalent := range []string{"FC2-3954834", "FC2PPV3954834", "FC2-PPV-3954834", "FC2_PPV_3954834", "fc2 ppv 3954834"} {
		if !routing.SameNumber(equivalent, "FC2-PPV-3954834") {
			t.Fatalf("%q should match FC2-PPV-3954834", equivalent)
		}
	}
	if routing.SameNumber("3954834", "FC2-PPV-3954834") {
		t.Fatal("a bare numeric provider ID must not match an FC2 catalog number")
	}
}

func TestAggregatedMovieInfoKeepsMovieFieldsTopLevelAndIncludesSources(t *testing.T) {
	aggregate := &AggregatedMovieInfo{
		MovieInfo: &model.MovieInfo{
			ID: "movie-1", Provider: "Selected", Number: "FC2-PPV3954834",
			Title: "Selected title", Homepage: "https://selected.example/movie/1",
		},
		Category: "uncensored",
		Sources: []MovieSourceRecord{{
			Provider: "Selected", ID: "movie-1", Status: "success",
			Metadata: &model.MovieInfo{ID: "movie-1", Provider: "Selected", Number: "FC2-PPV3954834"},
		}},
	}
	encoded, err := json.Marshal(aggregate)
	if err != nil {
		t.Fatalf("marshal aggregate: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("unmarshal aggregate: %v", err)
	}
	if body["id"] != "movie-1" || body["provider"] != "Selected" {
		t.Fatalf("MovieInfo fields were not kept at the top level: %s", encoded)
	}
	if _, nested := body["MovieInfo"]; nested {
		t.Fatalf("MovieInfo was unexpectedly nested: %s", encoded)
	}
	if body["category"] != "uncensored" {
		t.Fatalf("category missing from aggregate response: %s", encoded)
	}
	sources, ok := body["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("source records missing from aggregate response: %s", encoded)
	}
}
