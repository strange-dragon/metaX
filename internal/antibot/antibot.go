// Copyright 2026 metaX contributors.
//
// Package antibot provides the shared fetch layer used by providers whose live
// sites reject plain Go HTTP clients. It combines browser TLS fingerprints
// (via tls-client) with a Chromium fallback for JavaScript challenges.
//
// It is deliberately provider-agnostic: callers supply the origin guard, the
// cookies, the proxy and the timeout. The package never decides which site is
// allowed to be scraped.
//
// Design notes:
//   - Fingerprint rotation only helps against UA/TLS-based rejection. It does
//     not clear an interactive Cloudflare challenge, which is why the Chromium
//     fallback exists.
//   - The Chromium fallback is lazily started and reused. A single browser is
//     shared per Fetcher, serialized by a mutex.
//   - Bodies are buffered (bounded) so callers can both inspect a challenge
//     page and parse the successful response.
package antibot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	fhttpcookiejar "github.com/bogdanfinn/fhttp/cookiejar"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/publicsuffix"
)

// DefaultMaxBodyBytes bounds a fetched document. Providers that need more can
// raise Options.MaxBodyBytes.
const DefaultMaxBodyBytes = 8 << 20

// Profile pairs a TLS ClientHello profile with the matching browser
// User-Agent. Profiles are attempted in order.
type Profile struct {
	Name      string
	Profile   profiles.ClientProfile
	UserAgent string
}

// DefaultProfiles lists recent Chrome/Firefox profiles. Newest first: several
// sites behind bot management accept only the newest handshakes.
var DefaultProfiles = []Profile{
	{Name: "Chrome 146", Profile: profiles.Chrome_146, UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"},
	{Name: "Chrome 144", Profile: profiles.Chrome_144, UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"},
	{Name: "Chrome 133", Profile: profiles.Chrome_133, UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"},
	{Name: "Firefox 135", Profile: profiles.Firefox_135, UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:135.0) Gecko/20100101 Firefox/135.0"},
}

// Options configures a Fetcher. Only Timeout has a default.
type Options struct {
	// Timeout bounds each fingerprint attempt and each browser navigation.
	Timeout time.Duration
	// ProxyURL is an http/https/socks5 proxy. Empty falls back to the process
	// proxy environment variables.
	ProxyURL string
	// Cookies is a raw Cookie header value. CR/LF is rejected by the caller.
	Cookies string
	// AllowURL guards redirect targets and the final browser URL. A nil guard
	// allows every http/https origin.
	AllowURL func(*url.URL) bool
	// Profiles overrides DefaultProfiles.
	Profiles []Profile
	// MaxBodyBytes overrides DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// DisableBrowser disables the Chromium fallback, for tests and minimal
	// deployments.
	DisableBrowser bool
	// SkipFingerprint sends the request straight to the browser. Callers that
	// already rotated their own TLS profiles use it so the challenge is not
	// retried by a rotation that was always going to fail again.
	SkipFingerprint bool
}

// Fetcher performs fingerprint-rotating requests with an optional Chromium
// fallback. It is safe for concurrent use.
type Fetcher struct {
	mu       sync.Mutex
	opts     Options
	jar      fhttp.CookieJar
	browser  browserState
	failures []string
	// starting is non-nil while a Chromium cold start is in flight. Requests
	// that arrive during the warm-up wait on it for a bounded time instead of
	// blocking on the fetcher lock or starting a second browser.
	starting chan struct{}
	startErr error
	// Bookkeeping for BrowserStatus. Guarded by mu.
	starts      int
	restarts    int
	lastStartMS int64
	supervised  bool
}

type browserState struct {
	ctx      context.Context
	stop     context.CancelFunc
	alloc    context.CancelFunc
	display  context.CancelFunc
	proxyKey string
	started  time.Time
}

// BrowserStatus is a snapshot of the fallback browser, safe to expose over
// HTTP: it holds no proxy credentials and no page content.
type BrowserStatus struct {
	Running     bool   `json:"running"`
	UptimeMS    int64  `json:"uptime_ms,omitempty"`
	Starts      int    `json:"starts"`
	Restarts    int    `json:"restarts"`
	Supervised  bool   `json:"supervised"`
	LastError   string `json:"last_error,omitempty"`
	StartupTime int64  `json:"last_start_ms,omitempty"`
}

// New creates a Fetcher. The Chromium browser is not started until a
// challenge is detected.
func New(opts Options) *Fetcher {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if len(opts.Profiles) == 0 {
		opts.Profiles = DefaultProfiles
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	f := &Fetcher{opts: opts}
	if jar, err := fhttpcookiejar.New(&fhttpcookiejar.Options{PublicSuffixList: publicsuffix.List}); err == nil {
		f.jar = jar
	}
	return f
}

// SetOptions updates the mutable parts of the configuration. It does not
// interrupt an in-flight request.
func (f *Fetcher) SetOptions(update func(*Options)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	update(&f.opts)
}

// Options returns a copy of the current options.
func (f *Fetcher) Options() Options {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opts
}

// Close releases the Chromium browser, if one was started.
func (f *Fetcher) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopBrowserLocked()
}

// DisableBrowserFallback switches the browser off for good: the supervisor
// stops restarting it and no later request starts one.
//
// Closing alone would not do it. The supervisor exists precisely to restart a
// browser that disappeared, so a Close from outside would be read as a crash
// and answered with a fresh Chromium a second later. The flag has to be set
// first.
func (f *Fetcher) DisableBrowserFallback() {
	f.mu.Lock()
	f.opts.DisableBrowser = true
	f.mu.Unlock()
	f.Close()
}

// browserWarmupTimeout is generous on purpose: warming up happens off the
// request path, where a slow cold start costs nothing but boot time. It is
// still bounded, because a start that is in flight makes later requests wait:
// three minutes of that would look like an outage.
const browserWarmupTimeout = 90 * time.Second

// WarmBrowser starts the fallback browser before the first request needs it.
//
// Cold start is the expensive part: on a 512 MB shared instance Chromium took
// close to a minute to open its DevTools endpoint, which no per-request timeout
// can absorb. Paying that at boot is what lets the first scrape succeed.
//
// The start runs without the fetcher lock held, otherwise every request that
// arrives during the warm-up would sit in an untimed mutex wait — which the
// edge gateway then reports as a 504 with no error at all.
func (f *Fetcher) WarmBrowser(ctx context.Context) error {
	f.mu.Lock()
	if f.browser.ctx != nil {
		if f.browser.ctx.Err() == nil {
			f.mu.Unlock()
			return nil
		}
		f.stopBrowserLocked()
	}
	if waiter := f.starting; waiter != nil {
		f.mu.Unlock()
		select {
		case <-waiter:
			f.mu.Lock()
			err := f.startErr
			f.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timeout := browserWarmupTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline); left > 0 && left < timeout {
			timeout = left
		}
	}
	opts := f.opts
	waiter := make(chan struct{})
	f.starting = waiter
	f.mu.Unlock()

	err := f.startBrowserLocked(opts, timeout)

	f.mu.Lock()
	f.startErr = err
	f.starting = nil
	close(waiter)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	debugf("browser warmed up in advance")
	return nil
}

// awaitBrowserStart waits for an in-flight Chromium cold start to settle,
// bounded by ctx so a request never outlives its own budget waiting for a
// browser that may never come up.
func (f *Fetcher) awaitBrowserStart(ctx context.Context) error {
	f.mu.Lock()
	waiter := f.starting
	f.mu.Unlock()
	if waiter == nil {
		return nil
	}
	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waited for the browser cold start: %w", ctx.Err())
	}
}

// Do performs req with browser TLS fingerprints, rotating profiles on a
// rejection, then falls back to Chromium when every profile is challenged.
// The returned response body is buffered and already closed for reuse.
func (f *Fetcher) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	opts := f.opts
	f.failures = nil
	f.mu.Unlock()

	if !validRequestURL(req.URL, opts.AllowURL) {
		return nil, fmt.Errorf("antibot: refusing request to %q", req.URL)
	}
	body, err := replayableBody(req)
	if err != nil {
		return nil, err
	}

	if cookie := strings.TrimSpace(opts.Cookies); cookie != "" && !strings.ContainsAny(cookie, "\r\n") {
		if existing := req.Header.Get("Cookie"); existing != "" {
			req.Header.Set("Cookie", existing+"; "+cookie)
		} else {
			req.Header.Set("Cookie", cookie)
		}
	}

	var lastErr error
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = DefaultProfiles
	}
	// Callers that already ran their own TLS profiles (javlibrary does) must not
	// pay for a second rotation here: that is another four attempts at a timeout
	// each, which is how a single page fetch reached 100s.
	if !opts.SkipFingerprint {
		for i, profile := range profiles {
			if i > 0 {
				if err := sleepContext(req.Context(), 800*time.Millisecond); err != nil {
					return nil, err
				}
			}
			// Share what is left of the caller's deadline across the attempts
			// that are still to come. Without this, four profiles times a
			// per-attempt timeout turn one 20s request into a 100s hang and the
			// Chromium fallback never gets a turn.
			attemptOpts := opts
			attemptOpts.Timeout = attemptBudget(req.Context(), opts.Timeout, len(profiles)-i, fingerprintShare)
			if attemptOpts.Timeout < time.Second {
				lastErr = errDeadlineExhausted
				break
			}
			resp, payload, finalURL, err := f.attemptFingerprint(req, body, profile, attemptOpts)
			if err != nil {
				lastErr = err
				f.recordFailure(profile.Name + ": " + err.Error())
				continue
			}
			stdResp := responseWithBody(resp, payload, finalURL)
			if LooksLikeChallenge(stdResp.StatusCode, stdResp.Header, payload) {
				lastErr = fmt.Errorf("%s: HTTP %d challenge page", profile.Name, stdResp.StatusCode)
				f.recordFailure(lastErr.Error())
				// A JavaScript challenge is answered by executing it, not by
				// shaking a different TLS handshake at it: every remaining
				// profile returns the same interstitial. Rotating anyway just
				// burns the budget the browser needs.
				break
			}
			if isRejected(stdResp.StatusCode) {
				lastErr = fmt.Errorf("%s: rejected with HTTP %d", profile.Name, stdResp.StatusCode)
				f.recordFailure(lastErr.Error())
				continue
			}
			debugf("%s answered %s %s with HTTP %d", profile.Name, req.Method, req.URL, stdResp.StatusCode)
			return stdResp, nil
		}
	}

	if opts.DisableBrowser {
		return nil, f.exhaustedError("browser fallback disabled", lastErr)
	}
	browserOpts := opts
	browserOpts.Timeout = attemptBudget(req.Context(), opts.Timeout, 1, 1)
	if browserOpts.Timeout < time.Second {
		return nil, f.exhaustedError("no time left for the Chromium fallback", lastErr)
	}
	resp, err := f.fetchWithBrowser(req, browserOpts)
	if err == nil {
		debugf("headless Chromium answered %s %s (%d bytes)", req.Method, req.URL, resp.ContentLength)
		return resp, nil
	}
	return nil, f.exhaustedError(fmt.Sprintf("Chromium fallback failed: %v", err), lastErr)
}

// fingerprintShare is the fraction of the remaining deadline the TLS profile
// attempts may spend. The rest is reserved for the Chromium fallback, which is
// the only strategy that can clear a challenge and therefore must not be
// starved by profiles that were always going to be rejected.
const fingerprintShare = 0.6

// errDeadlineExhausted reports that the caller's deadline left no room for
// another attempt.
var errDeadlineExhausted = errors.New("antibot: request deadline exhausted before the browser fallback")

// attemptBudget returns how long one strategy attempt may run. It is the
// configured per-attempt timeout, further capped by the caller's remaining
// deadline split across the attempts still to come and the strategy's share of
// that budget. A context without a deadline keeps the configured timeout.
func attemptBudget(ctx context.Context, limit time.Duration, remaining int, share float64) time.Duration {
	if limit <= 0 {
		limit = 20 * time.Second
	}
	if remaining < 1 {
		remaining = 1
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return limit
	}
	left := time.Until(deadline)
	if left <= 0 {
		return 0
	}
	perAttempt := time.Duration(float64(left) * share / float64(remaining))
	return min(limit, perAttempt)
}

// debugf traces which strategy answered a request. It is off unless
// METAX_ANTIBOT_DEBUG is set, so normal runs stay quiet.
func debugf(format string, args ...any) {
	if strings.TrimSpace(os.Getenv("METAX_ANTIBOT_DEBUG")) == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[antibot] "+format+"\n", args...)
}

func (f *Fetcher) recordFailure(detail string) {
	f.mu.Lock()
	f.failures = append(f.failures, detail)
	f.mu.Unlock()
}

func (f *Fetcher) exhaustedError(reason string, lastErr error) error {
	f.mu.Lock()
	failures := strings.Join(f.failures, "; ")
	f.mu.Unlock()
	switch {
	case failures != "" && lastErr != nil:
		return fmt.Errorf("antibot: all fetch strategies failed (%s); last error: %w; attempts: %s", reason, lastErr, failures)
	case failures != "":
		return fmt.Errorf("antibot: all fetch strategies failed (%s); attempts: %s", reason, failures)
	case lastErr != nil:
		return fmt.Errorf("antibot: all fetch strategies failed (%s): %w", reason, lastErr)
	default:
		return fmt.Errorf("antibot: all fetch strategies failed (%s)", reason)
	}
}

func (f *Fetcher) attemptFingerprint(req *http.Request, body []byte, profile Profile, opts Options) (*fhttp.Response, []byte, *url.URL, error) {
	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeoutSeconds(max(1, int(opts.Timeout.Seconds()))),
		tlsclient.WithClientProfile(profile.Profile),
		tlsclient.WithNotFollowRedirects(),
	}
	if proxyURL := f.proxyFor(req, opts); proxyURL != "" {
		options = append(options, tlsclient.WithProxyUrl(proxyURL))
	}
	if f.jar != nil {
		options = append(options, tlsclient.WithCookieJar(f.jar))
	}
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, nil, nil, err
	}
	defer client.CloseIdleConnections()

	resp, finalURL, err := f.doFingerprint(client, req, body, profile.UserAgent, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBodyBytes+1))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(payload)) > opts.MaxBodyBytes {
		return nil, nil, nil, fmt.Errorf("response exceeds %d bytes", opts.MaxBodyBytes)
	}
	return resp, payload, finalURL, nil
}

func (f *Fetcher) proxyFor(req *http.Request, opts Options) string {
	if opts.ProxyURL != "" {
		return opts.ProxyURL
	}
	if envProxy, err := http.ProxyFromEnvironment(req); err == nil && envProxy != nil {
		return envProxy.String()
	}
	return ""
}

// doFingerprint issues the request and follows redirects manually so the
// origin guard applies to every hop. tls-client is configured to not follow
// redirects itself.
func (f *Fetcher) doFingerprint(client tlsclient.HttpClient, original *http.Request, body []byte, userAgent string, opts Options) (*fhttp.Response, *url.URL, error) {
	current := *original.URL
	method := original.Method
	payload := body
	previous := ""
	for redirects := 0; ; redirects++ {
		var reader io.Reader
		if len(payload) > 0 {
			reader = bytes.NewReader(payload)
		}
		req, err := fhttp.NewRequestWithContext(original.Context(), method, current.String(), reader)
		if err != nil {
			return nil, nil, err
		}
		if len(payload) > 0 {
			req.ContentLength = int64(len(payload))
		}
		for key, values := range original.Header {
			req.Header[key] = append([]string(nil), values...)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		if previous != "" {
			req.Header.Set("Referer", previous)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		isRedirect := resp.StatusCode == http.StatusMovedPermanently ||
			resp.StatusCode == http.StatusFound ||
			resp.StatusCode == http.StatusSeeOther ||
			resp.StatusCode == http.StatusTemporaryRedirect ||
			resp.StatusCode == http.StatusPermanentRedirect
		location := resp.Header.Get("Location")
		if !isRedirect || location == "" {
			final := current
			return resp, &final, nil
		}
		if redirects >= 10 {
			_ = resp.Body.Close()
			return nil, nil, errors.New("stopped after 10 redirects")
		}
		reference, err := url.Parse(location)
		if err != nil {
			_ = resp.Body.Close()
			return nil, nil, fmt.Errorf("parse redirect location: %w", err)
		}
		next := current.ResolveReference(reference)
		if !validRequestURL(next, opts.AllowURL) {
			_ = resp.Body.Close()
			return nil, nil, fmt.Errorf("refusing redirect to unexpected origin %q", next)
		}
		_ = resp.Body.Close()
		// 301/302/303 degrade a POST into a GET, matching browser and net/http
		// behaviour. Kept explicit because tls-client redirects are manual here.
		if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				method = http.MethodGet
				payload = nil
			}
		}
		previous = current.String()
		current = *next
	}
}

// replayableBody buffers the request body once so every fingerprint attempt and
// the browser fallback send the same payload.
func replayableBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("antibot: request body cannot be replayed")
	}
	reader, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("antibot: replay request body: %w", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("antibot: read request body: %w", err)
	}
	return body, nil
}

func validRequestURL(u *url.URL, allow func(*url.URL) bool) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	if allow == nil {
		return true
	}
	return allow(u)
}

func responseWithBody(template *fhttp.Response, body []byte, finalURL *url.URL) *http.Response {
	request := &http.Request{Method: http.MethodGet}
	if finalURL != nil {
		request.URL = finalURL
	} else if template.Request != nil {
		request.Method = template.Request.Method
		request.URL = template.Request.URL
	}
	header := make(http.Header, len(template.Header))
	for key, values := range template.Header {
		header[key] = append([]string(nil), values...)
	}
	return &http.Response{
		Status:        template.Status,
		StatusCode:    template.StatusCode,
		Proto:         "HTTP/1.1",
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}

func isRejected(status int) bool {
	switch status {
	case http.StatusForbidden, http.StatusNotAcceptable, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

// LooksLikeChallenge reports whether a response is a bot-management
// interstitial rather than the requested document.
func LooksLikeChallenge(status int, header http.Header, body []byte) bool {
	if header != nil {
		if strings.EqualFold(strings.TrimSpace(header.Get("cf-mitigated")), "challenge") {
			return true
		}
	}
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable && status != http.StatusOK {
		return false
	}
	lower := strings.ToLower(string(body))
	if lower == "" {
		return false
	}
	markers := []string{
		"just a moment", "checking your browser", "请稍候", "cf-chl", "cf_chl",
		"challenge-platform", "enable javascript and cookies to continue",
		"attention required! | cloudflare", "error code: 1005", "error code: 1020",
		"web page blocked",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// IsChallengeError reports whether err was produced by a challenge or block
// page rather than a transport failure.
func IsChallengeError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"just a moment", "checking your browser", "请稍候", "cf-chl",
		"challenge", "chromium fallback", "error code: 1005", "error code: 1020",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fetchWithBrowser renders req in Chromium so an interactive challenge can
// run, then captures the resulting document.
func (f *Fetcher) fetchWithBrowser(original *http.Request, opts Options) (*http.Response, error) {
	if err := f.awaitBrowserStart(original.Context()); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.startBrowserLocked(opts, startShareOfTimeout(opts.Timeout)); err != nil {
		return nil, err
	}
	// The navigation budget must come out of what is left of the caller's
	// deadline, not out of the configured timeout. The browser context is
	// detached from the request, so using opts.Timeout here let a cold start
	// plus a navigation run to nearly twice the caller's budget — which is how
	// a request that the provider thought was capped at 40s still reached a
	// 100s gateway timeout.
	navigation := opts.Timeout
	if deadline, ok := original.Context().Deadline(); ok {
		if left := time.Until(deadline); left < navigation {
			navigation = left
		}
	}
	if navigation < 3*time.Second {
		return nil, fmt.Errorf("browser: only %s left of the request budget, not enough to load the page", navigation.Round(time.Second))
	}
	ctx, cancel := context.WithTimeout(f.browser.ctx, navigation)
	defer cancel()

	var (
		finalURL string
		title    string
		html     string
		statusMu sync.Mutex
		status   int
	)
	chromedp.ListenTarget(f.browser.ctx, func(event any) {
		switch e := event.(type) {
		case *network.EventResponseReceived:
			if e.Type == network.ResourceTypeDocument {
				statusMu.Lock()
				status = int(e.Response.Status)
				statusMu.Unlock()
			}
		case *fetch.EventRequestPaused:
			paused := e
			go func() {
				if blockedResource(paused.ResourceType) {
					// No image, font or media byte is needed to read the
					// article, and skipping them is what keeps the resident
					// browser inside a small instance's memory: a gallery page
					// alone is several megabytes of decode buffers.
					_ = chromedp.Run(f.browser.ctx, fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient))
					return
				}
				if paused.ResourceType == network.ResourceTypeDocument {
					target, err := url.Parse(paused.Request.URL)
					if err != nil || !validRequestURL(target, opts.AllowURL) && !isChallengeHost(target) {
						_ = chromedp.Run(f.browser.ctx, fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient))
						return
					}
				}
				_ = chromedp.Run(f.browser.ctx, fetch.ContinueRequest(paused.RequestID))
			}()
		}
	})

	run := func(stage string, actions ...chromedp.Action) error {
		if err := chromedp.Run(ctx, actions...); err != nil {
			return fmt.Errorf("%s: %w", stage, err)
		}
		return nil
	}
	if err := run("enable network", network.Enable()); err != nil {
		return nil, err
	}
	patterns := []*fetch.RequestPattern{{URLPattern: "*", ResourceType: network.ResourceTypeDocument}}
	if blockHeavyResources() {
		patterns = append(patterns,
			&fetch.RequestPattern{URLPattern: "*", ResourceType: network.ResourceTypeImage},
			&fetch.RequestPattern{URLPattern: "*", ResourceType: network.ResourceTypeMedia},
			&fetch.RequestPattern{URLPattern: "*", ResourceType: network.ResourceTypeFont},
		)
	}
	if err := run("enable navigation guard", fetch.Enable().WithPatterns(patterns)); err != nil {
		return nil, err
	}
	var cookieActions []chromedp.Action
	for _, cookie := range ParseCookieHeader(opts.Cookies) {
		cookieActions = append(cookieActions, network.SetCookie(cookie.Name, cookie.Value).WithURL(original.URL.String()).WithPath("/"))
	}
	if len(cookieActions) > 0 {
		if err := run("set configured cookies", cookieActions...); err != nil {
			return nil, err
		}
	}
	if err := run("navigate", chromedp.Navigate(original.URL.String())); err != nil {
		return nil, err
	}
	if _, err := waitForDocumentClear(ctx, opts.Timeout); err != nil {
		diagnostic := struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			HTML  string `json:"html"`
		}{}
		diagnosticCtx, diagnosticCancel := context.WithTimeout(f.browser.ctx, 3*time.Second)
		diagnosticErr := chromedp.Run(diagnosticCtx, chromedp.Evaluate(`({url: location.href, title: document.title, html: document.documentElement.outerHTML})`, &diagnostic))
		diagnosticCancel()
		return nil, fmt.Errorf("%w (challenge state: url=%q title=%q status=%d diagnostic_error=%v body=%q)", err, diagnostic.URL, diagnostic.Title, status, diagnosticErr, truncateCollapse(diagnostic.HTML, 600))
	}
	if err := run("capture page", chromedp.Evaluate(`({url: location.href, title: document.title, html: document.documentElement.outerHTML})`, &struct {
		URL   *string `json:"url"`
		Title *string `json:"title"`
		HTML  *string `json:"html"`
	}{URL: &finalURL, Title: &title, HTML: &html})); err != nil {
		return nil, err
	}
	parsedFinal, err := url.Parse(finalURL)
	if err != nil || !validRequestURL(parsedFinal, opts.AllowURL) {
		return nil, fmt.Errorf("browser ended at unexpected URL %q", finalURL)
	}
	if LooksLikeChallenge(http.StatusOK, nil, []byte(title)) || strings.TrimSpace(html) == "" {
		return nil, fmt.Errorf("browser did not clear the challenge (title %q)", title)
	}
	statusMu.Lock()
	statusCode := status
	statusMu.Unlock()
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("browser page returned HTTP status %d (title %q, body %q)", statusCode, title, truncateCollapse(html, 600))
	}
	body := []byte(html)
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       original,
	}, nil
}

// IsInterstitialTitle reports whether a document title belongs to a bot-check
// or block page rather than the content the caller asked for.
func IsInterstitialTitle(title string) bool {
	return challengeTitle(title)
}

// documentState is what the challenge loop needs from the live page. Only the
// cheap fields are polled; the body is read once the page has settled.
type documentState struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Ready string `json:"ready"`
}

// challengeTitle reports whether a title still belongs to an interstitial.
// Interstitial titles seen in the wild: Cloudflare's English, Japanese,
// Simplified Chinese and "blocked" pages, plus the plain "challenge" wording.
var challengeTitlePattern = regexp.MustCompile(`(?i)just a moment|checking your browser|attention required|access denied|you have been blocked|请稍候|请稍等|安全验证|しばらくお待ちください|challenge`)

func challengeTitle(title string) bool {
	return challengeTitlePattern.MatchString(title)
}

// waitForDocumentClear waits until the document has loaded and its title no
// longer looks like an interstitial.
//
// A Cloudflare challenge reloads the page while it validates, and every reload
// detaches the JavaScript execution context. Chromedp's PollFunction gives up in
// that situation ("Inspected target navigated or closed"), which is why this
// loop evaluates from scratch on every iteration and treats a failed evaluation
// as "keep waiting" rather than as an error.
func waitForDocumentClear(ctx context.Context, timeout time.Duration) (documentState, error) {
	deadline := time.Now().Add(timeout)
	last := "no probe completed"
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var state documentState
		probeErr := chromedp.Run(probeCtx, chromedp.Evaluate(`({url: location.href, title: document.title, ready: document.readyState})`, &state))
		cancel()
		switch {
		case probeErr == nil:
			if state.Ready == "complete" && !challengeTitle(state.Title) {
				return state, nil
			}
			last = fmt.Sprintf("title=%q ready=%q", state.Title, state.Ready)
		case ctx.Err() != nil:
			return documentState{}, ctx.Err()
		default:
			// The document navigated underneath us: retry on the new document.
			last = probeErr.Error()
		}
		if time.Now().After(deadline) {
			return documentState{}, fmt.Errorf("still challenged after %s (%s)", timeout.Round(time.Second), last)
		}
		select {
		case <-ctx.Done():
			return documentState{}, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}

func (f *Fetcher) startBrowserLocked(opts Options, startupTimeout time.Duration) error {
	proxyKey := opts.ProxyURL
	if f.browser.ctx != nil {
		if f.browser.proxyKey == proxyKey && f.browser.ctx.Err() == nil {
			return nil
		}
		// A cancelled context means the browser process is gone — crashed or
		// killed for memory. Keeping it would make every later request fail
		// against a dead target, so it is replaced here.
		f.stopBrowserLocked()
	}
	begin := time.Now()
	execOpts, displayStop, err := BrowserLaunchOptions()
	if err != nil {
		f.startErr = err
		return err
	}
	if proxyKey != "" {
		execOpts = append(execOpts, chromedp.ProxyServer(proxyKey))
	}
	// Keep what Chromium prints so a container that cannot start it reports
	// why instead of just timing out.
	var chromeLog bytes.Buffer
	execOpts = append(execOpts, chromedp.CombinedOutput(&chromeLog))

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), execOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	// Run the first navigation-less target creation in a goroutine so a browser
	// that never opens its DevTools endpoint can be abandoned. A container with
	// too little memory can leave this hanging for minutes, which the edge
	// gateway then reports as a 504 instead of a useful error.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	if startupTimeout <= 0 {
		startupTimeout = chromiumStartupTimeout()
	}
	select {
	case err := <-started:
		if err != nil {
			browserCancel()
			allocCancel()
			if displayStop != nil {
				displayStop()
			}
			wrapped := fmt.Errorf("start Chromium: %w%s", err, chromiumLogTail(chromeLog.String()))
			f.startErr = wrapped
			return wrapped
		}
	case <-time.After(startupTimeout):
		browserCancel()
		allocCancel()
		if displayStop != nil {
			displayStop()
		}
		wrapped := fmt.Errorf("start Chromium: no DevTools endpoint after %s%s", startupTimeout, chromiumLogTail(chromeLog.String()))
		f.startErr = wrapped
		return wrapped
	}
	f.browser = browserState{ctx: browserCtx, stop: browserCancel, alloc: allocCancel, display: displayStop, proxyKey: proxyKey, started: time.Now()}
	f.starts++
	f.lastStartMS = time.Since(begin).Milliseconds()
	f.startErr = nil
	return nil
}

// BrowserStatus reports whether the fallback browser is running.
func (f *Fetcher) BrowserStatus() BrowserStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := BrowserStatus{
		Starts:      f.starts,
		Restarts:    f.restarts,
		Supervised:  f.supervised,
		StartupTime: f.lastStartMS,
	}
	if f.browser.ctx != nil && f.browser.ctx.Err() == nil {
		status.Running = true
		status.UptimeMS = time.Since(f.browser.started).Milliseconds()
	}
	if f.startErr != nil {
		status.LastError = f.startErr.Error()
	}
	return status
}

// KeepWarm supervises the fallback browser for as long as ctx lives: it starts
// it once and restarts it whenever the process goes away (a crash, an OOM kill
// on a small instance, a proxy change).
//
// Without this, the warm-up was a one-shot: after a crash the next scrape paid
// the full cold start, and on a 512 MB instance that cold start is what makes
// the request time out at the reverse proxy.
func (f *Fetcher) KeepWarm(ctx context.Context) error {
	f.mu.Lock()
	f.supervised = true
	disabled := f.opts.DisableBrowser
	f.mu.Unlock()
	if disabled {
		// A fetcher configured without a browser must not spawn one just
		// because somebody started its supervisor.
		return nil
	}
	backoff := time.Second
	for {
		f.mu.Lock()
		disabled = f.opts.DisableBrowser
		f.mu.Unlock()
		if disabled {
			// DisableBrowserFallback was called while this loop was running.
			f.Close()
			return nil
		}
		if err := f.WarmBrowser(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			debugf("browser supervisor: %v (retrying in %s)", err, backoff)
			if !sleepOK(ctx, backoff) {
				return ctx.Err()
			}
			backoff = min(2*backoff, time.Minute)
			continue
		}
		backoff = time.Second
		f.mu.Lock()
		browserCtx := f.browser.ctx
		f.mu.Unlock()
		if browserCtx == nil {
			continue
		}
		select {
		case <-ctx.Done():
			f.Close()
			return ctx.Err()
		case <-browserCtx.Done():
			// chromedp cancels the browser context when the process exits.
			debugf("browser supervisor: Chromium exited, restarting")
			f.mu.Lock()
			f.restarts++
			f.stopBrowserLocked()
			f.mu.Unlock()
		}
	}
}

// sleepOK waits for d, reporting false if ctx ended first.
func sleepOK(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// startShareOfTimeout is how much of a request's own budget may go into a
// cold start when the browser was not warmed up. Spending all of it starting
// Chromium leaves nothing to load the page, so the request fails having proved
// only that the browser can start.
func startShareOfTimeout(timeout time.Duration) time.Duration {
	share := time.Duration(float64(timeout) * 0.7)
	return max(min(chromiumStartupTimeout(), share), 5*time.Second)
}

// chromiumStartupTimeout bounds how long the fallback browser may take to come
// up. Override with METAX_CHROMIUM_START_TIMEOUT when a slow instance needs
// longer.
func chromiumStartupTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("METAX_CHROMIUM_START_TIMEOUT")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 45 * time.Second
}

// chromiumLogTail formats the last few lines of Chromium's own output for an
// error message, because "timed out" alone does not say whether the browser ran
// out of memory, could not create its sandbox or failed to reach the network.
func chromiumLogTail(log string) string {
	log = strings.TrimSpace(log)
	if log == "" {
		return " (Chromium printed nothing)"
	}
	if len(log) > 600 {
		log = log[len(log)-600:]
	}
	return " (Chromium said: " + truncateCollapse(log, 600) + ")"
}

func (f *Fetcher) stopBrowserLocked() {
	if f.browser.stop != nil {
		// Cancel() asks the browser to quit and waits for it. Cancelling the
		// context alone is asynchronous, so a short-lived process (a CLI probe,
		// a shutting-down server) can exit first and leave Chrome orphaned.
		done := make(chan struct{})
		go func() {
			_ = chromedp.Cancel(f.browser.ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		f.browser.stop()
	}
	if f.browser.alloc != nil {
		f.browser.alloc()
	}
	if f.browser.display != nil {
		f.browser.display()
	}
	f.browser = browserState{}
}

// ChromiumPath returns the Chromium/Chrome executable to drive. An explicit
// METAX_CHROMIUM_BIN wins, then the usual PATH names, then the macOS app
// bundles (which are not on PATH).
func ChromiumPath() string {
	if executable := strings.TrimSpace(os.Getenv("METAX_CHROMIUM_BIN")); executable != "" {
		return executable
	}
	return findChromium()
}

// chromiumVersionPattern extracts the major version from `chrome --version`
// output such as "Google Chrome 156.0.8078.73" or "Chromium 140.0.7339.80".
var chromiumVersionPattern = regexp.MustCompile(`(\d+)\.\d+\.\d+\.\d+`)

var (
	chromiumVersionMu sync.Mutex
	chromiumVersions  = map[string]string{}
)

// chromiumMajorVersion reports the major version of the browser binary, cached
// per path. It is needed because the UA string must agree with the client hints
// Chrome generates from its own build.
func chromiumMajorVersion(executable string) string {
	chromiumVersionMu.Lock()
	cached, ok := chromiumVersions[executable]
	chromiumVersionMu.Unlock()
	if ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	major := ""
	if out, err := exec.CommandContext(ctx, executable, "--version").Output(); err == nil {
		if match := chromiumVersionPattern.FindStringSubmatch(string(out)); len(match) == 2 {
			major = match[1]
		}
	}
	chromiumVersionMu.Lock()
	chromiumVersions[executable] = major
	chromiumVersionMu.Unlock()
	return major
}

// browserUserAgent builds a Chrome UA for the current platform and the installed
// browser version, so the UA string, the client hints and the binary agree.
func browserUserAgent(executable string) string {
	major := chromiumMajorVersion(executable)
	if major == "" {
		return ""
	}
	var platform string
	switch runtime.GOOS {
	case "darwin":
		platform = "(Macintosh; Intel Mac OS X 10_15_7)"
	case "windows":
		platform = "(Windows NT 10.0; Win64; x64)"
	default:
		platform = "(X11; Linux x86_64)"
	}
	return fmt.Sprintf("Mozilla/5.0 %s AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36", platform, major)
}

// BrowserLanguage is the Accept-Language and navigator.language a challenge
// expects to see. Sites are more suspicious of an English headless browser than
// of one whose language matches the content.
func BrowserLanguage() string {
	if language := strings.TrimSpace(os.Getenv("METAX_BROWSER_LANG")); language != "" {
		return language
	}
	return "ja-JP"
}

// Transport adapts a Fetcher to http.RoundTripper so providers built on the
// colly scraper can reuse the same anti-bot path instead of shipping a plain
// http.Client that gets a 403 from Cloudflare.
type Transport struct {
	mu      sync.Mutex
	fetcher *Fetcher
	opts    Options
}

// NewTransport returns a transport that fetches through the shared layer.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.fetcher == nil {
		t.fetcher = New(t.opts)
	}
	fetcher := t.fetcher
	t.mu.Unlock()
	return fetcher.Do(req)
}

// SetOptions updates the timeout, proxy or cookies used by later requests.
func (t *Transport) SetOptions(update func(*Options)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	update(&t.opts)
	if t.fetcher != nil {
		opts := t.opts
		t.fetcher.SetOptions(func(current *Options) {
			current.Timeout = opts.Timeout
			current.ProxyURL = opts.ProxyURL
			current.Cookies = opts.Cookies
		})
	}
}

// Close releases any browser the transport started.
func (t *Transport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fetcher != nil {
		t.fetcher.Close()
		t.fetcher = nil
	}
}

// blockHeavyResources reports whether the browser should refuse images,
// fonts and media. Off via METAX_CHROMIUM_BLOCK_MEDIA=0.
func blockHeavyResources() bool {
	return strings.TrimSpace(os.Getenv("METAX_CHROMIUM_BLOCK_MEDIA")) != "0"
}

// blockedResource reports whether a paused request is one of the resource types
// the scraper never reads. Stylesheets are deliberately not in this list: a
// challenge can depend on computed layout.
func blockedResource(kind network.ResourceType) bool {
	if !blockHeavyResources() {
		return false
	}
	switch kind {
	case network.ResourceTypeImage, network.ResourceTypeMedia, network.ResourceTypeFont:
		return true
	}
	return false
}

// lowMemoryChromium reports whether the browser should be launched with
// settings that trade a little speed for a much smaller footprint. Default on
// inside a container, where the instance usually has 512 MB for everything.
// METAX_CHROMIUM_LOW_MEMORY=0 disables it.
func lowMemoryChromium() bool {
	switch strings.TrimSpace(os.Getenv("METAX_CHROMIUM_LOW_MEMORY")) {
	case "0":
		return false
	case "1":
		return true
	}
	return inContainer()
}

// chromiumJSHeapMB is the V8 heap ceiling for the browser's renderer. It is a
// ceiling, not an allocation: pages use what they need. Lowering it only makes
// a runaway script die instead of taking the whole instance with it.
func chromiumJSHeapMB() int {
	if raw := strings.TrimSpace(os.Getenv("METAX_CHROMIUM_JS_HEAP_MB")); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil && mb >= 64 {
			return mb
		}
	}
	return 192
}

// flagBool and flagString are the only two ways a launch flag may be set here.
//
// chromedp.Flag takes an interface{} and does not check it: an option whose
// value is neither string nor bool is accepted, then rejected much later inside
// Allocate with the opaque "invalid exec pool flag". That failure only happens
// on a machine that actually launches a browser, so a wrong type passes every
// local test and breaks the deployment — which is exactly how an int
// renderer-process-limit shipped and kept the Koyeb browser from starting.
// Narrowing the parameter type turns that class of bug into a compile error.
func flagBool(name string, value bool) chromedp.ExecAllocatorOption {
	return chromedp.Flag(name, value)
}

func flagString(name, value string) chromedp.ExecAllocatorOption {
	return chromedp.Flag(name, value)
}

// BrowserLaunchOptions returns the shared Chromium launch flags together with a
// cleanup function for any helper process it started.
//
// Headless is the default: scraping must never open a window, steal focus or
// touch the operator's own browser session. chromedp also allocates a throwaway
// user-data-dir per launch, so cookies and logins of the local Chrome profile
// are never read or written. Set METAX_CHROMIUM_HEADED=1 only when a challenge
// has to be debugged interactively; on a headless Linux host that path starts
// Xvfb so the window still stays off any real display.
func BrowserLaunchOptions() ([]chromedp.ExecAllocatorOption, func(), error) {
	executable := ChromiumPath()
	if executable == "" {
		return nil, nil, errors.New("Chromium executable not found (set METAX_CHROMIUM_BIN or install chromium)")
	}
	execOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	execOpts = append(execOpts,
		chromedp.ExecPath(executable),
		flagBool("enable-automation", false),
		flagString("disable-blink-features", "AutomationControlled"),
		flagBool("no-first-run", true),
		flagBool("no-default-browser-check", true),
		flagBool("disable-background-networking", true),
		flagBool("disable-renderer-backgrounding", true),
		flagBool("disable-backgrounding-occluded-windows", true),
		// A real User-Agent is passed as a launch flag, never through CDP.
		// Emulation.setUserAgentOverride leaves a trace Cloudflare reads, and a
		// challenge under it never clears; the flag is indistinguishable from a
		// user launch. The version has to match the installed browser because
		// Chrome derives Sec-CH-UA from the binary, not from this string.
	)
	if userAgent := browserUserAgent(executable); userAgent != "" {
		execOpts = append(execOpts, flagString("user-agent", userAgent))
	}
	if language := BrowserLanguage(); language != "" {
		execOpts = append(execOpts, flagString("lang", language))
	}
	// Containers (Koyeb, Cloud Run, Kubernetes) do not grant Chromium what its
	// sandbox needs: unprivileged user namespaces are blocked by the default
	// seccomp profile and /dev/shm is capped at 64 MiB. Without these flags the
	// browser dies at startup with "No usable sandbox!" or the renderer crashes
	// with SIGBUS, which surfaces as an empty fetch and a failed provider.
	// Dropping the sandbox is acceptable here because the container itself is
	// the isolation boundary.
	if inContainer() {
		execOpts = append(execOpts,
			flagBool("no-sandbox", true),
			flagBool("disable-dev-shm-usage", true),
			flagBool("disable-gpu", true),
		)
		debugf("container detected: launching Chromium with --no-sandbox --disable-dev-shm-usage")
	}
	if lowMemoryChromium() {
		// One tab does not need one renderer process per site, and a renderer
		// costs tens of megabytes before it renders anything. The heap ceiling
		// turns a runaway challenge script into a failed renderer instead of an
		// OOM kill of the whole container.
		execOpts = append(execOpts,
			// renderer-process-limit is an integer to Chromium but a string
			// to chromedp; passing the int made Allocate refuse every flag
			// with "invalid exec pool flag" and the browser never started.
			flagString("renderer-process-limit", "1"),
			flagString("js-flags", fmt.Sprintf("--max-old-space-size=%d", chromiumJSHeapMB())),
			flagBool("mute-audio", true),
			flagBool("disable-audio-output", true),
			flagBool("disable-extensions", true),
			flagBool("disable-component-extensions-with-background-pages", true),
		)
		debugf("low-memory Chromium profile: single renderer, V8 heap %d MB, no extensions", chromiumJSHeapMB())
	}
	debugf("launching %q (headed=%v, ua=%q)", executable, headedRequested(), browserUserAgent(executable))
	if !headedRequested() {
		// "new" is the modern headless mode: it renders like a real browser,
		// which matters because legacy headless is trivially fingerprinted by
		// Cloudflare. The window size keeps a desktop viewport in headless.
		execOpts = append(execOpts,
			flagString("headless", "new"),
			chromedp.WindowSize(1280, 900),
		)
		return execOpts, func() {}, nil
	}
	execOpts = append(execOpts, flagBool("headless", false))
	if runtime.GOOS != "darwin" && os.Getenv("DISPLAY") == "" {
		display, stop, err := startVirtualDisplay()
		if err != nil {
			return nil, nil, fmt.Errorf("start virtual display for Chromium: %w", err)
		}
		execOpts = append(execOpts, chromedp.Env("DISPLAY="+display))
		return execOpts, stop, nil
	}
	return execOpts, func() {}, nil
}

// inContainer reports whether the process runs inside a container runtime.
// The check is deliberately cheap and tolerant: a false negative only means
// Chromium is launched with its sandbox, which is the safer default.
func inContainer() bool {
	if os.Getenv("METAX_CHROMIUM_NO_SANDBOX") == "1" {
		return true
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" || os.Getenv("KOYEB_APP_NAME") != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		text := string(data)
		for _, marker := range []string{"docker", "kubepods", "containerd", "lxc", "gke", "koyeb"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

// headedRequested reports whether the operator explicitly asked for a visible
// browser. Everything else runs headless, including a bare local run.
func headedRequested() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("METAX_CHROMIUM_HEADED"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func findChromium() string {
	for _, candidate := range []string{
		"chromium", "chromium-browser", "google-chrome", "google-chrome-stable",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	} {
		if found, err := exec.LookPath(candidate); err == nil {
			return found
		}
	}
	return ""
}

func startVirtualDisplay() (string, context.CancelFunc, error) {
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		return "", nil, errors.New("Xvfb is required for headed Chromium when DISPLAY is unset")
	}
	displayNumber := 100 + os.Getpid()%500
	display := fmt.Sprintf(":%d", displayNumber)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, xvfb, display, "-screen", "0", "1280x800x24", "-nolisten", "tcp", "-ac")
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("launch Xvfb: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	socket := fmt.Sprintf("/tmp/.X11-unix/X%d", displayNumber)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return display, cancel, nil
		}
		select {
		case <-ctx.Done():
			cancel()
			return "", nil, errors.New("Xvfb exited before its display became ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	return "", nil, errors.New("timed out waiting for Xvfb display")
}

func isChallengeHost(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Port() == "" && u.User == nil &&
		strings.EqualFold(u.Hostname(), "challenges.cloudflare.com")
}

// ParseCookieHeader parses a raw Cookie header value into cookies. Values with
// CR/LF are rejected so a malformed configuration cannot inject headers.
func ParseCookieHeader(raw string) []*http.Cookie {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Cookie", raw)
	return req.Cookies()
}

func truncateCollapse(s string, n int) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if len(collapsed) <= n {
		return collapsed
	}
	return collapsed[:n] + "…"
}
