package engine

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/strange-dragon/metaX/common/fetch"
	mt "github.com/strange-dragon/metaX/provider"
)

func (e *Engine) init() *Engine {
	e.initLogger()
	e.initFetcher()
	e.initActorProviders()
	e.initMovieProviders()
	e.warmBrowsers()
	return e
}

func (e *Engine) initLogger() {
	e.logger = log.New(os.Stdout, "[ENGINE]\u0020", log.LstdFlags|log.Llongfile)
}

func (e *Engine) initFetcher() {
	e.fetcher = fetch.Default(&fetch.Config{Timeout: e.timeout})
}

// initActorProviders initializes actor providers.
func (e *Engine) initActorProviders() {
	for name, factory := range mt.RangeActorFactory {
		provider := factory()

		if s, ok := provider.(mt.RequestTimeoutSetter); ok {
			s.SetRequestTimeout(e.timeout /* global timeout */)
		}

		explicitProxy := false
		if config, hasConfig := e.actorProviderConfigs.Get(name); hasConfig {
			explicitProxy = config.Has("proxy")
			e.applyProviderConfig("actor", provider, config)
		}
		if !explicitProxy {
			e.applyEnvProxy("actor", provider)
		}

		if provider.Priority() <= 0 {
			e.logger.Printf("Disable actor provider: %s", provider.Name())
			continue
		}

		// Add actor provider by name.
		e.actorProviders.Set(name, provider)
		// Add actor provider by host.
		host := provider.URL().Hostname()
		e.actorHostProviders.Set(host,
			append(e.actorHostProviders.
				GetOrDefault(host, nil), provider))
	}
}

// initMovieProviders initializes movie providers.
func (e *Engine) initMovieProviders() {
	for name, factory := range mt.RangeMovieFactory {
		provider := factory()

		if s, ok := provider.(mt.RequestTimeoutSetter); ok {
			s.SetRequestTimeout(e.timeout /* global timeout */)
		}

		explicitProxy := false
		if config, hasConfig := e.movieProviderConfigs.Get(name); hasConfig {
			explicitProxy = config.Has("proxy")
			e.applyProviderConfig("movie", provider, config)
		}
		if !explicitProxy {
			e.applyEnvProxy("movie", provider)
		}

		if provider.Priority() <= 0 {
			e.logger.Printf("Disable movie provider: %s", provider.Name())
			continue
		}

		// Add movie provider by name.
		e.movieProviders.Set(name, provider)
		// Add movie provider by host.
		host := provider.URL().Hostname()
		e.movieHostProviders.Set(host,
			append(e.movieHostProviders.
				GetOrDefault(host, nil), provider))
	}
}

// warmBrowsers starts the fallback browser for providers that need one.
//
// A provider behind a JavaScript challenge cannot answer its first request
// until Chromium is running, and on a small cloud instance that cold start
// takes longer than the whole request budget. Doing it at boot means the first
// scrape finds a browser already waiting instead of timing out.
func (e *Engine) warmBrowsers() {
	if os.Getenv("METAX_WARMUP_BROWSER") == "0" {
		return
	}
	if flag.Lookup("test.v") != nil {
		// Unit tests must never spawn a browser.
		return
	}
	providers := e.GetMovieProviders()
	go func() {
		for _, provider := range providers {
			warmer, ok := provider.(mt.BrowserWarmer)
			if !ok {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			start := time.Now()
			err := warmer.WarmupBrowser(ctx)
			cancel()
			if err != nil {
				e.logger.Printf("Warm up browser for %s: %v", provider.Name(), err)
				continue
			}
			e.logger.Printf("Browser ready for %s after %s", provider.Name(), time.Since(start).Round(time.Second))
		}
	}()
}

// applyEnvProxy hands every provider the proxy named by METAX_PROVIDER_PROXY.
//
// Sites that geo-block or score datacenter ASNs need one, and a single-container
// deployment (Koyeb, Cloud Run, a NAS) has no other convenient place to set it:
// the per-provider proxy lives in the database config, which is awkward to edit
// before the service is reachable. A provider that already has an explicit
// "proxy" config keeps it, so the environment variable is only a default.
//
// Failures are logged, not fatal: a proxy that a particular provider's client
// cannot parse should not take the whole server down.
func (e *Engine) applyEnvProxy(providerType string, provider mt.Provider) {
	raw := strings.TrimSpace(os.Getenv("METAX_PROVIDER_PROXY"))
	if raw == "" {
		return
	}
	setter, ok := provider.(mt.ProxySetter)
	if !ok {
		return
	}
	if err := setter.SetProxy(raw); err != nil {
		e.logger.Printf("METAX_PROVIDER_PROXY rejected by %s provider %q: %v", providerType, provider.Name(), err)
		return
	}
	e.logger.Printf("Applied METAX_PROVIDER_PROXY to %s provider %q: %s", providerType, provider.Name(), redactProxyForLog(raw))
}

func (e *Engine) applyProviderConfig(providerType string, provider mt.Provider, config mt.Config) {
	const (
		proxyConfigKey    = "proxy"
		priorityConfigKey = "priority"
		timeoutConfigKey  = "timeout"
	)

	// Apply overridden priority.
	if config.Has(priorityConfigKey) {
		if v, err := config.GetFloat64(priorityConfigKey); err == nil {
			e.logger.Printf("Override %s provider priority: %s=%.2f", providerType, provider.Name(), v)
			provider.SetPriority(v)
		}
	}

	// Apply overridden proxy.
	if s, ok := provider.(mt.ProxySetter); ok && config.Has(proxyConfigKey) {
		if v, err := config.GetString(proxyConfigKey); err == nil {
			if err := s.SetProxy(v); err != nil {
				e.logger.Fatalf("Set proxy for %s provider '%s' error: %v", providerType, provider.Name(), err)
			}
			e.logger.Printf("Override %s provider proxy: %s=%s", providerType, provider.Name(), redactProxyForLog(v))
		}
	}

	// Apply request timeout.
	if s, ok := provider.(mt.RequestTimeoutSetter); ok && config.Has(timeoutConfigKey) {
		if v, err := config.GetDuration(timeoutConfigKey); err == nil {
			e.logger.Printf("Override %s provider request timeout: %s=%s", providerType, provider.Name(), v)
			s.SetRequestTimeout(v)
		}
	}

	// Apply full config.
	if s, ok := provider.(mt.ConfigSetter); ok {
		if err := s.SetConfig(config); err != nil {
			e.logger.Fatalf("Set %s provider config for %s: %v", providerType, provider.Name(), err)
		}
	}
}
