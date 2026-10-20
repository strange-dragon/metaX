// Copyright 2026 metaX contributors.
package javdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestSetConfigSendsCookiesWithDetailRequests(t *testing.T) {
	var cookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie = r.Header.Get("Cookie")
		_, _ = fmt.Fprint(w, detailFixture)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	require.NoError(t, p.SetConfig(testConfig{"cookies": "session=abc; cf_clearance=xyz"}))
	_, err := p.GetMovieInfoByURL(server.URL + "/v/sample")
	require.NoError(t, err)
	require.Equal(t, "session=abc; cf_clearance=xyz", cookie)
}

func TestSetConfigAcceptsSingularCookieKeyAndClears(t *testing.T) {
	var cookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		_, _ = fmt.Fprint(w, "<html></html>")
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	require.NoError(t, p.SetConfig(testConfig{"cookie": "a=1"}))
	_, err := p.Fetch(server.URL + "/")
	require.NoError(t, err)
	require.NoError(t, p.SetConfig(testConfig{"cookies": " "}))
	_, err = p.Fetch(server.URL + "/")
	require.NoError(t, err)
	require.Equal(t, []string{"a=1", ""}, cookies)
}

func TestSetConfigRejectsHeaderInjection(t *testing.T) {
	p := New()
	require.Error(t, p.SetConfig(testConfig{"cookies": "a=1\r\nX-Injected: yes"}))
}
