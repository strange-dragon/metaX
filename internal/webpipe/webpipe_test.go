package webpipe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New("wc-test")
	client.BaseURL = server.URL
	return client
}

func TestScrapeReturnsRawHTML(t *testing.T) {
	var got scrapeRequest
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer wc-test" {
			t.Errorf("authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"rawHtml": "<html><body>real page</body></html>",
				"metadata": map[string]any{
					"title":      "Example",
					"statusCode": 200,
					"sourceURL":  "https://example.com/",
				},
			},
		})
	})

	result, err := client.Scrape(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if result.RawHTML != "<html><body>real page</body></html>" {
		t.Errorf("rawHtml = %q", result.RawHTML)
	}
	if result.StatusCode != 200 || result.Title != "Example" {
		t.Errorf("metadata not carried through: %+v", result)
	}
	// rawHtml keeps script tags, so parsing can rely on the origin's markup;
	// asking for "html" instead would return a cleaned document.
	if len(got.Formats) != 2 || got.Formats[0] != "rawHtml" {
		t.Errorf("formats = %v, want rawHtml first", got.Formats)
	}
	if got.UseProxy {
		t.Error("use_proxy must default to false: the pool is opt-in")
	}
}

func TestScrapeForwardsProxySettings(t *testing.T) {
	var got scrapeRequest
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"data":{"rawHtml":"<html></html>"}}`))
	})
	client.UseProxy = true
	client.Proxy = "http://user:pass@proxy:8080"

	if _, err := client.Scrape(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if !got.UseProxy || got.Proxy != "http://user:pass@proxy:8080" {
		t.Errorf("proxy settings not forwarded: %+v", got)
	}
}

func TestScrapeReportsUnauthorizedKey(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":"未授权"}`))
	})

	_, err := client.Scrape(context.Background(), "https://example.com")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestScrapeSurfacesServiceError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":"抓取超时"}`))
	})

	_, err := client.Scrape(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "抓取超时") {
		t.Fatalf("err = %v, want the service message", err)
	}
}

// An empty body is the failure mode that matters most here: the service answers
// 200 with success true but nothing to parse when it could not get past the
// challenge, and reporting that as a successful empty page would hide the real
// problem one layer up.
func TestScrapeRejectsEmptyBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"rawHtml":""}}`))
	})

	_, err := client.Scrape(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "empty body") {
		t.Fatalf("err = %v, want empty body error", err)
	}
}

func TestScrapeRequiresKey(t *testing.T) {
	client := New("   ")
	if _, err := client.Scrape(context.Background(), "https://example.com"); err == nil {
		t.Fatal("expected an error when no key is configured")
	}
}
