package engine

import (
	"strings"

	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

// lookupProviderIDByNumber resolves a catalog number from the local metadata
// cache before any provider is contacted. It is intentionally best-effort:
// query errors and partial rows fall back to the live route search, so a
// damaged cache cannot make a previously scrapable number unavailable.
func (e *Engine) lookupProviderIDByNumber(number string, providers []string) (providerid.ProviderID, bool) {
	if e == nil || e.db == nil || len(providers) == 0 {
		return providerid.ProviderID{}, false
	}
	number = strings.TrimSpace(number)
	if number == "" {
		return providerid.ProviderID{}, false
	}
	keys := routing.NumberLookupKeys(number)
	if len(keys) == 0 {
		return providerid.ProviderID{}, false
	}

	// Keep the configured route order so a number stored by several providers
	// still resolves to the same route-first identity as a live search.
	routeIndex := make(map[string]int, len(providers))
	lowerProviders := make([]string, 0, len(providers))
	for index, name := range providers {
		folded := strings.ToLower(strings.TrimSpace(name))
		if folded == "" {
			continue
		}
		if _, exists := routeIndex[folded]; exists {
			continue
		}
		routeIndex[folded] = index
		lowerProviders = append(lowerProviders, folded)
	}
	if len(lowerProviders) == 0 {
		return providerid.ProviderID{}, false
	}

	var rows []model.MovieInfo
	err := e.db.Model(&model.MovieInfo{}).
		Select("provider", "id", "number", "title", "cover_url", "homepage").
		// This mirrors routing.NormalizeNumber: case-insensitive, and hyphens
		// and spaces are insignificant. Underscores stay significant.
		Where("LOWER(REPLACE(REPLACE(TRIM(number), '-', ''), ' ', '')) IN ?", keys).
		Where("LOWER(provider) IN ?", lowerProviders).
		Find(&rows).Error
	if err != nil {
		return providerid.ProviderID{}, false
	}

	bestIndex := len(providers)
	bestID := ""
	for _, row := range rows {
		if !row.IsValid() || !routing.SameNumber(row.Number, number) {
			continue
		}
		index, ok := routeIndex[strings.ToLower(strings.TrimSpace(row.Provider))]
		if !ok {
			continue
		}
		if index < bestIndex {
			bestIndex = index
			bestID = row.ID
		}
	}
	if bestID == "" {
		return providerid.ProviderID{}, false
	}
	return providerid.ProviderID{Provider: providers[bestIndex], ID: bestID}, true
}
