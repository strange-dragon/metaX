package scraper

import (
	"net/http"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/debug"

	"github.com/strange-dragon/metaX/common/random"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

type Option func(*Scraper) error

func WithAllowURLRevisit() Option {
	return func(s *Scraper) error {
		colly.AllowURLRevisit()(s.c)
		return nil
	}
}

func WithLogDebugger() Option {
	return func(s *Scraper) error {
		colly.Debugger(&debug.LogDebugger{})
		return nil
	}
}

func WithDetectCharset() Option {
	return func(s *Scraper) error {
		colly.DetectCharset()(s.c)
		return nil
	}
}

// WithRetryTransport installs the bounded retry transport shared by the
// hand-rolled HTTP providers. colly's default transport has no retry at all, so
// a single transient 502 or reset connection used to fail the whole source.
// Only idempotent methods and replayable bodies are retried; see httpretry.
//
// Timeouts are excluded: colly's SetRequestTimeout sets http.Client.Timeout,
// which bounds the whole request. Retrying after it fires spends the same
// budget twice more and always fails again, turning a 10s stall into 30s.
func WithRetryTransport() Option {
	return WithTransport(retryPolicy.WrapTransport(nil))
}

var retryPolicy = func() httpretry.Policy {
	policy := httpretry.DefaultPolicy
	policy.SkipTimeouts = true
	return policy
}()

func WithIgnoreRobotsTxt() Option {
	return func(s *Scraper) error {
		colly.IgnoreRobotsTxt()(s.c)
		return nil
	}
}

func WithHeaders(headers map[string]string) Option {
	return func(s *Scraper) error {
		colly.Headers(headers)(s.c)
		return nil
	}
}

func WithUserAgent(ua string) Option {
	return func(s *Scraper) error {
		colly.UserAgent(ua)(s.c)
		return nil
	}
}

func WithRandomUserAgent() Option {
	return WithUserAgent(random.UserAgent())
}

func WithCookies(url string, cookies []*http.Cookie) Option {
	return func(s *Scraper) error {
		return s.c.SetCookies(url, cookies)
	}
}

func WithRequestTimeout(timeout time.Duration) Option {
	return func(s *Scraper) error {
		s.c.SetRequestTimeout(timeout)
		return nil
	}
}

func WithDisableCookies() Option {
	return func(s *Scraper) error {
		s.c.DisableCookies()
		return nil
	}
}

func WithDisableRedirects() Option {
	return func(s *Scraper) error {
		s.c.ParseHTTPErrorResponse = true
		s.c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		})
		return nil
	}
}

func WithTransport(transport http.RoundTripper) Option {
	return func(s *Scraper) error {
		s.c.WithTransport(transport)
		return nil
	}
}

func WithLimit(rule *colly.LimitRule) Option {
	return func(s *Scraper) error {
		return s.c.Limit(rule)
	}
}
