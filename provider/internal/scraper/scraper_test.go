package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/language"
)

type testConfig map[string]string

func (c testConfig) Has(key string) bool {
	_, ok := c[strings.ToLower(key)]
	return ok
}

func (c testConfig) GetString(key string) (string, error) {
	value, ok := c[strings.ToLower(key)]
	if !ok {
		return "", fmt.Errorf("missing config key %q", key)
	}
	return value, nil
}

func (c testConfig) GetBool(string) (bool, error)              { return false, nil }
func (c testConfig) GetInt64(string) (int64, error)            { return 0, nil }
func (c testConfig) GetFloat64(string) (float64, error)        { return 0, nil }
func (c testConfig) GetDuration(string) (time.Duration, error) { return 0, nil }

func TestSetConfigInstallsCookiesForBaseURL(t *testing.T) {
	var cookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie = r.Header.Get("Cookie")
		_, _ = fmt.Fprint(w, "<html></html>")
	}))
	defer server.Close()

	s := NewDefaultScraper("Test", server.URL, 1, language.English)
	if err := s.SetConfig(testConfig{"cookies": "session=abc; cf_clearance=xyz"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if err := s.ClonedCollector().Visit(server.URL); err != nil {
		t.Fatalf("Visit: %v", err)
	}
	if cookie != "session=abc; cf_clearance=xyz" {
		t.Fatalf("Cookie = %q", cookie)
	}
}

func TestSetConfigCookieValidation(t *testing.T) {
	s := NewDefaultScraper("Test", "https://example.test", 1, language.English)
	err := s.SetConfig(testConfig{"cookie": "not-a-cookie"})
	if err == nil {
		t.Fatal("expected invalid cookie error")
	}
	if strings.Contains(err.Error(), "not-a-cookie") {
		t.Fatalf("cookie validation error leaked the configured value: %v", err)
	}
	if err := s.SetConfig(testConfig{"cookies": "a=1\r\nX-Injected: yes"}); err == nil {
		t.Fatal("expected header injection error")
	}
	if err := s.SetConfig(testConfig{"cookies": " "}); err != nil {
		t.Fatalf("empty cookies: %v", err)
	}
	if err := s.SetConfig(testConfig{}); err != nil {
		t.Fatalf("missing cookies: %v", err)
	}
}
