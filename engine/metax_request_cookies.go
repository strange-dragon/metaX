package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/strange-dragon/metaX/collection/maps"
	mt "github.com/strange-dragon/metaX/provider"
)

var errConfigKeyNotFound = errors.New("key not found")

// WithRequestCookies returns a request-scoped engine view. Providers named in
// the cookie dictionary are replaced with fresh instances configured only for
// this request. The receiver and its provider instances are never mutated, so
// concurrent requests carrying different cookies cannot leak into each other.
//
// Unknown provider names are ignored for forward compatibility. A known
// provider that cannot accept a request cookie is reported as an error.
func (e *Engine) WithRequestCookies(cookies map[string]string) (*Engine, error) {
	normalized := make(map[string]string, len(cookies))
	for name, raw := range cookies {
		name = strings.TrimSpace(name)
		raw = strings.TrimSpace(raw)
		if name == "" || raw == "" {
			continue
		}
		if strings.ContainsAny(raw, "\r\n") {
			return nil, fmt.Errorf("provider %s cookies must not contain CR or LF", name)
		}
		normalized[strings.ToLower(name)] = raw
	}
	if len(normalized) == 0 {
		return e, nil
	}

	cloned := *e
	cloned.movieProviders = e.movieProviders.Copy()
	cloned.actorProviders = e.actorProviders.Copy()
	movieReplacements := make(map[string]mt.MovieProvider)
	actorReplacements := make(map[string]mt.ActorProvider)

	for name, raw := range normalized {
		if provider, ok := e.movieProviders.Get(name); ok {
			clone, err := e.cloneMovieProviderWithCookies(provider, raw)
			if err != nil {
				return nil, err
			}
			cloned.movieProviders.Set(provider.Name(), clone)
			movieReplacements[strings.ToLower(provider.Name())] = clone
		}
		if provider, ok := e.actorProviders.Get(name); ok {
			clone, err := e.cloneActorProviderWithCookies(provider, raw)
			if err != nil {
				return nil, err
			}
			cloned.actorProviders.Set(provider.Name(), clone)
			actorReplacements[strings.ToLower(provider.Name())] = clone
		}
	}

	cloned.movieHostProviders = replaceMovieHostProviders(e.movieHostProviders, movieReplacements)
	cloned.actorHostProviders = replaceActorHostProviders(e.actorHostProviders, actorReplacements)
	return &cloned, nil
}

func (e *Engine) cloneMovieProviderWithCookies(provider mt.MovieProvider, cookies string) (mt.MovieProvider, error) {
	factory := findMovieFactory(provider.Name())
	if factory == nil {
		return nil, fmt.Errorf("provider %s is not available for request-scoped cookies", provider.Name())
	}
	clone := factory()
	if clone == nil {
		return nil, fmt.Errorf("provider %s factory returned nil", provider.Name())
	}
	config := &requestCookieConfig{
		base:   providerConfigOrNil(e.movieProviderConfigs, provider.Name()),
		cookie: cookies,
	}
	if err := e.applyRequestProviderConfig("movie", clone, config); err != nil {
		return nil, err
	}
	return clone, nil
}

func (e *Engine) cloneActorProviderWithCookies(provider mt.ActorProvider, cookies string) (mt.ActorProvider, error) {
	factory := findActorFactory(provider.Name())
	if factory == nil {
		return nil, fmt.Errorf("provider %s is not available for request-scoped cookies", provider.Name())
	}
	clone := factory()
	if clone == nil {
		return nil, fmt.Errorf("provider %s factory returned nil", provider.Name())
	}
	config := &requestCookieConfig{
		base:   providerConfigOrNil(e.actorProviderConfigs, provider.Name()),
		cookie: cookies,
	}
	if err := e.applyRequestProviderConfig("actor", clone, config); err != nil {
		return nil, err
	}
	return clone, nil
}

func (e *Engine) applyRequestProviderConfig(providerType string, provider mt.Provider, config mt.Config) error {
	const (
		proxyConfigKey    = "proxy"
		priorityConfigKey = "priority"
		timeoutConfigKey  = "timeout"
	)

	if setter, ok := provider.(mt.RequestTimeoutSetter); ok {
		setter.SetRequestTimeout(e.timeout /* global timeout */)
	}
	if config.Has(priorityConfigKey) {
		if value, err := config.GetFloat64(priorityConfigKey); err == nil {
			provider.SetPriority(value)
		}
	}
	if setter, ok := provider.(mt.ProxySetter); ok && config.Has(proxyConfigKey) {
		value, err := config.GetString(proxyConfigKey)
		if err != nil {
			return fmt.Errorf("read %s provider %s proxy: %w", providerType, provider.Name(), err)
		}
		if err := setter.SetProxy(value); err != nil {
			return fmt.Errorf("set %s provider %s proxy: %w", providerType, provider.Name(), err)
		}
	}
	if setter, ok := provider.(mt.RequestTimeoutSetter); ok && config.Has(timeoutConfigKey) {
		value, err := config.GetDuration(timeoutConfigKey)
		if err != nil {
			return fmt.Errorf("read %s provider %s timeout: %w", providerType, provider.Name(), err)
		}
		setter.SetRequestTimeout(value)
	}
	setter, ok := provider.(mt.ConfigSetter)
	if !ok {
		return fmt.Errorf("provider %s does not support cookies", provider.Name())
	}
	if err := setter.SetConfig(config); err != nil {
		return fmt.Errorf("configure %s provider %s: %w", providerType, provider.Name(), err)
	}
	return nil
}

func providerConfigOrNil(configs *maps.CaseInsensitiveMap[mt.Config], name string) mt.Config {
	if configs == nil {
		return nil
	}
	config, _ := configs.Get(name)
	return config
}

func findMovieFactory(name string) mt.MovieFactory {
	var factory mt.MovieFactory
	mt.RangeMovieFactory(func(candidate string, value mt.MovieFactory) bool {
		if strings.EqualFold(candidate, name) {
			factory = value
		}
		return true
	})
	return factory
}

func findActorFactory(name string) mt.ActorFactory {
	var factory mt.ActorFactory
	mt.RangeActorFactory(func(candidate string, value mt.ActorFactory) bool {
		if strings.EqualFold(candidate, name) {
			factory = value
		}
		return true
	})
	return factory
}

func replaceMovieHostProviders(
	original *maps.CaseInsensitiveMap[[]mt.MovieProvider],
	replacements map[string]mt.MovieProvider,
) *maps.CaseInsensitiveMap[[]mt.MovieProvider] {
	cloned := maps.NewCaseInsensitiveMap[[]mt.MovieProvider]()
	if original == nil {
		return cloned
	}
	for host, providers := range original.Iterator() {
		next := make([]mt.MovieProvider, 0, len(providers))
		for _, provider := range providers {
			if replacement, ok := replacements[strings.ToLower(provider.Name())]; ok {
				next = append(next, replacement)
				continue
			}
			next = append(next, provider)
		}
		cloned.Set(host, next)
	}
	return cloned
}

func replaceActorHostProviders(
	original *maps.CaseInsensitiveMap[[]mt.ActorProvider],
	replacements map[string]mt.ActorProvider,
) *maps.CaseInsensitiveMap[[]mt.ActorProvider] {
	cloned := maps.NewCaseInsensitiveMap[[]mt.ActorProvider]()
	if original == nil {
		return cloned
	}
	for host, providers := range original.Iterator() {
		next := make([]mt.ActorProvider, 0, len(providers))
		for _, provider := range providers {
			if replacement, ok := replacements[strings.ToLower(provider.Name())]; ok {
				next = append(next, replacement)
				continue
			}
			next = append(next, provider)
		}
		cloned.Set(host, next)
	}
	return cloned
}

// requestCookieConfig overlays a request cookie onto the provider's startup
// configuration without mutating that shared configuration.
type requestCookieConfig struct {
	base   mt.Config
	cookie string
}

func (c *requestCookieConfig) Has(key string) bool {
	if isCookieKey(key) {
		return true
	}
	return c.base != nil && c.base.Has(key)
}

func (c *requestCookieConfig) GetString(key string) (string, error) {
	if isCookieKey(key) {
		return c.cookie, nil
	}
	if c.base != nil {
		return c.base.GetString(key)
	}
	return "", errConfigKeyNotFound
}

func (c *requestCookieConfig) GetBool(key string) (bool, error) {
	if c.base != nil {
		return c.base.GetBool(key)
	}
	return false, errConfigKeyNotFound
}

func (c *requestCookieConfig) GetInt64(key string) (int64, error) {
	if c.base != nil {
		return c.base.GetInt64(key)
	}
	return 0, errConfigKeyNotFound
}

func (c *requestCookieConfig) GetFloat64(key string) (float64, error) {
	if c.base != nil {
		return c.base.GetFloat64(key)
	}
	return 0, errConfigKeyNotFound
}

func (c *requestCookieConfig) GetDuration(key string) (time.Duration, error) {
	if c.base != nil {
		return c.base.GetDuration(key)
	}
	return 0, errConfigKeyNotFound
}

func isCookieKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "cookies") ||
		strings.EqualFold(strings.TrimSpace(key), "cookie")
}
