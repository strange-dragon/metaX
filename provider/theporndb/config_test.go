package theporndb

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/strange-dragon/metaX/provider"
)

type configTestValues map[string]string

func (c configTestValues) Has(key string) bool {
	_, ok := c.lookup(key)
	return ok
}

func (c configTestValues) lookup(key string) (string, bool) {
	for name, value := range c {
		if strings.EqualFold(name, key) {
			return value, true
		}
	}
	return "", false
}

func (c configTestValues) GetString(key string) (string, error) {
	value, ok := c.lookup(key)
	if !ok {
		return "", errors.New("key not found")
	}
	return value, nil
}

func (c configTestValues) GetBool(string) (bool, error) { return false, errors.New("key not found") }
func (c configTestValues) GetInt64(string) (int64, error) {
	return 0, errors.New("key not found")
}
func (c configTestValues) GetFloat64(string) (float64, error) {
	return 0, errors.New("key not found")
}
func (c configTestValues) GetDuration(string) (time.Duration, error) {
	return 0, errors.New("key not found")
}

var (
	_ provider.ConfigSetter = (*ThePornDBActor)(nil)
	_ provider.ConfigSetter = (*ThePornDBVideo)(nil)
)

func TestThePornDBSetConfigReadsAccessToken(t *testing.T) {
	actor := NewThePornDBActor()
	if err := actor.SetConfig(configTestValues{"ACCESS_TOKEN": " actor-token "}); err != nil {
		t.Fatalf("actor SetConfig() error = %v", err)
	}
	if actor.accessToken != "actor-token" {
		t.Fatalf("actor accessToken = %q, want actor-token", actor.accessToken)
	}

	movie := NewThePornDBMovie()
	if err := movie.SetConfig(configTestValues{"access_token": "movie-token"}); err != nil {
		t.Fatalf("movie SetConfig() error = %v", err)
	}
	if movie.accessToken != "movie-token" {
		t.Fatalf("movie accessToken = %q, want movie-token", movie.accessToken)
	}
}

func TestThePornDBSetConfigReadsAPITokenAlias(t *testing.T) {
	actor := NewThePornDBActor()
	if err := actor.SetConfig(configTestValues{"API_TOKEN": "amanetoken"}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if actor.accessToken != "amanetoken" {
		t.Fatalf("accessToken = %q, want amanetoken", actor.accessToken)
	}
}

func TestThePornDBSetConfigAppliesTransportCookies(t *testing.T) {
	actor := NewThePornDBActor()
	if err := actor.SetConfig(configTestValues{"cookies": "not-a-cookie"}); err == nil {
		t.Fatal("actor SetConfig() accepted malformed cookies")
	}

	movie := NewThePornDBMovie()
	if err := movie.SetConfig(configTestValues{"cookie": "not-a-cookie"}); err == nil {
		t.Fatal("movie SetConfig() accepted malformed cookies")
	}
}

func TestThePornDBSetConfigPreservesTokenWhenAbsent(t *testing.T) {
	actor := NewThePornDBActor()
	actor.accessToken = "existing"
	if err := actor.SetConfig(configTestValues{}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if actor.accessToken != "existing" {
		t.Fatalf("accessToken = %q, want existing", actor.accessToken)
	}
	if err := actor.SetConfig(nil); err != nil {
		t.Fatalf("SetConfig(nil) error = %v", err)
	}
	if actor.accessToken != "existing" {
		t.Fatalf("accessToken after nil config = %q, want existing", actor.accessToken)
	}
}

func TestThePornDBSetConfigReadsTokenFromPluginCookieField(t *testing.T) {
	actor := NewThePornDBActor()
	if err := actor.SetConfig(configTestValues{"cookies": "theme=dark; access_token= cookie-token "}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if actor.accessToken != "cookie-token" {
		t.Fatalf("accessToken = %q, want cookie-token", actor.accessToken)
	}

	movie := NewThePornDBMovie()
	if err := movie.SetConfig(configTestValues{"cookie": "api_token=api-cookie-token"}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if movie.accessToken != "api-cookie-token" {
		t.Fatalf("accessToken = %q, want api-cookie-token", movie.accessToken)
	}
}
