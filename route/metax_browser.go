package route

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/strange-dragon/metaX/engine"
	mt "github.com/strange-dragon/metaX/provider"
)

type metaxBrowserState struct {
	Provider    string `json:"provider"`
	Running     bool   `json:"running"`
	Supervised  bool   `json:"supervised"`
	UptimeMS    int64  `json:"uptime_ms,omitempty"`
	Starts      int    `json:"starts"`
	Restarts    int    `json:"restarts"`
	LastStartMS int64  `json:"last_start_ms,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

// getMetaxBrowserStatus reports the fallback browsers the server keeps alive.
//
// The headless browser is invisible from the outside: when it dies, requests
// simply slow down or fail, with nothing that says why. This endpoint makes
// "is it up, how long has it been up, has it been restarted" answerable in one
// request after a deployment.
func getMetaxBrowserStatus(app *engine.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		providers := app.GetMovieProviders()
		names := make([]string, 0, len(providers))
		for name := range providers {
			names = append(names, name)
		}
		sort.Strings(names)

		states := make([]metaxBrowserState, 0, len(names))
		for _, name := range names {
			reporter, ok := providers[name].(mt.BrowserReporter)
			if !ok {
				continue
			}
			status := reporter.BrowserStatus()
			states = append(states, metaxBrowserState{
				Provider:    name,
				Running:     status.Running,
				Supervised:  status.Supervised,
				UptimeMS:    status.Uptime.Milliseconds(),
				Starts:      status.Starts,
				Restarts:    status.Restarts,
				LastStartMS: status.LastStart.Milliseconds(),
				LastError:   status.LastError,
			})
		}
		c.JSON(http.StatusOK, &responseMessage{Data: states})
	}
}
