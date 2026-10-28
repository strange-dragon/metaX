// Package webpipe talks to the WebPipe scraping API, which renders pages in a
// hosted browser pool and returns the result as raw HTML.
//
// It exists because one target (JavLibrary) sits behind a Cloudflare challenge
// that no amount of local browser tuning clears: the block follows the egress
// IP, and the container's datacenter range is scored as hostile, so the
// interstitial loops until the request budget runs out. A hosted fetcher with
// its own browser pool moves the problem somewhere we do not operate.
//
// The format requested is rawHtml and not html or markdown on purpose: rawHtml
// is byte-identical to what the origin served, so the existing selectors in
// provider/javlibrary keep working unchanged. The cleaned variants strip script
// and style tags and rewrite structure, which would quietly break parsing that
// has been validated against the real markup.
package webpipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the hosted endpoint. Override for a self-hosted instance.
const DefaultBaseURL = "https://api.web2json.ai"

// DefaultTimeout covers the whole hosted render, which includes navigating a
// challenge page and waiting for it to clear. It is deliberately longer than a
// plain HTTP fetch: the service is doing what our local browser could not.
const DefaultTimeout = 90 * time.Second

// Client is a WebPipe API client. The zero value is not usable; build it with
// New so the timeout and endpoint defaults are applied.
type Client struct {
	APIKey   string
	BaseURL  string
	HTTP     *http.Client
	UseProxy bool

	// Proxy sends the request through the operator's own proxy instead of the
	// service's pool. Only http(s) is supported by the API; an empty value
	// means "let the service decide" (pool, then service env, then system).
	Proxy string
}

// New returns a client for the given key. An empty key is rejected here rather
// than at request time, because every call would fail identically.
func New(apiKey string) *Client {
	return &Client{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: DefaultTimeout},
	}
}

// Result is the part of a scrape response this service cares about. The API
// returns more formats when asked; only the ones needed for parsing are kept.
type Result struct {
	RawHTML    string
	StatusCode int
	Title      string
	SourceURL  string
}

// ErrUnauthorized reports a missing or rejected API key. It is separated from
// other failures because it is a configuration mistake, not a transient one:
// retrying will never help and the operator has to act.
var ErrUnauthorized = errors.New("webpipe: API key rejected")

type scrapeRequest struct {
	URL      string   `json:"url"`
	Formats  []string `json:"formats"`
	UseProxy bool     `json:"use_proxy,omitempty"`
	Proxy    string   `json:"proxy,omitempty"`
}

type scrapeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		RawHTML  string `json:"rawHtml"`
		Metadata struct {
			Title      string `json:"title"`
			StatusCode int    `json:"statusCode"`
			SourceURL  string `json:"sourceURL"`
		} `json:"metadata"`
	} `json:"data"`
}

// Scrape fetches targetURL through the hosted browser and returns its raw HTML.
//
// A non-2xx answer from the *target* is reported the way the origin sent it
// (via Result.StatusCode) only when the service managed to return a body; the
// service itself reports target errors as HTTP 500 with a message, which is
// surfaced as an error because there is nothing to parse.
func (c *Client) Scrape(ctx context.Context, targetURL string) (*Result, error) {
	if c.APIKey == "" {
		return nil, errors.New("webpipe: no API key configured")
	}
	body, err := json.Marshal(scrapeRequest{
		URL:      targetURL,
		Formats:  []string{"rawHtml", "metadata"},
		UseProxy: c.UseProxy,
		Proxy:    strings.TrimSpace(c.Proxy),
	})
	if err != nil {
		return nil, fmt.Errorf("webpipe: encode request: %w", err)
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/v1/scrape"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("webpipe: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webpipe: request failed: %w", err)
	}
	defer resp.Body.Close()

	// The response can be large (a whole page), so it is capped rather than
	// streamed: a runaway body is a service bug, not something to parse.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("webpipe: read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webpipe: HTTP %d: %s", resp.StatusCode, apiError(payload))
	}

	var parsed scrapeResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("webpipe: decode response: %w", err)
	}
	if !parsed.Success {
		return nil, fmt.Errorf("webpipe: scrape failed: %s", firstNonEmpty(parsed.Error, "no detail"))
	}
	if parsed.Data.RawHTML == "" {
		return nil, errors.New("webpipe: empty body (the page may have needed a challenge the service did not clear)")
	}
	return &Result{
		RawHTML:    parsed.Data.RawHTML,
		StatusCode: parsed.Data.Metadata.StatusCode,
		Title:      parsed.Data.Metadata.Title,
		SourceURL:  parsed.Data.Metadata.SourceURL,
	}, nil
}

// apiError extracts the service's error string, falling back to a truncated
// body so an unexpected HTML error page is still diagnosable.
func apiError(payload []byte) string {
	var parsed scrapeResponse
	if err := json.Unmarshal(payload, &parsed); err == nil {
		if msg := strings.TrimSpace(parsed.Error); msg != "" {
			return msg
		}
	}
	text := strings.TrimSpace(string(payload))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return firstNonEmpty(text, "no detail")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
