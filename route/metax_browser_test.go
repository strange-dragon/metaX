package route_test

import (
	"net/http"
	"testing"
)

func TestMetaXBrowserStatusReportsBrowserProviders(t *testing.T) {
	status, body := requestJSON(t, newContractRouter(t), "/v1/metax/browser")
	if status != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%v", status, http.StatusOK, body)
	}
	states, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data=%v want a list of browser states", body["data"])
	}

	var javlibrary map[string]any
	for _, raw := range states {
		state, _ := raw.(map[string]any)
		if name, _ := state["provider"].(string); name == "JavLibrary" {
			javlibrary = state
		}
	}
	if javlibrary == nil {
		t.Fatalf("data=%v want JavLibrary to report its browser", states)
	}
	// Unit tests never spawn a browser, so the honest answer is "not running"
	// rather than a missing entry: the endpoint must describe state, not hide it.
	if running, _ := javlibrary["running"].(bool); running {
		t.Errorf("running=%v want false in tests", javlibrary["running"])
	}
	for _, secretKey := range []string{"cookie", "cookies", "token", "proxy"} {
		if _, exists := javlibrary[secretKey]; exists {
			t.Errorf("browser status exposes secret field %q: %v", secretKey, javlibrary)
		}
	}
}
