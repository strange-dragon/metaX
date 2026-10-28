package antibot

import (
	"context"
	"testing"
	"time"
)

// DisableBrowserFallback has to be permanent. The supervisor exists to restart
// a browser that disappeared, so a bare Close would be read as a crash and
// answered with a fresh Chromium a second later — the opposite of the intent.
func TestDisableBrowserFallbackStopsSupervisor(t *testing.T) {
	f := New(Options{Timeout: time.Second})
	f.DisableBrowserFallback()

	done := make(chan error, 1)
	go func() { done <- f.KeepWarm(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("KeepWarm = %v, want nil once the fallback is disabled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("KeepWarm did not return; a disabled fallback must not keep the supervisor alive")
	}

	if status := f.BrowserStatus(); status.Running {
		t.Fatal("browser is running after the fallback was disabled")
	}
}
