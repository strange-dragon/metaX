package httpretry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func testPolicy(maxAttempts int) Policy {
	return Policy{
		MaxAttempts: maxAttempts,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    time.Second,
		Jitter:      func(d time.Duration) time.Duration { return d },
		Sleep:       func(context.Context, time.Duration) error { return nil },
	}
}

func TestRetriesTransientStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return response(status, "retry"), nil
				}
				return response(http.StatusOK, "ok"), nil
			})
			req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
			resp, err := testPolicy(2).WrapTransport(transport).RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if calls != 2 {
				t.Fatalf("calls = %d, want 2", calls)
			}
		})
	}
}

func TestDoesNotRetryPermanentResponses(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return response(status, "permanent"), nil
			})
			req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
			resp, err := testPolicy(3).WrapTransport(transport).RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, status)
			}
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestDoesNotRetryUnsafeOrUnreplayableRequests(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
	}{
		{"POST", httptest.NewRequest(http.MethodPost, "https://example.invalid/", strings.NewReader("x"))},
		{"GET without GetBody", func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", strings.NewReader("x"))
			req.GetBody = nil
			return req
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return response(http.StatusServiceUnavailable, "retry"), nil
			})
			resp, err := testPolicy(3).WrapTransport(transport).RoundTrip(tc.req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer resp.Body.Close()
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestRetryAfterSecondsIsBounded(t *testing.T) {
	var sleeps []time.Duration
	policy := testPolicy(2)
	policy.MaxDelay = 500 * time.Millisecond
	policy.Sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := response(http.StatusTooManyRequests, "retry")
			resp.Header.Set("Retry-After", "3")
			return resp, nil
		}
		return response(http.StatusOK, "ok"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	resp, err := policy.WrapTransport(transport).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if len(sleeps) != 1 || sleeps[0] != 500*time.Millisecond {
		t.Fatalf("sleeps = %v, want [500ms]", sleeps)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	var sleeps []time.Duration
	policy := testPolicy(2)
	policy.MaxDelay = 5 * time.Second
	policy.Sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	now := time.Now()
	retryAt := now.Add(2 * time.Second).UTC().Format(http.TimeFormat)
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := response(http.StatusServiceUnavailable, "retry")
			resp.Header.Set("Retry-After", retryAt)
			return resp, nil
		}
		return response(http.StatusOK, "ok"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	resp, err := policy.WrapTransport(transport).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if len(sleeps) != 1 || sleeps[0] < time.Second || sleeps[0] > 3*time.Second {
		t.Fatalf("sleeps = %v, want about 2s", sleeps)
	}
}

func TestRetriesTransientNetworkErrors(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, timeoutError{}
		}
		return response(http.StatusOK, "ok"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	resp, err := testPolicy(2).WrapTransport(transport).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDoesNotRetryPermanentNetworkErrors(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("permanent network failure")
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	if _, err := testPolicy(3).WrapTransport(transport).RoundTrip(req); err == nil {
		t.Fatal("RoundTrip() error = nil, want permanent error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, "ok"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil).WithContext(ctx)
	if _, err := testPolicy(3).WrapTransport(transport).RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestContextCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	policy := testPolicy(3)
	policy.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusServiceUnavailable, "retry"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil).WithContext(ctx)
	if _, err := policy.WrapTransport(transport).RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestExhaustsBoundedAttempts(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusServiceUnavailable, "retry"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	resp, err := testPolicy(3).WrapTransport(transport).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestRetryTransportsDoNotSerializeEachOther(t *testing.T) {
	slowSleeping := make(chan struct{})
	releaseSlow := make(chan struct{}, 1)
	defer func() {
		select {
		case releaseSlow <- struct{}{}:
		default:
		}
	}()

	slowPolicy := testPolicy(2)
	slowPolicy.Sleep = func(context.Context, time.Duration) error {
		close(slowSleeping)
		<-releaseSlow
		return nil
	}
	slowCalls := 0
	slow := slowPolicy.WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		slowCalls++
		if slowCalls == 1 {
			return response(http.StatusServiceUnavailable, "retry"), nil
		}
		return response(http.StatusOK, "slow-ok"), nil
	}))

	slowDone := make(chan error, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "https://slow.invalid/", nil)
		resp, err := slow.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		slowDone <- err
	}()
	<-slowSleeping

	fast := testPolicy(2).WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "fast-ok"), nil
	}))
	fastDone := make(chan error, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "https://fast.invalid/", nil)
		resp, err := fast.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		fastDone <- err
	}()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatalf("fast transport error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fast transport blocked behind another provider's retry backoff")
	}
	releaseSlow <- struct{}{}
	if err := <-slowDone; err != nil {
		t.Fatalf("slow transport error = %v", err)
	}
}
