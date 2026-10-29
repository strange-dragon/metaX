package engine

import (
	"os"
	"strings"
)

// MergePreference names the provider whose values should win for a group of
// fields before route order is consulted. Route order stays the fallback for
// every field, so a provider that failed or was not queried never blocks the
// merged result.
//
// The two groups exist because they have different failure modes in practice:
// artwork is a per-provider asset (one site's CDN may be unreachable while its
// metadata is fine), whereas text metadata benefits from the provider with the
// most complete catalogue entry.
type MergePreference struct {
	// Image prefers this provider for ThumbURL, BigThumbURL, CoverURL,
	// BigCoverURL and PreviewImages.
	Image string
	// Detail prefers this provider for Title, Summary, Director, Actors,
	// Genres, Maker, Label, Series and the other scalar metadata.
	Detail string
}

const (
	// EnvMergeImageProvider overrides the artwork preference.
	EnvMergeImageProvider = "METAX_MERGE_IMAGE_PROVIDER"
	// EnvMergeDetailProvider overrides the metadata preference.
	EnvMergeDetailProvider = "METAX_MERGE_DETAIL_PROVIDER"
)

// Defaults match the deployment these were tuned for: Jav321 serves artwork
// from the DMM picture CDN, which stays reachable where JavDB's image host is
// not, while JavDB carries the most complete Chinese metadata for the same
// catalog number.
const (
	defaultMergeImageProvider  = "Jav321"
	defaultMergeDetailProvider = "JavDB"
)

// ActiveMergePreference reads the process-wide field preferences. Each lookup
// is deliberately cheap and uncached so tests and embedders can change the
// environment between calls without restarting the process.
func ActiveMergePreference() MergePreference {
	return MergePreference{
		Image:  mergeProviderFromEnv(EnvMergeImageProvider, defaultMergeImageProvider),
		Detail: mergeProviderFromEnv(EnvMergeDetailProvider, defaultMergeDetailProvider),
	}
}

// mergeProviderFromEnv returns the configured provider name, or "" when the
// preference is switched off and route order should decide every field.
func mergeProviderFromEnv(name, fallback string) string {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	raw = strings.TrimSpace(raw)
	switch strings.ToLower(raw) {
	case "", "off", "none", "route", "default":
		return ""
	}
	return raw
}

// OrderByProviderPreference moves the entry produced by provider to the front
// of items, keeping every other entry in its original position. It returns
// items unchanged when provider is empty or absent, so callers can apply a
// preference without checking whether the route contains that provider.
func OrderByProviderPreference[T any](items []T, provider string, providerOf func(T) string) []T {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return items
	}
	index := -1
	for i, item := range items {
		if strings.EqualFold(strings.TrimSpace(providerOf(item)), provider) {
			index = i
			break
		}
	}
	if index <= 0 {
		return items
	}
	ordered := make([]T, 0, len(items))
	ordered = append(ordered, items[index])
	ordered = append(ordered, items[:index]...)
	ordered = append(ordered, items[index+1:]...)
	return ordered
}
