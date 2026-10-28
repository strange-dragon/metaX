package engine

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/strange-dragon/metaX/common/fetch"
	"github.com/strange-dragon/metaX/internal/antibot"
	mt "github.com/strange-dragon/metaX/provider"
)

const (
	// browserProbeTimeout bounds the boot probe. It runs in the background, but
	// a hung probe would delay the browser start behind it.
	browserProbeTimeout = 15 * time.Second
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

// warmBrowsers decides, per provider, whether a fallback browser is needed and
// then either supervises it or switches it off.
//
// The browser exists for one reason: to execute a JavaScript challenge. When the
// egress already passes (a residential proxy, an IP Cloudflare does not score, a
// valid cf_clearance cookie) a plain TLS request returns the real page and the
// browser is nothing but 200-400 MB of resident memory. On a 512 MB instance
// that memory is the difference between running and being OOM-killed, so the
// decision is made from evidence: providers that can tell are asked once at
// boot, and only a clear "challenged" answer keeps the browser.
//
// METAX_BROWSER_MODE overrides the decision: "auto" (default) probes, "off"
// never starts a browser, "always" skips the probe and starts it.
func (e *Engine) warmBrowsers() {
	if flag.Lookup("test.v") != nil {
		// Unit tests must never spawn a browser.
		return
	}
	mode := BrowserMode()
	providers := e.GetMovieProviders()
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	// Decide synchronously, start asynchronously. A request that arrived while
	// the probe was still running would find browserAllowed=true and start the
	// very browser the probe is about to switch off — the probe costs a couple
	// of seconds at boot, which is cheaper than paying 300 MB for a race.
	wanted := make([]mt.Provider, 0, len(names))
	for _, name := range names {
		provider := providers[name]
		_, isKeeper := provider.(mt.BrowserKeeper)
		_, isWarmer := provider.(mt.BrowserWarmer)
		if !isKeeper && !isWarmer {
			continue
		}
		if e.browserNeeded(provider, mode) {
			wanted = append(wanted, provider)
		}
	}

	go func() {
		for _, provider := range wanted {
			if keeper, ok := provider.(mt.BrowserKeeper); ok {
				go e.keepBrowser(provider, keeper)
				continue
			}
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

// BrowserMode resolves METAX_BROWSER_MODE. It is re-exported here so routes and
// tools report the same decision the engine made.
func BrowserMode() string { return antibot.BrowserMode() }

// browserNeeded applies the mode to one provider and reports whether its
// browser should be started.
func (e *Engine) browserNeeded(provider mt.Provider, mode string) bool {
	setter, canSet := provider.(mt.BrowserAllowedSetter)

	if mode == antibot.BrowserModeOff {
		if canSet {
			setter.SetBrowserAllowed(false, "METAX_BROWSER_MODE=off")
		}
		e.logger.Printf("Fallback browser for %s is off (METAX_BROWSER_MODE=off)", provider.Name())
		return false
	}
	if mode == antibot.BrowserModeAlways {
		if canSet {
			setter.SetBrowserAllowed(true, "METAX_BROWSER_MODE=always")
		}
		return true
	}

	prober, ok := provider.(mt.BrowserProber)
	if !ok {
		return true
	}
	// Say so before the probe: it runs during boot, so a slow start should be
	// explained by the log rather than look like a hang.
	e.logger.Printf("Probing whether %s needs the fallback browser", provider.Name())
	ctx, cancel := context.WithTimeout(context.Background(), browserProbeTimeout)
	required, detail := prober.ProbeBrowserNeed(ctx)
	cancel()
	if canSet {
		setter.SetBrowserAllowed(required, detail)
	}
	if required {
		e.logger.Printf("Fallback browser required for %s: %s", provider.Name(), detail)
		return true
	}
	e.logger.Printf("Fallback browser not needed for %s: %s; Chromium will not be started", provider.Name(), detail)
	return false
}

// keepBrowser runs a provider's browser supervisor and reports when the
// browser first becomes available, so the deployment log shows the state
// without anyone having to call the status endpoint.
func (e *Engine) keepBrowser(provider mt.Provider, keeper mt.BrowserKeeper) {
	go func() {
		// Wait for the supervisor's first start in the background; the keeper
		// itself blocks for the life of the process.
		if reporter, ok := provider.(mt.BrowserReporter); ok {
			start := time.Now()
			for i := 0; i < 180; i++ {
				if reporter.BrowserStatus().Running {
					e.logger.Printf("Browser ready for %s after %s (kept running)", provider.Name(), time.Since(start).Round(time.Second))
					return
				}
				time.Sleep(time.Second)
			}
			e.logger.Printf("Browser for %s is not running after 3m: %s", provider.Name(), reporter.BrowserStatus().LastError)
		}
	}()
	// The keeper lives as long as the process; there is nothing to cancel it
	// with, so an error here is only worth a log line.
	if err := keeper.KeepBrowser(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Printf("Browser keeper for %s stopped: %v", provider.Name(), err)
	}
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
