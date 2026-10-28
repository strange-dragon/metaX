package engine

import (
	"net/url"
	"testing"

	"golang.org/x/text/language"
)

// proxyStub is the smallest type that satisfies mt.Provider plus ProxySetter.
type proxyStub struct {
	name  string
	proxy string
	err   error
}

func (p *proxyStub) Name() string           { return p.name }
func (p *proxyStub) Priority() float64      { return 1 }
func (p *proxyStub) SetPriority(float64)    {}
func (p *proxyStub) Language() language.Tag { return language.Japanese }
func (p *proxyStub) URL() *url.URL          { return &url.URL{Scheme: "https", Host: "example.com"} }
func (p *proxyStub) SetProxy(raw string) error {
	if p.err != nil {
		return p.err
	}
	p.proxy = raw
	return nil
}

func TestApplyEnvProxySetsProviderProxy(t *testing.T) {
	t.Setenv("METAX_PROVIDER_PROXY", "socks5://user:pass@127.0.0.1:1080")
	e := &Engine{}
	e.initLogger()

	provider := &proxyStub{name: "stub"}
	e.applyEnvProxy("movie", provider)

	if provider.proxy != "socks5://user:pass@127.0.0.1:1080" {
		t.Fatalf("proxy = %q, want the METAX_PROVIDER_PROXY value", provider.proxy)
	}
}

func TestApplyEnvProxyIgnoresUnsetEnv(t *testing.T) {
	t.Setenv("METAX_PROVIDER_PROXY", "")
	e := &Engine{}
	e.initLogger()

	provider := &proxyStub{name: "stub", proxy: "http://keep:1"}
	e.applyEnvProxy("movie", provider)

	if provider.proxy != "http://keep:1" {
		t.Fatalf("proxy = %q, want the previous value untouched", provider.proxy)
	}
}

// A proxy a provider rejects must be logged, not fatal: one broken provider
// should never stop the server from starting.
func TestApplyEnvProxySwallowsProviderError(t *testing.T) {
	t.Setenv("METAX_PROVIDER_PROXY", "://bad")
	e := &Engine{}
	e.initLogger()

	provider := &proxyStub{name: "stub", err: errStubProxy}
	e.applyEnvProxy("actor", provider)
}

var errStubProxy = &stubProxyError{}

type stubProxyError struct{}

func (*stubProxyError) Error() string { return "unsupported proxy scheme" }
