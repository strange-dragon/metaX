// Package httpretry provides a bounded retry transport for idempotent provider
// requests. It retries only transient HTTP statuses and network errors, and it
// never retries an unsafe method or a request whose body cannot be replayed.
package httpretry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 200 * time.Millisecond
	DefaultMaxDelay    = 2 * time.Second
)

// Policy controls retry attempts and delays. The zero value uses the defaults.
// Sleep and Jitter are injectable so tests do not depend on wall-clock timing.
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      func(time.Duration) time.Duration
	Sleep       func(context.Context, time.Duration) error
}

// DefaultPolicy is deliberately conservative: at most three attempts, a
// two-second cap, and no retry for permanent 4xx responses.
var DefaultPolicy = Policy{
	MaxAttempts: DefaultMaxAttempts,
	BaseDelay:   DefaultBaseDelay,
	MaxDelay:    DefaultMaxDelay,
}

// WrapTransport wraps next with the default retry policy. A nil next uses
// http.DefaultTransport. Wrapping an already-wrapped transport is a no-op.
func WrapTransport(next http.RoundTripper) http.RoundTripper {
	return DefaultPolicy.WrapTransport(next)
}

// WrapTransport wraps next with this policy. A nil next uses
// http.DefaultTransport. Wrapping an already-wrapped transport is a no-op.
func (p Policy) WrapTransport(next http.RoundTripper) http.RoundTripper {
	if retry, ok := next.(*retryTransport); ok {
		return retry
	}
	if next == nil {
		next = http.DefaultTransport
	}
	return &retryTransport{next: next, policy: p.normalized()}
}

func (p Policy) normalized() Policy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultMaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultBaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultMaxDelay
	}
	if p.Jitter == nil {
		p.Jitter = defaultJitter
	}
	if p.Sleep == nil {
		p.Sleep = sleepContext
	}
	return p
}

type retryTransport struct {
	next   http.RoundTripper
	policy Policy
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("httpretry: nil request")
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	retryable := isRetryableMethod(req.Method) && isReplayable(req)
	attempts := t.policy.MaxAttempts
	if !retryable {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptReq, err := requestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := t.next.RoundTrip(attemptReq)
		if !retryable || attempt == attempts {
			return resp, err
		}
		if err != nil {
			if !isTransientError(err) {
				return nil, err
			}
			lastErr = err
			if err := t.wait(req.Context(), t.policy.Jitter(t.backoff(attempt))); err != nil {
				return nil, err
			}
			continue
		}
		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}
		delay := t.retryAfter(resp, time.Now())
		if delay <= 0 {
			delay = t.policy.Jitter(t.backoff(attempt))
		}
		drainAndClose(resp.Body)
		lastErr = fmt.Errorf("httpretry: retryable HTTP status %s", resp.Status)
		if err := t.wait(req.Context(), delay); err != nil {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("httpretry: retry attempts exhausted")
	}
	return nil, lastErr
}

func requestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	next := req.Clone(req.Context())
	if attempt == 1 || next.Body == nil || next.Body == http.NoBody {
		return next, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("httpretry: request body cannot be replayed")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("httpretry: replay request body: %w", err)
	}
	next.Body = body
	return next, nil
}

func (t *retryTransport) backoff(attempt int) time.Duration {
	delay := t.policy.BaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= t.policy.MaxDelay/2 {
			delay = t.policy.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > t.policy.MaxDelay {
		delay = t.policy.MaxDelay
	}
	return delay
}

func (t *retryTransport) retryAfter(resp *http.Response, now time.Time) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	var delay time.Duration
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		}
	} else if at, err := http.ParseTime(raw); err == nil {
		if d := at.Sub(now); d > 0 {
			delay = d
		}
	}
	if delay <= 0 {
		return 0
	}
	if delay > t.policy.MaxDelay {
		delay = t.policy.MaxDelay
	}
	return delay
}

func (t *retryTransport) wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	return t.policy.Sleep(ctx, delay)
}

func isRetryableMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isReplayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	_ = body.Close()
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func defaultJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	delta := delay / 5
	if delta <= 0 {
		return delay
	}
	return delay - delta + time.Duration(rand.Int64N(int64(2*delta)+1))
}
