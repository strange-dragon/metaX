// Copyright 2026 metaX contributors.

package antibot

import (
	"context"
	"testing"
	"time"
)

// A hanging handshake must not eat the budget the browser fallback needs: four
// profiles at a 20s timeout used to make one request take 100s.
func TestAttemptBudgetSplitsTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 20s left, 60% of it for fingerprints, shared by 4 profiles.
	got := attemptBudget(ctx, 30*time.Second, 4, fingerprintShare)
	want := 3 * time.Second
	if got < want-200*time.Millisecond || got > want+200*time.Millisecond {
		t.Fatalf("attemptBudget = %s, want about %s", got, want)
	}

	// The browser gets the whole remaining deadline, bounded by the limit.
	if got := attemptBudget(ctx, 10*time.Second, 1, 1); got != 10*time.Second {
		t.Fatalf("browser budget = %s, want the 10s limit", got)
	}
}

func TestAttemptBudgetWithoutDeadlineKeepsLimit(t *testing.T) {
	got := attemptBudget(context.Background(), 15*time.Second, 3, fingerprintShare)
	if got != 15*time.Second {
		t.Fatalf("attemptBudget = %s, want the configured 15s", got)
	}
}

func TestAttemptBudgetExpired(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if got := attemptBudget(ctx, 10*time.Second, 1, 1); got != 0 {
		t.Fatalf("attemptBudget = %s, want 0 once the deadline passed", got)
	}
}

func TestAttemptBudgetDefaultsZeroLimit(t *testing.T) {
	if got := attemptBudget(context.Background(), 0, 1, 1); got != 20*time.Second {
		t.Fatalf("attemptBudget = %s, want the 20s default", got)
	}
}

// A fetcher configured without a browser must not spawn one just because the
// server starts its supervisor at boot.
func TestKeepWarmSkipsDisabledBrowser(t *testing.T) {
	f := New(Options{Timeout: 2 * time.Second, DisableBrowser: true})

	done := make(chan error, 1)
	go func() { done <- f.KeepWarm(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("KeepWarm = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("KeepWarm did not return; it must not wait when the browser is disabled")
	}
	if status := f.BrowserStatus(); status.Running {
		t.Fatalf("running=%v want false", status.Running)
	}
}

func TestBrowserStatusBeforeAnyStart(t *testing.T) {
	f := New(Options{Timeout: time.Second})
	status := f.BrowserStatus()
	if status.Running || status.Supervised || status.Starts != 0 {
		t.Fatalf("status=%+v want a zero state", status)
	}
}
