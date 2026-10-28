package antibot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestLaunchOptionsSurviveChromedpFlagValidation walks the real launch option
// set through chromedp's Allocate, which is the only place that validates the
// *type* of every flag value.
//
// That validation is late and silent until it runs: a flag whose value is
// neither string nor bool is accepted when the option is built, then rejected
// inside Allocate with "invalid exec pool flag". Because Allocate only runs on
// a host that actually launches a browser, an int renderer-process-limit
// passed every local test and still stopped the deployed browser from starting.
// This test reaches the same code path with a stub executable, so a wrong flag
// type fails here instead of in production.
func TestLaunchOptionsSurviveChromedpFlagValidation(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "chromium-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub browser: %v", err)
	}

	t.Setenv("METAX_CHROMIUM_BIN", stub)
	// Force both branches that add the fragile flags: the low-memory profile
	// (renderer-process-limit, js-flags) and the container flags (no-sandbox).
	t.Setenv("METAX_CHROMIUM_LOW_MEMORY", "1")
	t.Setenv("METAX_CHROMIUM_NO_SANDBOX", "1")
	t.Setenv("METAX_CHROMIUM_HEADED", "")
	t.Setenv("METAX_CHROMIUM_JS_HEAP_MB", "192")

	opts, cleanup, err := BrowserLaunchOptions()
	if err != nil {
		t.Fatalf("BrowserLaunchOptions: %v", err)
	}
	defer cleanup()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	ctx, cancel := context.WithTimeout(allocCtx, 1500*time.Millisecond)
	defer cancel()

	allocator := chromedp.FromContext(ctx)
	if allocator == nil || allocator.Allocator == nil {
		t.Fatal("no allocator in context")
	}
	// The stub exits immediately without a DevTools endpoint, so this is
	// expected to fail — just never on the flag check.
	_, allocErr := allocator.Allocator.Allocate(ctx)
	if allocErr != nil && strings.Contains(allocErr.Error(), "invalid exec pool flag") {
		t.Fatalf("chromedp rejected a launch flag type: %v", allocErr)
	}
}
