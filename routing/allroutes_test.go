package routing_test

import (
	"testing"

	_ "github.com/strange-dragon/metaX/engine"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

// experimentalProviders are compiled only with `-tags experimental`, so a
// default build legitimately reports them as route gaps.
var experimentalProviders = map[string]struct{}{
	"AVBASE":  {},
	"MadouQu": {},
}

// TestDefaultRoutesCoverTheRealProviderInventory keeps DefaultConfig honest:
// every movie provider linked into the binary must be reachable from at least
// one category, and the only allowed gaps are the experimental providers.
func TestDefaultRoutesCoverTheRealProviderInventory(t *testing.T) {
	registered := make([]string, 0, 64)
	mt.RangeMovieFactory(func(name string, _ mt.MovieFactory) bool {
		registered = append(registered, name)
		return true
	})
	if len(registered) == 0 {
		t.Fatal("no movie providers registered; engine import did not link providers")
	}

	config := routing.DefaultConfig()
	if err := config.Validate(); err != nil {
		t.Fatalf("default route table is invalid: %v", err)
	}

	used := make(map[string]struct{}, len(registered))
	for _, category := range []routing.Category{
		routing.CategoryCensored,
		routing.CategoryUncensored,
		routing.CategoryChinese,
		routing.CategoryAmateur,
		routing.CategoryWestern,
		routing.CategoryHentai,
	} {
		resolved, err := config.Resolve(category, "", registered)
		if err != nil {
			t.Fatalf("resolve %q: %v", category, err)
		}
		for _, provider := range resolved {
			used[provider] = struct{}{}
		}
	}

	for _, name := range registered {
		if _, ok := used[name]; !ok {
			t.Errorf("provider %q is compiled in but not routed by routing.DefaultConfig()", name)
		}
	}

	coverage := config.ProviderCoverage(registered)
	for _, gap := range coverage.Gaps {
		for _, missing := range gap.Missing {
			if _, allowed := experimentalProviders[missing]; !allowed {
				t.Errorf("route %q references unregistered provider %q", gap.Category, missing)
			}
		}
	}
	if len(coverage.Empty) != 0 {
		t.Errorf("default route table has empty categories: %v", coverage.Empty)
	}
}
