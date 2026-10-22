package scraper

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"go.uber.org/atomic"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/provider"
)

var (
	_ provider.Provider             = (*Scraper)(nil)
	_ provider.ConfigSetter         = (*Scraper)(nil)
	_ provider.ProxySetter          = (*Scraper)(nil)
	_ provider.RequestTimeoutSetter = (*Scraper)(nil)
)

// Scraper implements the basic Provider interface.
type Scraper struct {
	name     string
	baseURL  *url.URL
	priority *atomic.Float64
	language language.Tag
	c        *colly.Collector
}

// NewScraper returns a *Scraper that implements provider.Provider.
func NewScraper(name, base string, priority float64, lang language.Tag, opts ...Option) *Scraper {
	baseURL, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	s := &Scraper{
		name:     name,
		baseURL:  baseURL,
		priority: atomic.NewFloat64(priority),
		language: lang,
		c:        colly.NewCollector(),
	}
	for _, opt := range opts {
		// Apply options.
		if err := opt(s); err != nil {
			panic(err)
		}
	}
	return s
}

// NewDefaultScraper returns a *Scraper with default options enabled.
func NewDefaultScraper(name, baseURL string, priority float64, lang language.Tag, opts ...Option) *Scraper {
	return NewScraper(name, baseURL, priority, lang, append([]Option{
		WithAllowURLRevisit(),
		WithIgnoreRobotsTxt(),
		WithRandomUserAgent(),
	}, opts...)...)
}

func (s *Scraper) Name() string { return s.name }

func (s *Scraper) URL() *url.URL { return s.baseURL }

func (s *Scraper) Priority() float64 { return s.priority.Load() }

func (s *Scraper) SetPriority(v float64) { s.priority.Store(v) }

func (s *Scraper) Language() language.Tag { return s.language }

func (s *Scraper) NormalizeMovieID(id string) string { return id /* AS IS */ }

func (s *Scraper) ParseMovieIDFromURL(string) (string, error) { panic("unimplemented") }

func (s *Scraper) NormalizeActorID(id string) string { return id /* AS IS */ }

func (s *Scraper) ParseActorIDFromURL(string) (string, error) { panic("unimplemented") }

// ClonedCollector returns cloned internal collector.
func (s *Scraper) ClonedCollector() *colly.Collector { return s.c.Clone() }

// SetProxy sets http or socks5 proxy for HTTP requests.
func (s *Scraper) SetProxy(proxyURL string) error { return s.c.SetProxy(proxyURL) }

// SetRequestTimeout sets timeout for HTTP requests.
func (s *Scraper) SetRequestTimeout(timeout time.Duration) { s.c.SetRequestTimeout(timeout) }

// SetConfig applies provider configuration shared by scraper-based providers.
// A Cookie header supplied through the provider's COOKIES (or COOKIE) setting
// is installed for the provider's base URL. This matches Amane's per-site
// cookie configuration and lets deployments authenticate to sites that return
// login or anti-bot pages to anonymous clients.
func (s *Scraper) SetConfig(config provider.Config) error {
	if config == nil {
		return nil
	}
	var (
		raw string
		err error
	)
	switch {
	case config.Has("cookies"):
		raw, err = config.GetString("cookies")
	case config.Has("cookie"):
		raw, err = config.GetString("cookie")
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: read cookies config: %w", s.name, err)
	}
	cookies, err := parseCookieHeader(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", s.name, err)
	}
	if len(cookies) == 0 {
		return nil
	}
	if err := s.c.SetCookies(s.baseURL.String(), cookies); err != nil {
		return fmt.Errorf("%s: set cookies: %w", s.name, err)
	}
	return nil
}

func parseCookieHeader(raw string) ([]*http.Cookie, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid cookie setting")
		}
		if strings.ContainsAny(name+value, "\r\n") {
			return nil, fmt.Errorf("cookie setting must not contain CR or LF")
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}
	return cookies, nil
}
