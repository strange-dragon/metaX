package routing

import (
	"context"
	"sync"
)

// ProviderResult retains source identity so each match can be resolved by its
// original provider and ID. Results are not merged or deduplicated here.
type ProviderResult[T any] struct {
	Provider string
	Items    []T
}

// SearchAll fans out to every selected provider concurrently. Empty results and
// provider errors are treated as that provider contributing no matches; other
// providers can still contribute results. Output order follows providers, not
// completion order, for deterministic responses.
func SearchAll[T any](ctx context.Context, providers []string, search func(context.Context, string) ([]T, error)) []ProviderResult[T] {
	if len(providers) == 0 || search == nil {
		return nil
	}
	type outcome struct {
		items []T
	}
	outcomes := make([]outcome, len(providers))
	var wg sync.WaitGroup
	wg.Add(len(providers))
	for i, provider := range providers {
		i, provider := i, provider
		go func() {
			defer wg.Done()
			items, err := search(ctx, provider)
			if err != nil || len(items) == 0 {
				return
			}
			outcomes[i] = outcome{items: items}
		}()
	}
	wg.Wait()
	results := make([]ProviderResult[T], 0, len(providers))
	for i, provider := range providers {
		if len(outcomes[i].items) > 0 {
			results = append(results, ProviderResult[T]{Provider: provider, Items: outcomes[i].items})
		}
	}
	return results
}
