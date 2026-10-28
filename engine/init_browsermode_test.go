package engine

import (
	"context"
	"net/url"
	"testing"

	"github.com/strange-dragon/metaX/internal/antibot"
	"golang.org/x/text/language"
)

// browserStub is the smallest provider that also implements the browser
// contracts, so the boot decision can be tested without a browser or a network.
type browserStub struct {
	name     string
	required bool
	detail   string

	probes  int
	allowed bool
	reason  string
}

func (b *browserStub) Name() string           { return b.name }
func (b *browserStub) Priority() float64      { return 1 }
func (b *browserStub) SetPriority(float64)    {}
func (b *browserStub) Language() language.Tag { return language.Japanese }
func (b *browserStub) URL() *url.URL          { return &url.URL{Scheme: "https", Host: "example.com"} }

func (b *browserStub) ProbeBrowserNeed(context.Context) (bool, string) {
	b.probes++
	return b.required, b.detail
}

func (b *browserStub) SetBrowserAllowed(allowed bool, reason string) {
	b.allowed, b.reason = allowed, reason
}

func TestBrowserModeResolution(t *testing.T) {
	cases := []struct {
		env    string
		warmup string
		want   string
	}{
		{"", "", antibot.BrowserModeAuto},
		{"auto", "", antibot.BrowserModeAuto},
		{"AUTO", "", antibot.BrowserModeAuto},
		{"off", "", antibot.BrowserModeOff},
		{"never", "", antibot.BrowserModeOff},
		{"disabled", "", antibot.BrowserModeOff},
		{"always", "", antibot.BrowserModeAlways},
		{"on", "", antibot.BrowserModeAlways},
		{"force", "", antibot.BrowserModeAlways},
		{"nonsense", "", antibot.BrowserModeAuto},
		{"", "0", antibot.BrowserModeOff},
		// An explicit mode wins over the legacy variable.
		{"always", "0", antibot.BrowserModeAlways},
	}
	for _, tc := range cases {
		t.Setenv("METAX_BROWSER_MODE", tc.env)
		t.Setenv("METAX_WARMUP_BROWSER", tc.warmup)
		if got := BrowserMode(); got != tc.want {
			t.Errorf("METAX_BROWSER_MODE=%q METAX_WARMUP_BROWSER=%q → %q, want %q", tc.env, tc.warmup, got, tc.want)
		}
	}
}

// The whole point of the probe: when a plain request already gets through, the
// 300 MB browser must not start.
func TestBrowserNeededSkipsBrowserWhenEgressPasses(t *testing.T) {
	e := &Engine{}
	e.initLogger()
	p := &browserStub{name: "stub", allowed: true, detail: "a plain TLS request got 200 OK without a challenge"}

	if e.browserNeeded(p, antibot.BrowserModeAuto) {
		t.Fatal("browserNeeded = true, want false when the probe saw no challenge")
	}
	if p.probes != 1 {
		t.Errorf("probes = %d, want exactly one probe at boot", p.probes)
	}
	if p.allowed {
		t.Error("provider still allows a browser after the probe said it is not needed")
	}
	if p.reason == "" {
		t.Error("reason must reach the provider, it is what the status endpoint reports")
	}
}

func TestBrowserNeededKeepsBrowserWhenChallenged(t *testing.T) {
	e := &Engine{}
	e.initLogger()
	p := &browserStub{name: "stub", required: true, detail: "a plain TLS request was served a challenge (HTTP 403)"}

	if !e.browserNeeded(p, antibot.BrowserModeAuto) {
		t.Fatal("browserNeeded = false, want true when the egress is challenged")
	}
	if !p.allowed {
		t.Error("provider must be allowed to start the browser when the probe saw a challenge")
	}
}

// A probe that cannot tell must keep the browser: losing it silently would
// remove the only way through for such a provider.
func TestBrowserNeededConservativeWithoutProber(t *testing.T) {
	e := &Engine{}
	e.initLogger()
	p := &proxyStub{name: "no-prober"}

	if !e.browserNeeded(p, antibot.BrowserModeAuto) {
		t.Fatal("browserNeeded = false for a provider that cannot probe, want true")
	}
}

func TestBrowserNeededModeOverridesProbe(t *testing.T) {
	e := &Engine{}
	e.initLogger()

	off := &browserStub{name: "off", required: true, allowed: true}
	if e.browserNeeded(off, antibot.BrowserModeOff) {
		t.Fatal("mode off must never start a browser")
	}
	if off.probes != 0 {
		t.Errorf("probes = %d in mode off, want 0 (the decision is already made)", off.probes)
	}
	if off.allowed {
		t.Error("mode off must switch the provider's browser off")
	}

	always := &browserStub{name: "always", required: false}
	if !e.browserNeeded(always, antibot.BrowserModeAlways) {
		t.Fatal("mode always must start the browser even if a probe would say otherwise")
	}
	if always.probes != 0 {
		t.Errorf("probes = %d in mode always, want 0", always.probes)
	}
	if !always.allowed {
		t.Error("mode always must allow the browser")
	}
}
