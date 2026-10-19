package engine

import (
	"sort"
	"sync"

	"github.com/metatube-community/metatube-sdk-go/collection/sets"
	"github.com/metatube-community/metatube-sdk-go/common/comparer"
	"github.com/metatube-community/metatube-sdk-go/common/number"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

// SearchMovieProviders searches only the supplied providers concurrently.
// Errors and empty results from individual providers do not discard successful
// results from other providers. Result identity remains provider + ID, matching
// the SDK's existing detail endpoint contract.
func (e *Engine) SearchMovieProviders(keyword string, names []string, fallback bool) ([]*model.MovieSearchResult, error) {
	keyword = number.Trim(keyword)
	if keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}

	providers := make([]mt.MovieProvider, 0, len(names))
	seenProviders := make(map[string]struct{}, len(names))
	for _, name := range names {
		provider, err := e.GetMovieProviderByName(name)
		if err != nil {
			continue
		}
		canonical := provider.Name()
		if _, exists := seenProviders[canonical]; exists {
			continue
		}
		seenProviders[canonical] = struct{}{}
		providers = append(providers, provider)
	}
	if len(providers) == 0 {
		return nil, mt.ErrProviderNotFound
	}

	type response struct {
		provider mt.MovieProvider
		results  []*model.MovieSearchResult
	}
	responses := make([]response, len(providers))
	var wg sync.WaitGroup
	wg.Add(len(providers))
	for i, provider := range providers {
		i, provider := i, provider
		go func() {
			defer wg.Done()
			results, err := e.searchMovie(keyword, provider, false)
			if err != nil {
				return
			}
			responses[i] = response{provider: provider, results: results}
		}()
	}
	wg.Wait()

	results := make([]*model.MovieSearchResult, 0)
	for i, provider := range providers {
		results = append(results, responses[i].results...)
		if fallback {
			// Keep database fallback inside the selected category; querying the
			// whole database here could leak matches from unrelated categories.
			if cached, err := e.searchMovieFromDB(keyword, provider, false); err == nil {
				results = append(results, cached...)
			}
		}
	}

	unique := sets.NewOrderedSetWithHash(func(v *model.MovieSearchResult) string {
		return v.Provider + "\x00" + v.ID
	})
	for _, result := range results {
		if result == nil || !result.IsValid() {
			continue
		}
		if !e.IsMovieProvider(result.Provider) {
			continue
		}
		unique.Add(result)
	}
	results = unique.AsSlice()
	sort.SliceStable(results, func(i, j int) bool {
		left, right := results[i], results[j]
		leftProvider, leftErr := e.GetMovieProviderByName(left.Provider)
		rightProvider, rightErr := e.GetMovieProviderByName(right.Provider)
		if leftErr != nil || rightErr != nil {
			return false
		}
		leftScore := comparer.Compare(keyword, left.Number) * leftProvider.Priority()
		rightScore := comparer.Compare(keyword, right.Number) * rightProvider.Priority()
		if leftScore == rightScore {
			return leftProvider.Priority() > rightProvider.Priority()
		}
		return leftScore > rightScore
	})
	if len(results) == 0 {
		return nil, mt.ErrInfoNotFound
	}
	return results, nil
}
