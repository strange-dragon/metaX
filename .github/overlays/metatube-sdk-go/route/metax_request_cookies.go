package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

const (
	providerCookiesHeader       = "X-MetaX-Provider-Cookies"
	maxProviderCookiesHeaderLen = 16 << 10
)

// requestEngine returns a per-request engine view configured with the cookie
// dictionary supplied by the Jellyfin plugin. The plugin remains the only
// persistent source of truth; the server never stores these values.
func requestEngine(c *gin.Context, app *engine.Engine) (*engine.Engine, error) {
	cookies, err := requestProviderCookies(c)
	if err != nil {
		return nil, err
	}
	if len(cookies) == 0 {
		return app, nil
	}
	cookies = scopeRequestCookies(c, app, cookies)
	if len(cookies) == 0 {
		return app, nil
	}
	scoped, err := app.WithRequestCookies(cookies)
	if err != nil {
		// Do not echo cookie values or provider configuration in the HTTP
		// response. The error is intentionally reduced to a stable message.
		return nil, errors.New("invalid provider cookies")
	}
	return scoped, nil
}

// scopeRequestCookies keeps only cookies for providers that the current request
// can reach. The plugin is expected to send a category-scoped dictionary, but
// the server enforces the same boundary defensively. Explicit provider
// selection wins over category routing, matching the SDK route contract.
//
// Requests without a category or explicit provider (for example actor search)
// keep the full dictionary because their provider set is not category-routed.
func scopeRequestCookies(c *gin.Context, app *engine.Engine, cookies map[string]string) map[string]string {
	allowed, scoped := requestProviderScope(c, app)
	if !scoped {
		return cookies
	}
	filtered := make(map[string]string, len(cookies))
	for name, value := range cookies {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(name))]; ok {
			filtered[name] = value
		}
	}
	return filtered
}

func requestProviderScope(c *gin.Context, app *engine.Engine) (map[string]struct{}, bool) {
	if explicit := strings.TrimSpace(c.Query("provider")); explicit != "" {
		allowed := make(map[string]struct{})
		for _, name := range routing.ResolveProviderAliases(explicit) {
			allowed[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
		return allowed, true
	}

	rawCategory := strings.TrimSpace(c.Query("category"))
	if rawCategory == "" {
		return nil, false
	}
	category, err := routing.ParseCategory(rawCategory)
	if err != nil {
		// The route handler returns the canonical category error; do not turn
		// a cookie-scope problem into a different response.
		return nil, false
	}
	registered := make([]string, 0, len(app.GetMovieProviders()))
	for name := range app.GetMovieProviders() {
		registered = append(registered, name)
	}
	providers, err := routing.ActiveConfig().Resolve(category, "", registered)
	if err != nil {
		return nil, false
	}
	allowed := make(map[string]struct{}, len(providers))
	for _, name := range providers {
		allowed[strings.ToLower(name)] = struct{}{}
	}
	return allowed, true
}

func requestProviderCookies(c *gin.Context) (map[string]string, error) {
	raw := strings.TrimSpace(c.GetHeader(providerCookiesHeader))
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxProviderCookiesHeaderLen {
		return nil, fmt.Errorf("provider cookie header exceeds %d bytes", maxProviderCookiesHeaderLen)
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	var cookies map[string]string
	if err := decoder.Decode(&cookies); err != nil {
		return nil, errors.New("provider cookie header must be a JSON object")
	}
	if err := ensureProviderCookiesEOF(decoder); err != nil {
		return nil, errors.New("provider cookie header must contain one JSON object")
	}
	expanded := make(map[string]string, len(cookies))
	for name, value := range cookies {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("provider cookie names must not be empty")
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("provider %s cookies must not contain CR or LF", name)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, candidate := range routing.ResolveProviderAliases(name) {
			key := strings.ToLower(strings.TrimSpace(candidate))
			if key == "" {
				continue
			}
			if existing, exists := expanded[key]; exists && existing != value {
				return nil, fmt.Errorf("conflicting cookies for provider %s", candidate)
			}
			expanded[key] = value
		}
	}
	return expanded, nil
}

func ensureProviderCookiesEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
