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
