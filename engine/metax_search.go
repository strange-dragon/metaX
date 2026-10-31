package engine

import (
	"sort"
	"sync"
	"time"

	"github.com/strange-dragon/metaX/collection/sets"
	"github.com/strange-dragon/metaX/common/comparer"
	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
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

	// The configured category order is the Amane content-route priority. It is
	// the primary result order; match quality only orders results within the
	// same provider. The SDK's numeric Provider.Priority() is not used here.
	providerOrder := make(map[string]int, len(providers))
	for i, provider := range providers {
		providerOrder[provider.Name()] = i
	}

	type response struct {
		results []*model.MovieSearchResult
		err     error
		elapsed time.Duration
	}
	// The split between providers must not let one straggler hold the answer.
	// A plain wg.Wait() meant a provider whose own timeout is 20s set the
	// latency of every category search, and a Jellyfin library refresh queues
	// one search per file, so the whole scan looked hung. The budget keeps the
	// answers that arrived and reports the rest as timed out.
	var (
		mu        sync.Mutex
		responses = make([]response, len(providers))
		settled   = make([]bool, len(providers))
	)
	var wg sync.WaitGroup
	wg.Add(len(providers))
	for i, provider := range providers {
		i, provider := i, provider
		go func() {
			defer wg.Done()
			release := aggregateFanoutLimiter.acquire()
			defer release()
			started := time.Now()
			results, err := e.searchMovie(keyword, provider, fallback)
			mu.Lock()
			responses[i] = response{
				results: results,
				err:     err,
				elapsed: time.Since(started),
			}
			settled[i] = true
			mu.Unlock()
		}()
	}
	budget := searchBudget()
	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allDone)
	}()
	if budget > 0 {
		select {
		case <-allDone:
		case <-time.After(budget):
		}
	} else {
		<-allDone
	}

	mu.Lock()
	snapshot := make([]response, len(responses))
	copy(snapshot, responses)
	answered := make([]bool, len(settled))
	copy(answered, settled)
	mu.Unlock()

	results := make([]*model.MovieSearchResult, 0)
	for i, provider := range providers {
		response := snapshot[i]
		switch {
		case !answered[i]:
			e.logger.Printf("Search keyword %s: %s: <still running after %s, skipped>",
				keyword, provider.Name(), budget)
		case response.err != nil:
			e.logger.Printf("Search keyword %s: %s(%s): <%v>", keyword, provider.Name(), response.elapsed, response.err)
		default:
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
		leftOrder, leftOK := providerOrder[leftProvider.Name()]
		rightOrder, rightOK := providerOrder[rightProvider.Name()]
		if leftOK && rightOK && leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if leftOK != rightOK {
			return leftOK
		}
		leftScore := comparer.Compare(keyword, left.Number)
		rightScore := comparer.Compare(keyword, right.Number)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return leftProvider.Name() < rightProvider.Name()
	})
	if len(results) == 0 {
		return nil, mt.ErrInfoNotFound
	}
	return results, nil
}
