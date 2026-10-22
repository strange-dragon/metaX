package route

import (
	"testing"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"

	"github.com/strange-dragon/metaX/engine"
	"github.com/strange-dragon/metaX/model"
)

func TestPrependAggregatedCandidateMergesMissingSearchFields(t *testing.T) {
	releaseDate := datatypes.Date(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	results := []*model.MovieSearchResult{
		{
			Provider: "JavDB", ID: "javdb-id", Number: "ABP-123",
			Homepage: "https://javdb.invalid/movie/1",
			Actors:   pq.StringArray{"Actor A"},
		},
		{
			Provider: "JavBus", ID: "javbus-id", Number: "ABP-123",
			Title: "Merged title", ThumbURL: "https://javbus.invalid/thumb.jpg",
			CoverURL: "https://javbus.invalid/cover.jpg", Score: 7.5,
			Actors: pq.StringArray{"actor a", "Actor B"}, ReleaseDate: releaseDate,
		},
	}

	got := prependAggregatedCandidate(results)
	if len(got) != 3 {
		t.Fatalf("result count = %d, want 3 (merged + two originals)", len(got))
	}
	merged := got[0]
	if merged.Provider != engine.AggregatedProviderName || merged.ID != "ABP-123" {
		t.Fatalf("merged identity = %s/%s, want %s/ABP-123",
			merged.Provider, merged.ID, engine.AggregatedProviderName)
	}
	if merged.Title != "Merged title" || merged.CoverURL == "" || merged.ThumbURL == "" {
		t.Fatalf("merged fields were not filled: %+v", merged)
	}
	if merged.Homepage != "https://javdb.invalid/movie/1" {
		t.Fatalf("merged homepage = %q, want first route provider homepage", merged.Homepage)
	}
	if merged.Score != 7.5 || time.Time(merged.ReleaseDate).IsZero() {
		t.Fatalf("merged score/date = %v/%v, want 7.5/%v", merged.Score, merged.ReleaseDate, releaseDate)
	}
	if len(merged.Actors) != 2 || merged.Actors[0] != "Actor A" || merged.Actors[1] != "Actor B" {
		t.Fatalf("merged actors = %v, want deduplicated [Actor A Actor B]", merged.Actors)
	}
	if got[1] != results[0] || got[2] != results[1] {
		t.Fatal("original provider results must remain available after the merged candidate")
	}
}

func TestPrependAggregatedCandidateUsesFirstValidResultAndKeepsOriginals(t *testing.T) {
	results := []*model.MovieSearchResult{
		nil,
		{Provider: "JavBus", ID: "javbus-id", Number: "ABP-456", Title: "Second"},
	}
	got := prependAggregatedCandidate(results)
	if len(got) != 3 {
		t.Fatalf("result count = %d, want 3", len(got))
	}
	if got[0].Provider != engine.AggregatedProviderName || got[0].ID != "ABP-456" {
		t.Fatalf("merged candidate = %+v, want MetaX/ABP-456", got[0])
	}
	if got[1] != nil || got[2] != results[1] {
		t.Fatalf("originals were not preserved: %+v", got[1:])
	}
}

func TestPrependAggregatedCandidateLeavesUnidentifiableResultsAlone(t *testing.T) {
	results := []*model.MovieSearchResult{{Provider: "JavDB", ID: "javdb-id"}}
	got := prependAggregatedCandidate(results)
	if len(got) != 1 || got[0] != results[0] {
		t.Fatalf("results without a number must not produce a merged candidate: %+v", got)
	}
}
