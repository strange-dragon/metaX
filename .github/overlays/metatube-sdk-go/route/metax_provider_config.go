package route

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
)

const maxProviderConfigBody = 64 << 10

type providerConfigRequest struct {
	Type     string            `json:"type"`
	Provider string            `json:"provider"`
	Config   map[string]string `json:"config"`
}

func updateProviderConfig(app *engine.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProviderConfigBody)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var request providerConfigRequest
		if err := decoder.Decode(&request); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, "invalid provider config body")
			return
		}
		if err := ensureJSONEOF(decoder); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, "invalid provider config body")
			return
		}
		request.Type = strings.TrimSpace(request.Type)
		request.Provider = strings.TrimSpace(request.Provider)
		if request.Provider == "" {
			abortWithStatusMessage(c, http.StatusBadRequest, "provider is required")
			return
		}
		if len(request.Config) == 0 {
			abortWithStatusMessage(c, http.StatusBadRequest, "config must not be empty")
			return
		}
		for key, value := range request.Config {
			if !strings.EqualFold(strings.TrimSpace(key), "cookies") &&
				!strings.EqualFold(strings.TrimSpace(key), "cookie") {
				abortWithStatusMessage(c, http.StatusBadRequest, fmt.Sprintf("unsupported config key %q", key))
				return
			}
			if strings.ContainsAny(key+value, "\r\n") {
				abortWithStatusMessage(c, http.StatusBadRequest, fmt.Sprintf("config %q must not contain CR or LF", key))
				return
			}
		}

		if err := app.SetProviderConfig(request.Type, request.Provider, request.Config); err != nil {
			abortWithError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"type":     strings.ToLower(request.Type),
				"provider": request.Provider,
				"applied":  true,
			},
		})
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
