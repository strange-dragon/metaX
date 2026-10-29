package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

const (
	// EnvRoutesFile points at a JSON route-table file.
	EnvRoutesFile = "METAX_ROUTES_FILE"
	// EnvRoutes contains an inline JSON route table and takes precedence over
	// EnvRoutesFile when both are set.
	EnvRoutes = "METAX_ROUTES"
	// EnvRoutesFingerprint pins the expected Config.FingerprintHash value. It
	// lets multi-instance deployments fail fast when route tables drift.
	EnvRoutesFingerprint = "METAX_ROUTES_FINGERPRINT"
)

// ActiveConfig is the process-wide route table used by the stock upstream
// endpoints. It defaults to DefaultConfig and is replaced at startup by
// InitActiveConfigFromEnv.
var (
	activeConfigMu sync.RWMutex
	activeConfig   = DefaultConfig()
)

// ActiveConfig returns the current route table. Callers must treat the result
// as immutable; use SetActiveConfig to replace it.
func ActiveConfig() Config {
	activeConfigMu.RLock()
	defer activeConfigMu.RUnlock()
	return activeConfig
}

// SetActiveConfig validates and installs a route table. It is safe to call from
// tests or embedders that do not use the command-line server.
func SetActiveConfig(config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	activeConfigMu.Lock()
	activeConfig = cloneConfig(config)
	activeConfigMu.Unlock()
	return nil
}

// InitActiveConfigFromEnv loads and validates the route table from EnvRoutes or
// EnvRoutesFile, then installs it. With neither variable set it installs
// DefaultConfig. An invalid value returns an error and leaves the previous
// table unchanged, which lets the server fail fast during startup.
func InitActiveConfigFromEnv() error {
	config, err := LoadConfigFromEnv()
	if err != nil {
		return err
	}
	if expected := normalizeFingerprint(os.Getenv(EnvRoutesFingerprint)); expected != "" {
		if actual := config.FingerprintHash(); expected != actual {
			return fmt.Errorf("routing config fingerprint mismatch: expected %s, got %s", expected, actual)
		}
	}
	return SetActiveConfig(config)
}

func normalizeFingerprint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimPrefix(value, "sha256:")
}

// LoadConfigFromEnv reads the route table selected by EnvRoutes or
// EnvRoutesFile. Inline JSON wins over a file path. With neither set, the
// built-in table is chosen by EnvRouteProfile: "direct" (datacenter-friendly,
// the default when no proxy is configured) or "full" (every provider).
func LoadConfigFromEnv() (Config, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvRoutes)); raw != "" {
		return ParseConfig([]byte(raw))
	}
	if path := strings.TrimSpace(os.Getenv(EnvRoutesFile)); path != "" {
		return LoadConfig(path)
	}
	profile, err := RouteProfile()
	if err != nil {
		return Config{}, err
	}
	return ConfigForProfile(profile), nil
}

// LoadConfig reads and validates a JSON route-table file.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read routing config %s: %w", path, err)
	}
	config, err := ParseConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse routing config %s: %w", path, err)
	}
	return config, nil
}

// ParseConfig accepts either {"routes": {"censored": ["JavDB"]}} or the bare
// {"censored": ["JavDB"]} form. Unknown top-level fields and unknown or legacy
// category names are rejected so a typo cannot silently disable a route.
func ParseConfig(data []byte) (Config, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Config{}, errors.New("routing config is empty")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("invalid routing config JSON: %w", err)
	}

	var routes map[Category][]string
	if wrapped, exists := raw["routes"]; exists {
		if len(raw) != 1 {
			return Config{}, errors.New(`routing config with a "routes" field must not contain other top-level fields`)
		}
		if err := json.Unmarshal(wrapped, &routes); err != nil {
			return Config{}, fmt.Errorf(`routing config "routes" must be an object: %w`, err)
		}
	} else {
		routes = make(map[Category][]string, len(raw))
		for category, providersRaw := range raw {
			var providers []string
			if err := json.Unmarshal(providersRaw, &providers); err != nil {
				return Config{}, fmt.Errorf("category %q must be an array of provider names: %w", category, err)
			}
			routes[Category(category)] = providers
		}
	}

	config := Config{Routes: routes}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func cloneConfig(config Config) Config {
	routes := make(map[Category][]string, len(config.Routes))
	for category, providers := range config.Routes {
		routes[category] = append([]string(nil), providers...)
	}
	return Config{Routes: routes}
}
