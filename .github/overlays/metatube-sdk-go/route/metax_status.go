package route

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

// MetaXCookieTransportVersion identifies the request-scoped cookie transport
// implemented by route/metax_request_cookies.go. Bump it when the header
// contract changes; clients may use it to detect an old server.
const MetaXCookieTransportVersion = "1"

type metaxRouteProviderGap struct {
	Category string   `json:"category"`
	Missing  []string `json:"missing"`
}

type metaxStatus struct {
	MovieProviders         int                     `json:"movie_providers"`
	ActorProviders         int                     `json:"actor_providers"`
	RouteCategories        int                     `json:"route_categories"`
	RouteFingerprint       string                  `json:"route_fingerprint"`
	RouteProviderGaps      []metaxRouteProviderGap `json:"route_provider_gaps"`
	CookieTransportVersion string                  `json:"cookie_transport_version"`
}

// getMetaxStatus exposes non-secret operational state for health checks and
// deployment diagnostics. Provider configuration, cookies, tokens, and proxy
// credentials are intentionally absent.
func getMetaxStatus(app *engine.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		config := routing.ActiveConfig()
		registered := make([]string, 0, len(app.GetMovieProviders()))
		for name := range app.GetMovieProviders() {
			registered = append(registered, name)
		}
		coverage := config.ProviderCoverage(registered)
		gaps := make([]metaxRouteProviderGap, 0, len(coverage.Gaps))
		for _, gap := range coverage.Gaps {
			gaps = append(gaps, metaxRouteProviderGap{
				Category: string(gap.Category),
				Missing:  append([]string(nil), gap.Missing...),
			})
		}
		c.JSON(http.StatusOK, &responseMessage{Data: metaxStatus{
			MovieProviders:         len(app.GetMovieProviders()),
			ActorProviders:         len(app.GetActorProviders()),
			RouteCategories:        len(config.Routes),
			RouteFingerprint:       config.FingerprintHash(),
			RouteProviderGaps:      gaps,
			CookieTransportVersion: MetaXCookieTransportVersion,
		}})
	}
}
