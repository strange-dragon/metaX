package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigAcceptsWrappedAndFlatForms(t *testing.T) {
	want := Config{Routes: map[Category][]string{
		CategoryCensored:   {"JavDB", "JavBus"},
		CategoryUncensored: {"JavDB", "FC2"},
	}}
	for _, data := range []string{
		`{"routes":{"censored":["JavDB","JavBus"],"uncensored":["JavDB","FC2"]}}`,
		`{"censored":["JavDB","JavBus"],"uncensored":["JavDB","FC2"]}`,
	} {
		got, err := ParseConfig([]byte(data))
		if err != nil {
			t.Fatalf("ParseConfig(%s) error = %v", data, err)
		}
		if got.Fingerprint() != want.Fingerprint() {
			t.Fatalf("ParseConfig(%s) = %q, want %q", data, got.Fingerprint(), want.Fingerprint())
		}
	}
}

func TestParseConfigRejectsInvalidRoutes(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "empty", data: ``, want: "empty"},
		{name: "syntax", data: `{`, want: "invalid routing config JSON"},
		{name: "routes not object", data: `{"routes":[]}`, want: "routes"},
		{name: "unknown category", data: `{"routes":{"bogus":["JavDB"]}}`, want: "unknown category"},
		{name: "legacy alias", data: `{"routes":{"fc2":["FC2"]}}`, want: "legacy alias"},
		{name: "empty route", data: `{"routes":{"censored":[]}}`, want: "no providers"},
		{name: "duplicate provider", data: `{"routes":{"censored":["JavDB"," javdb "]}}`, want: "duplicate provider"},
		{name: "extra top-level field", data: `{"routes":{"censored":["JavDB"]},"extra":true}`, want: "other top-level fields"},
		{name: "provider not array", data: `{"censored":"JavDB"}`, want: "array of provider names"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.data))
			if err == nil {
				t.Fatalf("ParseConfig(%s) error = nil, want %q", tc.data, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseConfig(%s) error = %q, want substring %q", tc.data, err, tc.want)
			}
		})
	}
}

func TestLoadConfigFromEnvPrefersInlineJSONThenFileThenDefault(t *testing.T) {
	t.Setenv(EnvRoutes, "")
	t.Setenv(EnvRoutesFile, "")

	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(`{"routes":{"censored":["FileProvider"]}}`), 0o600); err != nil {
		t.Fatalf("write routing config: %v", err)
	}

	t.Setenv(EnvRoutesFile, path)
	t.Setenv(EnvRoutes, `{"routes":{"censored":["InlineProvider"]}}`)
	config, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load inline routing config: %v", err)
	}
	if got := config.Routes[CategoryCensored]; len(got) != 1 || got[0] != "InlineProvider" {
		t.Fatalf("inline config = %v, want [InlineProvider]", got)
	}

	t.Setenv(EnvRoutes, "")
	config, err = LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load file routing config: %v", err)
	}
	if got := config.Routes[CategoryCensored]; len(got) != 1 || got[0] != "FileProvider" {
		t.Fatalf("file config = %v, want [FileProvider]", got)
	}

	t.Setenv(EnvRoutesFile, "")
	config, err = LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load default routing config: %v", err)
	}
	if config.Fingerprint() != DefaultConfig().Fingerprint() {
		t.Fatalf("default config fingerprint = %q, want %q", config.Fingerprint(), DefaultConfig().Fingerprint())
	}
}

func TestInitActiveConfigFromEnvKeepsPreviousConfigOnError(t *testing.T) {
	t.Cleanup(func() { _ = SetActiveConfig(DefaultConfig()) })
	previous := Config{Routes: map[Category][]string{
		CategoryCensored: {"PreviousProvider"},
	}}
	if err := SetActiveConfig(previous); err != nil {
		t.Fatalf("set previous config: %v", err)
	}

	t.Setenv(EnvRoutes, `{"routes":{"bogus":["JavDB"]}}`)
	if err := InitActiveConfigFromEnv(); err == nil {
		t.Fatal("InitActiveConfigFromEnv() error = nil, want invalid category error")
	}
	if got := ActiveConfig().Fingerprint(); got != previous.Fingerprint() {
		t.Fatalf("active config changed after invalid init: %q, want %q", got, previous.Fingerprint())
	}

	t.Setenv(EnvRoutes, `{"routes":{"censored":["LoadedProvider"]}}`)
	if err := InitActiveConfigFromEnv(); err != nil {
		t.Fatalf("init valid config: %v", err)
	}
	if got := ActiveConfig().Routes[CategoryCensored]; len(got) != 1 || got[0] != "LoadedProvider" {
		t.Fatalf("active route = %v, want [LoadedProvider]", got)
	}
}

func TestSetActiveConfigCopiesRouteSlices(t *testing.T) {
	t.Cleanup(func() { _ = SetActiveConfig(DefaultConfig()) })
	providers := []string{"JavDB", "JavBus"}
	config := Config{Routes: map[Category][]string{
		CategoryCensored: providers,
	}}
	if err := SetActiveConfig(config); err != nil {
		t.Fatalf("SetActiveConfig() error = %v", err)
	}
	providers[0] = "Mutated"
	if got := ActiveConfig().Routes[CategoryCensored][0]; got != "JavDB" {
		t.Fatalf("active config observed caller mutation: %q", got)
	}
}
