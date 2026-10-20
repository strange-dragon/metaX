package route_test

import (
	"net/http"
	"testing"
)

func TestMetaXProvidersEndpointListsIntegratedProviders(t *testing.T) {
	router := newContractRouter(t)
	status, body := requestJSON(t, router, "/v1/providers")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/providers status = %d, want %d; body: %v", status, http.StatusOK, body)
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected providers envelope: %v", body)
	}
	movieProviders, ok := data["movie_providers"].(map[string]any)
	if !ok {
		t.Fatalf("movie_providers missing: %v", data)
	}
	actorProviders, ok := data["actor_providers"].(map[string]any)
	if !ok {
		t.Fatalf("actor_providers missing: %v", data)
	}
	for _, name := range []string{"Airav", "AVSOX", "FC2Club", "Freejavbt", "GIGA", "IQQTV", "Jav321", "JavDB", "JavLibrary", "XCity"} {
		if _, exists := movieProviders[name]; !exists {
			t.Errorf("GET /v1/providers did not expose movie provider %q", name)
		}
	}
	for _, name := range []string{"AVBaseActor", "JavDBActor", "MinnanoActor"} {
		if _, exists := actorProviders[name]; !exists {
			t.Errorf("GET /v1/providers did not expose actor provider %q", name)
		}
	}
}
