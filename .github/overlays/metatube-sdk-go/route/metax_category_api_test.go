package route_test

import (
	"net/http"
	"testing"
)

func TestMetaXCategorySearchRejectsUnknownCategory(t *testing.T) {
	router := newContractRouter(t)
	status, body := requestJSON(t, router, "/v1/movies/search?q=contract&category=not-a-category&fallback=false")
	if status != http.StatusBadRequest {
		t.Fatalf("unknown category status=%d want=%d body=%v", status, http.StatusBadRequest, body)
	}
	if body["error"] == nil {
		t.Fatalf("unknown category response has no error field: %v", body)
	}
}

func TestMetaXExplicitProviderOverridesCategory(t *testing.T) {
	router := newContractRouter(t)
	target := "/v1/movies/search?q=contract&provider=" + contractMovieName + "&category=not-a-category&fallback=false"
	status, body := requestJSON(t, router, target)
	if status != http.StatusOK {
		t.Fatalf("explicit provider should override category; status=%d body=%v", status, body)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("unexpected search result envelope: %v", body)
	}
	result, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result record: %v", data[0])
	}
	if result["provider"] != contractMovieName || result["id"] != "movie-123" {
		t.Fatalf("unexpected provider/id identity: %v", result)
	}
}

func TestMetaXMovieInfoAggregatesByDefaultAndPreservesSourceRecord(t *testing.T) {
	router := newContractRouter(t)
	status, body := requestJSON(t, router, "/v1/movies/"+contractMovieName+"/movie-123")
	if status != http.StatusOK {
		t.Fatalf("detail status=%d want=%d body=%v", status, http.StatusOK, body)
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected detail response envelope: %v", body)
	}
	if data["id"] != "movie-123" || data["provider"] != contractMovieName {
		t.Fatalf("selected provider identity changed: %v", data)
	}
	if data["title"] != "Contract title" || data["summary"] != "Contract summary" {
		t.Fatalf("original MovieInfo fields missing from top-level response: %v", data)
	}
	if data["category"] != "unknown" {
		t.Fatalf("unknown catalog number should be marked unknown, got %v", data["category"])
	}

	sources, ok := data["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("expected one baseline source record, got %v", data["sources"])
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected source record: %v", sources[0])
	}
	if source["provider"] != contractMovieName || source["id"] != "movie-123" || source["status"] != "baseline_only" {
		t.Fatalf("unexpected source identity/status: %v", source)
	}
	metadata, ok := source["metadata"].(map[string]any)
	if !ok || metadata["title"] != "Contract title" || metadata["number"] != "contract" {
		t.Fatalf("independent source metadata missing or incomplete: %v", source["metadata"])
	}
}

func TestMetaXMovieInfoAggregateFalsePreservesStockDetailResponse(t *testing.T) {
	router := newContractRouter(t)
	status, body := requestJSON(t, router, "/v1/movies/"+contractMovieName+"/movie-123?aggregate=false")
	if status != http.StatusOK {
		t.Fatalf("detail status=%d want=%d body=%v", status, http.StatusOK, body)
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected detail response envelope: %v", body)
	}
	if data["id"] != "movie-123" || data["provider"] != contractMovieName || data["title"] != "Contract title" {
		t.Fatalf("stock MovieInfo fields changed: %v", data)
	}
	if _, exists := data["category"]; exists {
		t.Fatalf("aggregate=false must omit category: %v", data)
	}
	if _, exists := data["sources"]; exists {
		t.Fatalf("aggregate=false must omit source records: %v", data)
	}
}
