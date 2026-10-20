package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func providerCookieContext(t *testing.T, header string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Request.Header.Set(providerCookiesHeader, header)
	return context
}

func TestRequestProviderCookiesExpandsRouteAliases(t *testing.T) {
	cookies, err := requestProviderCookies(providerCookieContext(t,
		`{"DMM":"session=dmm","MGStage":"session=mgs","ThePornDB":"session=porndb","JavDB":"session=javdb"}`))
	if err != nil {
		t.Fatalf("requestProviderCookies() error = %v", err)
	}
	want := map[string]string{
		"dmm":            "session=dmm",
		"fanza":          "session=dmm",
		"mgstage":        "session=mgs",
		"mgs":            "session=mgs",
		"theporndb":      "session=porndb",
		"theporndbmovie": "session=porndb",
		"theporndbscene": "session=porndb",
		"javdb":          "session=javdb",
	}
	for key, value := range want {
		if cookies[key] != value {
			t.Fatalf("expanded cookies[%q] = %q, want %q; all=%v", key, cookies[key], value, cookies)
		}
	}
}

func TestRequestProviderCookiesRejectsAliasConflict(t *testing.T) {
	if _, err := requestProviderCookies(providerCookieContext(t,
		`{"DMM":"session=one","FANZA":"session=two"}`)); err == nil {
		t.Fatal("requestProviderCookies() error = nil, want alias conflict")
	}
	if _, err := requestProviderCookies(providerCookieContext(t,
		`{"DMM":"session=one","FANZA":"session=one"}`)); err != nil {
		t.Fatalf("requestProviderCookies() identical alias values error = %v", err)
	}
}

func TestRequestProviderCookiesSkipsEmptyAliasValues(t *testing.T) {
	cookies, err := requestProviderCookies(providerCookieContext(t, `{"DMM":"   "}`))
	if err != nil {
		t.Fatalf("requestProviderCookies() error = %v", err)
	}
	if len(cookies) != 0 {
		t.Fatalf("requestProviderCookies() = %v, want no cookies", cookies)
	}
}
