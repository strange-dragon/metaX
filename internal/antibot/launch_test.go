package antibot

import (
	"testing"

	"github.com/chromedp/cdproto/network"
)

// Images, fonts and media are never read by a scraper, and blocking them is
// what keeps the resident browser inside a small instance's memory.
func TestBlockedResource(t *testing.T) {
	cases := []struct {
		kind network.ResourceType
		want bool
	}{
		{network.ResourceTypeImage, true},
		{network.ResourceTypeMedia, true},
		{network.ResourceTypeFont, true},
		{network.ResourceTypeStylesheet, false},
		{network.ResourceTypeScript, false},
		{network.ResourceTypeDocument, false},
	}
	for _, tc := range cases {
		if got := blockedResource(tc.kind); got != tc.want {
			t.Errorf("blockedResource(%s) = %v, want %v", tc.kind, got, tc.want)
		}
	}

	t.Setenv("METAX_CHROMIUM_BLOCK_MEDIA", "0")
	if blockedResource(network.ResourceTypeImage) {
		t.Error("images must not be blocked when METAX_CHROMIUM_BLOCK_MEDIA=0")
	}
}

func TestLowMemoryChromiumOverride(t *testing.T) {
	t.Setenv("METAX_CHROMIUM_LOW_MEMORY", "0")
	if lowMemoryChromium() {
		t.Error("METAX_CHROMIUM_LOW_MEMORY=0 must disable the low-memory profile")
	}
	t.Setenv("METAX_CHROMIUM_LOW_MEMORY", "1")
	if !lowMemoryChromium() {
		t.Error("METAX_CHROMIUM_LOW_MEMORY=1 must enable the low-memory profile")
	}
}

func TestChromiumJSHeapDefault(t *testing.T) {
	t.Setenv("METAX_CHROMIUM_JS_HEAP_MB", "")
	if got := chromiumJSHeapMB(); got != 192 {
		t.Fatalf("chromiumJSHeapMB = %d, want 192", got)
	}
	t.Setenv("METAX_CHROMIUM_JS_HEAP_MB", "32")
	if got := chromiumJSHeapMB(); got != 192 {
		t.Fatalf("chromiumJSHeapMB = %d, want the 192 default for an unusable value", got)
	}
	t.Setenv("METAX_CHROMIUM_JS_HEAP_MB", "256")
	if got := chromiumJSHeapMB(); got != 256 {
		t.Fatalf("chromiumJSHeapMB = %d, want 256", got)
	}
}
