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
	CategoryHentai           Category = "hentai"
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

// DefaultConfig merges the uncensored and FC2 routes into one uncensored category.
// FC2 remains accepted as a legacy category alias in Resolve. Some providers
// may not be registered by a given MetaTube build; Resolve skips those entries
// without changing the configured order of the available providers.
func DefaultConfig() Config {
	return Config{Routes: map[Category][]string{
		CategoryCensored:         {"DMM", "JavDB", "JavBus", "Official"},
		CategoryUncensored:       {"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"},
		CategoryChineseSubtitles: {"IQQTV", "JavDB", "Airav", "Freejavbt"},
		CategoryAmateur:          {"MGStage", "DMM", "JavDB", "JavBus"},
		CategoryWestern:          {"ThePornDB", "JavDB", "Freejavbt"},
		CategoryHentai:           {"Getchu", "DMM", "JavDB"},
		// CategoryAnimation remains an alias for callers using the earlier API label.
		CategoryAnimation: {"Getchu", "DMM", "JavDB"},
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

	// FC2 is no longer a separate content category. Keep accepting the old
	// label so existing callers receive the combined uncensored provider set.
	if category == CategoryFC2 {
		category = CategoryUncensored
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
