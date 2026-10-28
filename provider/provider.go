package provider

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/model"
)

type Provider interface {
	// Name returns the name of the provider.
	Name() string

	// Priority returns the matching priority of the provider.
	Priority() float64

	// SetPriority sets the provider priority to the given value.
	SetPriority(v float64)

	// Language returns the primary language supported by the provider.
	Language() language.Tag

	// URL returns the base url of the provider.
	URL() *url.URL
}

type MovieSearcher interface {
	// SearchMovie searches matched movies.
	SearchMovie(keyword string) ([]*model.MovieSearchResult, error)

	// NormalizeMovieKeyword converts movie keyword to provider-friendly form.
	NormalizeMovieKeyword(Keyword string) string
}

type MovieReviewer interface {
	// GetMovieReviewsByID gets the user reviews of given movie id.
	GetMovieReviewsByID(id string) ([]*model.MovieReviewDetail, error)

	// GetMovieReviewsByURL gets the user reviews of given movie URL.
	GetMovieReviewsByURL(rawURL string) ([]*model.MovieReviewDetail, error)
}

type MovieProvider interface {
	// Provider should be implemented.
	Provider

	// NormalizeMovieID normalizes movie ID to conform to standard.
	NormalizeMovieID(id string) string

	// ParseMovieIDFromURL parses movie ID from given URL.
	ParseMovieIDFromURL(rawURL string) (string, error)

	// GetMovieInfoByID gets movie's info by id.
	GetMovieInfoByID(id string) (*model.MovieInfo, error)

	// GetMovieInfoByURL gets movie's info by url.
	GetMovieInfoByURL(url string) (*model.MovieInfo, error)
}

type ActorSearcher interface {
	// SearchActor searches matched actor/s.
	SearchActor(keyword string) ([]*model.ActorSearchResult, error)
}

type ActorProvider interface {
	// Provider should be implemented.
	Provider

	// NormalizeActorID normalizes actor ID to conform to standard.
	NormalizeActorID(id string) string

	// ParseActorIDFromURL parses actor ID from given URL.
	ParseActorIDFromURL(rawURL string) (string, error)

	// GetActorInfoByID gets actor's info by id.
	GetActorInfoByID(id string) (*model.ActorInfo, error)

	// GetActorInfoByURL gets actor's info by url.
	GetActorInfoByURL(url string) (*model.ActorInfo, error)
}

type Fetcher interface {
	// Fetch fetches media resources from url.
	Fetch(url string) (*http.Response, error)
}

type RequestTimeoutSetter interface {
	// SetRequestTimeout sets timeout for HTTP requests.
	SetRequestTimeout(timeout time.Duration)
}

type ProxySetter interface {
	// SetProxy sets http or socks5 proxy for HTTP requests.
	SetProxy(proxyURL string) error
}

// BrowserWarmer is implemented by providers whose fetch path needs a headless
// browser. The server calls it once at boot so the cold start, which can take
// a minute on a small cloud instance, is not paid by the first request.
type BrowserWarmer interface {
	WarmupBrowser(ctx context.Context) error
}

// BrowserKeeper is implemented by providers that want their browser kept
// running for the whole process lifetime, not just warmed once. The method
// blocks: the server runs it in a goroutine until the process exits, and the
// implementation restarts the browser whenever it disappears.
type BrowserKeeper interface {
	KeepBrowser(ctx context.Context) error
}

// BrowserProber is implemented by providers that can tell whether their
// fallback browser is actually required from this egress.
//
// A browser exists for one reason: to execute a JavaScript challenge. When the
// network already passes — a residential proxy, an IP Cloudflare does not
// score, a valid cf_clearance cookie — the cheap TLS client gets the real page
// and the browser is pure memory cost. On a 512 MB instance that cost is the
// difference between staying up and being OOM-killed. The server therefore
// asks each such provider once at boot and skips the browser when it is not
// needed.
type BrowserProber interface {
	// ProbeBrowserNeed reports whether a plain request from this egress is
	// challenged. detail is a short human-readable reason that goes into the
	// boot log and the status endpoint. An implementation must be conservative:
	// when the probe cannot tell, it returns true.
	ProbeBrowserNeed(ctx context.Context) (required bool, detail string)
}

// BrowserAllowedSetter lets the server forbid a provider from starting its
// fallback browser. reason is recorded for the status endpoint.
type BrowserAllowedSetter interface {
	SetBrowserAllowed(allowed bool, reason string)
}

// BrowserStatus is a snapshot of a provider's fallback browser, safe to expose
// over HTTP: no proxy credentials, no page content.
type BrowserStatus struct {
	Running    bool          `json:"running"`
	Supervised bool          `json:"supervised"`
	Uptime     time.Duration `json:"-"`
	Starts     int           `json:"starts"`
	Restarts   int           `json:"restarts"`
	LastStart  time.Duration `json:"-"`
	LastError  string        `json:"last_error,omitempty"`
	// Disabled is set when the server decided this deployment does not need a
	// browser at all, so Running will stay false on purpose.
	Disabled bool `json:"disabled,omitempty"`
	// Reason explains a Disabled decision, or the probe result otherwise.
	Reason string `json:"reason,omitempty"`
}

// BrowserReporter is implemented by providers that can describe their fallback
// browser, for the boot log and the status endpoint.
type BrowserReporter interface {
	BrowserStatus() BrowserStatus
}

type Config interface {
	Has(string) bool
	GetString(string) (string, error)
	GetBool(string) (bool, error)
	GetInt64(string) (int64, error)
	GetFloat64(string) (float64, error)
	GetDuration(string) (time.Duration, error)
}

type ConfigSetter interface {
	// SetConfig sets any additional configs for Provider.
	SetConfig(config Config) error
}
