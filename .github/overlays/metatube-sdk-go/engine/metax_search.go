package engine

import (
	"sort"
	"sync"
	"time"

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

	// Keep the configured Amane route order available for deterministic
	// tie-breaking after concurrent searches finish.
	providerOrder := make(map[string]int, len(providers))
	for i, provider := range providers {
		providerOrder[provider.Name()] = i
	}

	type response struct {
		results []*model.MovieSearchResult
		err     error
		elapsed time.Duration
	}
	responses := make([]response, len(providers))
	var wg sync.WaitGroup
	wg.Add(len(providers))
	for i, provider := range providers {
		i, provider := i, provider
		go func() {
			defer wg.Done()
			started := time.Now()
			results, err := e.searchMovie(keyword, provider, fallback)
			responses[i] = response{
				results: results,
				err:     err,
				elapsed: time.Since(started),
			}
		}()
	}
	wg.Wait()

	results := make([]*model.MovieSearchResult, 0)
	for i, provider := range providers {
		response := responses[i]
		if response.err != nil {
			e.logger.Printf("Search keyword %s: %s(%s): <%v>", keyword, provider.Name(), response.elapsed, response.err)
		} else {
			e.logger.Printf("Search keyword %s: %s(%s): %d result(s)", keyword, provider.Name(), response.elapsed, len(response.results))
			results = append(results, response.results...)
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
			leftOrder, leftOK := providerOrder[leftProvider.Name()]
			rightOrder, rightOK := providerOrder[rightProvider.Name()]
			if leftOK && rightOK && leftOrder != rightOrder {
				return leftOrder < rightOrder
			}
			return leftProvider.Priority() > rightProvider.Priority()
		}
		return leftScore > rightScore
	})
	if len(results) == 0 {
		return nil, mt.ErrInfoNotFound
	}
	return results, nil
}
