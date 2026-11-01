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
	// EnvArtworkLastResort lists the providers whose artwork is only used when
	// no other source in the chain published an image.
	EnvArtworkLastResort = "METAX_ARTWORK_LAST_RESORT"
)

// Defaults match the deployment these were tuned for: Jav321 serves artwork
// from the DMM picture CDN, which stays reachable where JavDB's image host is
// not, while JavDB carries the most complete Chinese metadata for the same
// catalog number.
const (
	defaultMergeImageProvider  = "Jav321"
	defaultMergeDetailProvider = "JavDB"
)

// defaultArtworkLastResort keeps JavDB at the back of the artwork chain.
//
// JavDB is the best source for text and the worst for artwork on the same
// record: its covers are served through a hotlink-protected image host that
// answers a placeholder to many clients, and it publishes one size, so a
// merged record could end up with a JavDB cover while a source next to it held
// the full-size original. Demoting is not dropping: if every other source in
// the chain came back without an image, JavDB still supplies one.
const defaultArtworkLastResort = "JavDB"

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

// ActiveArtworkLastResort reads the providers whose artwork may only be used as
// a last resort. It returns nil when the behaviour is switched off, which
// restores plain route order.
func ActiveArtworkLastResort() []string {
	raw, exists := os.LookupEnv(EnvArtworkLastResort)
	if !exists {
		raw = defaultArtworkLastResort
	}
	names := make([]string, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "off", "none":
			// "off" anywhere in the list disables the whole rule, so a
			// deployment that wants the old order does not have to guess.
			return nil
		}
		names = append(names, part)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// DemoteProviders moves the entries produced by the named providers to the end
// of items, preserving the relative order of both halves. It returns items
// unchanged when none of the names matches, so callers can demote a provider
// that is not in this particular chain without checking first.
//
// This is the difference between "prefer" and "fall back to": a preference can
// still be overturned by the other ordering rules, which is how a source with
// a worse image but a large variant kept winning the cover.
func DemoteProviders[T any](items []T, providers []string, providerOf func(T) string) []T {
	if len(providers) == 0 || len(items) < 2 {
		return items
	}
	demoted := make(map[string]struct{}, len(providers))
	for _, name := range providers {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			demoted[name] = struct{}{}
		}
	}
	kept := make([]T, 0, len(items))
	last := make([]T, 0, len(items))
	for _, item := range items {
		if _, ok := demoted[strings.ToLower(strings.TrimSpace(providerOf(item)))]; ok {
			last = append(last, item)
			continue
		}
		kept = append(kept, item)
	}
	if len(last) == 0 || len(kept) == 0 {
		return items
	}
	return append(kept, last...)
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
