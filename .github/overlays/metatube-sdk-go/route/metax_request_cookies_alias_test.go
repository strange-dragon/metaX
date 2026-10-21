package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

func providerCookieContext(t *testing.T, header string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Request.Header.Set(providerCookiesHeader, header)
	return context
}

func providerCookieRouteContext(t *testing.T, target, header string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if header != "" {
		context.Request.Header.Set(providerCookiesHeader, header)
	}
	return context
}

func withCookieRouteConfig(t *testing.T, routes map[routing.Category][]string) {
	t.Helper()
	original := routing.ActiveConfig()
	t.Cleanup(func() {
		if err := routing.SetActiveConfig(original); err != nil {
			t.Fatalf("restore routing config: %v", err)
		}
	})
	if err := routing.SetActiveConfig(routing.Config{Routes: routes}); err != nil {
		t.Fatalf("set routing config: %v", err)
	}
}

func TestScopeRequestCookiesByCategory(t *testing.T) {
	withCookieRouteConfig(t, map[routing.Category][]string{
		routing.CategoryCensored: {"JavDB", "JavBus"},
	})
	app := engine.New(nil)
	cookies := map[string]string{
		"javdb":     "session=javdb",
		"javbus":    "session=javbus",
		"freejavbt": "session=freejavbt",
	}
	context := providerCookieRouteContext(t, "/?category=censored", "")
	filtered := scopeRequestCookies(context, app, cookies)
	if len(filtered) != 2 || filtered["javdb"] != "session=javdb" || filtered["javbus"] != "session=javbus" {
		t.Fatalf("scopeRequestCookies(category) = %v, want only JavDB/JavBus", filtered)
	}
}

func TestScopeRequestCookiesLetsExplicitProviderWin(t *testing.T) {
	withCookieRouteConfig(t, map[routing.Category][]string{
		routing.CategoryCensored: {"JavDB", "JavBus"},
	})
	app := engine.New(nil)
	cookies := map[string]string{
		"javdb":     "session=javdb",
		"javbus":    "session=javbus",
		"freejavbt": "session=freejavbt",
	}
	context := providerCookieRouteContext(t, "/?category=censored&provider=JavBus", "")
	filtered := scopeRequestCookies(context, app, cookies)
	if len(filtered) != 1 || filtered["javbus"] != "session=javbus" {
		t.Fatalf("scopeRequestCookies(explicit provider) = %v, want only JavBus", filtered)
	}
}

func TestScopeRequestCookiesMapsRouteAliases(t *testing.T) {
	withCookieRouteConfig(t, map[routing.Category][]string{
		routing.CategoryCensored: {"DMM"},
	})
	app := engine.New(nil)
	context := providerCookieRouteContext(t,
		"/?category=censored", `{"DMM":"session=dmm","JavDB":"session=javdb"}`)
	cookies, err := requestProviderCookies(context)
	if err != nil {
		t.Fatalf("requestProviderCookies() error = %v", err)
	}
	filtered := scopeRequestCookies(context, app, cookies)
	if len(filtered) != 1 || filtered["fanza"] != "session=dmm" {
		t.Fatalf("scopeRequestCookies(alias) = %v, want only FANZA alias", filtered)
	}
}

func TestScopeRequestCookiesLeavesUnscopedRequestsAlone(t *testing.T) {
	app := engine.New(nil)
	cookies := map[string]string{"javdb": "session=javdb"}
	context := providerCookieRouteContext(t, "/?q=ABC-123", "")
	filtered := scopeRequestCookies(context, app, cookies)
	if len(filtered) != 1 || filtered["javdb"] != "session=javdb" {
		t.Fatalf("scopeRequestCookies(no scope) = %v, want unchanged", filtered)
	}
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
