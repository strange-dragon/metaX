package route_test

import (
	"net/http"
	"testing"
)

func TestMetaXSearchAndDetailAPIContracts(t *testing.T) {
	router := newContractRouter(t)
	cases := []struct {
		name, target, expectedID, expectedProvider string
	}{
		{"movie search", "/v1/movies/search?q=contract&provider=" + contractMovieName + "&fallback=false", "movie-123", contractMovieName},
		{"actor search", "/v1/actors/search?q=Contract%20Actor&provider=" + contractActorName + "&fallback=false", "actor-42", contractActorName},
		{"movie detail", "/v1/movies/" + contractMovieName + "/movie-123?lazy=false", "movie-123", contractMovieName},
		{"actor detail", "/v1/actors/" + contractActorName + "/actor-42?lazy=false", "actor-42", contractActorName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := requestJSON(t, router, tc.target)
			if status != http.StatusOK {
				t.Fatalf("status=%d want=%d body=%v", status, http.StatusOK, body)
			}
			data, ok := body["data"]
			if !ok {
				t.Fatalf("response has no data field: %v", body)
			}
			var record map[string]any
			switch v := data.(type) {
			case []any:
				if len(v) != 1 {
					t.Fatalf("result count=%d want=1: %v", len(v), v)
				}
				record, ok = v[0].(map[string]any)
			case map[string]any:
				record = v
			default:
				t.Fatalf("unexpected data shape %T: %v", data, data)
			}
			if !ok {
				t.Fatalf("unexpected result record: %v", data)
			}
			if record["id"] != tc.expectedID {
				t.Errorf("id=%v want=%s", record["id"], tc.expectedID)
			}
			if record["provider"] != tc.expectedProvider {
				t.Errorf("provider=%v want=%s", record["provider"], tc.expectedProvider)
			}
			if tc.name == "movie search" || tc.name == "movie detail" {
				for _, key := range []string{"number", "title", "homepage", "cover_url"} {
					if record[key] == nil || record[key] == "" {
						t.Errorf("movie response missing %q: %v", key, record)
					}
				}
			} else {
				for _, key := range []string{"name", "homepage"} {
					if record[key] == nil || record[key] == "" {
						t.Errorf("actor response missing %q: %v", key, record)
					}
				}
			}
		})
	}
}

func TestMetaXSearchRequiresQuery(t *testing.T) {
	router := newContractRouter(t)
	for _, target := range []string{"/v1/movies/search", "/v1/actors/search"} {
		status, body := requestJSON(t, router, target)
		if status != http.StatusBadRequest {
			t.Errorf("%s status=%d want=%d body=%v", target, status, http.StatusBadRequest, body)
		}
		if body["error"] == nil {
			t.Errorf("%s error response missing error field: %v", target, body)
		}
	}
}
