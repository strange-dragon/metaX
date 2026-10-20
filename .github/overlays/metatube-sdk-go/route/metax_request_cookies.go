package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
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
	scoped, err := app.WithRequestCookies(cookies)
	if err != nil {
		// Do not echo cookie values or provider configuration in the HTTP
		// response. The error is intentionally reduced to a stable message.
		return nil, errors.New("invalid provider cookies")
	}
	return scoped, nil
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
	for name, value := range cookies {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("provider cookie names must not be empty")
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("provider %s cookies must not contain CR or LF", name)
		}
	}
	return cookies, nil
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
