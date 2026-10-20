package engine

import (
	"fmt"
	"net/http"
	"strings"

	sdkerrors "github.com/metatube-community/metatube-sdk-go/errors"
	"github.com/metatube-community/metatube-sdk-go/internal/envconfig"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

// SetProviderConfig applies a provider configuration update at runtime.
//
// The current task intentionally keeps updates in memory. Persistence and
// startup replay are a separate follow-up so the request path remains small
// and fully testable. The provider must implement provider.ConfigSetter.
func (e *Engine) SetProviderConfig(providerType, name string, values map[string]string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return sdkerrors.New(http.StatusBadRequest, "provider is required")
	}
	if len(values) == 0 {
		return sdkerrors.New(http.StatusBadRequest, "config must not be empty")
	}

	var provider mt.Provider
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "movie":
		p, ok := e.movieProviders.Get(name)
		if !ok {
			return mt.ErrProviderNotFound
		}
		provider = p
	case "actor":
		p, ok := e.actorProviders.Get(name)
		if !ok {
			return mt.ErrProviderNotFound
		}
		provider = p
	default:
		return sdkerrors.New(http.StatusBadRequest, "type must be movie or actor")
	}

	setter, ok := provider.(mt.ConfigSetter)
	if !ok {
		return sdkerrors.New(http.StatusBadRequest, fmt.Sprintf("provider %s does not support runtime configuration", provider.Name()))
	}

	config := envconfig.NewConfig()
	for key, value := range values {
		key = strings.TrimSpace(key)
		if !strings.EqualFold(key, "cookies") && !strings.EqualFold(key, "cookie") {
			return sdkerrors.New(http.StatusBadRequest, fmt.Sprintf("unsupported config key %q", key))
		}
		if strings.ContainsAny(key+value, "\r\n") {
			return sdkerrors.New(http.StatusBadRequest, fmt.Sprintf("config %q must not contain CR or LF", key))
		}
		config.Set(key, value)
	}
	if err := setter.SetConfig(config); err != nil {
		return sdkerrors.New(http.StatusBadRequest, err.Error())
	}
	return nil
}
