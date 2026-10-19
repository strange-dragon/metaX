// Package routing defines ordered, category-based provider selection.
// It deliberately has no dependency on MetaTube's HTTP or provider packages so
// routing behavior can be tested independently and embedded at the SDK boundary.
package routing

import (
	"errors"
	"fmt"
	"strings"
)

type Category string

const (
	CategoryCensored         Category = "censored"
	CategoryUncensored       Category = "uncensored"
	CategoryFC2              Category = "fc2"
	CategoryChineseSubtitles Category = "chinese_subtitles"
	CategoryAmateur          Category = "amateur"
	CategoryWestern          Category = "western"
	CategoryAnimation        Category = "animation"
)

var (
	ErrUnknownCategory     = errors.New("unknown content category")
	ErrNoAvailableProvider = errors.New("no registered provider for category")
	ErrProviderUnavailable = errors.New("explicit provider is not registered")
)

// Config maps each content category to providers in preferred execution order.
// Provider names are matched case-insensitively, but returned in their
// registered canonical spelling.
type Config struct {
	Routes map[Category][]string
}

// DefaultConfig mirrors the route order audited from Amane. Some entries are
// intentionally retained even when a given MetaTube build does not register
// them yet; Resolve skips unavailable providers without changing the order of
// the providers that are available.
func DefaultConfig() Config {
	return Config{Routes: map[Category][]string{
		CategoryCensored:         {"DMM", "JavDB", "JavBus", "Official"},
		CategoryUncensored:       {"JavDB", "JavBus", "AVSOX", "Freejavbt"},
		CategoryFC2:              {"JavDB", "FC2PPVDB", "FC2", "Freejavbt"},
		CategoryChineseSubtitles: {"IQQTV", "JavDB", "Airav", "Freejavbt"},
		CategoryAmateur:          {"MGStage", "DMM", "JavDB", "JavBus"},
		CategoryWestern:          {"ThePornDB", "JavDB", "Freejavbt"},
		CategoryAnimation:        {"Getchu", "DMM", "JavDB"},
	}}
}

// Validate checks route configuration for empty categories, empty provider
// names, and duplicate providers within one route.
func (c Config) Validate() error {
	if len(c.Routes) == 0 {
		return errors.New("routing config has no categories")
	}
	for category, providers := range c.Routes {
		if strings.TrimSpace(string(category)) == "" {
			return errors.New("routing config contains an empty category")
		}
		if len(providers) == 0 {
			return fmt.Errorf("category %q has no providers", category)
		}
		seen := make(map[string]struct{}, len(providers))
		for _, provider := range providers {
			key := normalize(provider)
			if key == "" {
				return fmt.Errorf("category %q contains an empty provider", category)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("category %q contains duplicate provider %q", category, provider)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

// Resolve returns the registered providers in configured preference order.
// An explicit provider is a manual override and takes precedence over category
// routing. If a category has no registered providers, the caller gets an error
// rather than silently searching every provider.
func (c Config) Resolve(category Category, explicitProvider string, registered []string) ([]string, error) {
	canonical := make(map[string]string, len(registered))
	for _, name := range registered {
		if key := normalize(name); key != "" {
			canonical[key] = name
		}
	}

	if name := normalize(explicitProvider); name != "" {
		if registeredName, ok := canonical[name]; ok {
			return []string{registeredName}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, explicitProvider)
	}

	providers, ok := c.Routes[category]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCategory, category)
	}

	result := make([]string, 0, len(providers))
	for _, provider := range providers {
		if registeredName, exists := canonical[normalize(provider)]; exists {
			result = append(result, registeredName)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoAvailableProvider, category)
	}
	return result, nil
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
