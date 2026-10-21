package route_test

import (
	"net/http"
	"testing"

	sdkroute "github.com/metatube-community/metatube-sdk-go/route"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

func TestMetaXStatusReportsNonSecretState(t *testing.T) {
	original := routing.ActiveConfig()
	t.Cleanup(func() {
		if err := routing.SetActiveConfig(original); err != nil {
			t.Fatalf("restore routing config: %v", err)
		}
	})
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {contractMovieName, "MissingProvider"},
	}}
	if err := routing.SetActiveConfig(config); err != nil {
		t.Fatalf("set routing config: %v", err)
	}

	status, body := requestJSON(t, newContractRouter(t), "/v1/metax/status")
	if status != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%v", status, http.StatusOK, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("status response missing data object: %v", body)
	}
	if got := data["route_fingerprint"]; got != config.FingerprintHash() {
		t.Errorf("route_fingerprint=%v want=%s", got, config.FingerprintHash())
	}
	if got := data["route_categories"]; got != float64(1) {
		t.Errorf("route_categories=%v want=1", got)
	}
	if got := data["cookie_transport_version"]; got != sdkroute.MetaXCookieTransportVersion {
		t.Errorf("cookie_transport_version=%v want=%s", got, sdkroute.MetaXCookieTransportVersion)
	}
	if got, ok := data["movie_providers"].(float64); !ok || got < 1 {
		t.Errorf("movie_providers=%v want >= 1", data["movie_providers"])
	}
	if got, ok := data["actor_providers"].(float64); !ok || got < 1 {
		t.Errorf("actor_providers=%v want >= 1", data["actor_providers"])
	}
	routes, ok := data["route_providers"].(map[string]any)
	if !ok {
		t.Fatalf("route_providers=%v want object", data["route_providers"])
	}
	providers, ok := routes[string(routing.CategoryCensored)].([]any)
	if !ok || len(providers) != 1 || providers[0] != contractMovieName {
		t.Fatalf("route_providers[censored]=%v want [%s]", routes[string(routing.CategoryCensored)], contractMovieName)
	}
	gaps, ok := data["route_provider_gaps"].([]any)
	if !ok || len(gaps) != 1 {
		t.Fatalf("route_provider_gaps=%v want one gap", data["route_provider_gaps"])
	}
	gap, _ := gaps[0].(map[string]any)
	if gap["category"] != string(routing.CategoryCensored) {
		t.Errorf("gap category=%v want=%s", gap["category"], routing.CategoryCensored)
	}
	missing, _ := gap["missing"].([]any)
	if len(missing) != 1 || missing[0] != "MissingProvider" {
		t.Errorf("gap missing=%v want=[MissingProvider]", gap["missing"])
	}

	for _, secretKey := range []string{"cookie", "cookies", "token", "access_token", "proxy"} {
		if _, exists := data[secretKey]; exists {
			t.Errorf("status response exposes secret field %q: %v", secretKey, data)
		}
	}
}
