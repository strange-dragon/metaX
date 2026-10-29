package antibot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// A redirect to an origin the caller's guard rejects is an answer, not a
// transport failure. Rotating four TLS fingerprints and launching a browser
// against it cost nine seconds per retired thumbnail: DMM answers a delisted
// cover with a 302 to its "now printing" placeholder on imgsrc.dmm.com.
func TestRefusedRedirectStopsAfterOneAttempt(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "https://imgsrc.invalid/placeholder.jpg", http.StatusFound)
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	fetcher := New(Options{
		Timeout:        5 * time.Second,
		AllowURL:       func(u *url.URL) bool { return u.Hostname() == base.Hostname() },
		DisableBrowser: true,
	})
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/cover.jpg", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	start := time.Now()
	resp, err := fetcher.Do(req)
	if resp != nil {
		resp.Body.Close()
		t.Fatal("expected the redirect to be refused")
	}
	if !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("want ErrRedirectRefused, got %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("origin saw %d requests, want exactly 1: the refusal was retried", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("refusal took %v, want an immediate return", elapsed)
	}
	// The error text carries "chromium fallback" on the browser path, and the
	// challenge detector matches that phrase. A refused redirect must never be
	// mistaken for an edge challenge, or callers report a block that never
	// happened.
	if IsChallengeError(err) {
		t.Fatal("a refused redirect must not be classified as a challenge")
	}
}
