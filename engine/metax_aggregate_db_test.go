package engine

import (
	"testing"
	"time"

	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/routing"
)

func seedAggregateDBMovie(t *testing.T, app *Engine, provider, id, number string) {
	t.Helper()
	info := &model.MovieInfo{
		ID:       id,
		Number:   number,
		Provider: provider,
		Title:    "DB title " + number,
		CoverURL: "https://db.invalid/" + id + ".jpg",
		Homepage: "https://db.invalid/" + id,
	}
	if err := app.db.Create(info).Error; err != nil {
		t.Fatalf("seed movie %s/%s: %v", provider, id, err)
	}
}

func TestResolveAggregatedProviderIDUsesDatabaseBeforeLiveSearch(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, cacheCountingProvider, "db-resolve-1", "DB-RESOLVE-001")

	pid, err := app.ResolveAggregatedProviderID("DB-RESOLVE-001", routing.CategoryCensored, config)
	if err != nil {
		t.Fatalf("resolve from database: %v", err)
	}
	if pid.Provider != cacheCountingProvider || pid.ID != "db-resolve-1" {
		t.Fatalf("resolved identity = %+v, want %s/db-resolve-1", pid, cacheCountingProvider)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 0 {
		t.Fatalf("database hit performed %d live searches, want 0", got)
	}
}

func TestResolveAggregatedProviderIDMatchesStoredFC2Variants(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, cacheCountingProvider, "db-fc2-1", "FC2-PPV-999999")
	seedAggregateDBMovie(t, app, cacheCountingProvider, "db-fc2-2", "FC2-999998")

	for _, tc := range []struct {
		number string
		wantID string
	}{
		{number: "FC2-999999", wantID: "db-fc2-1"},
		{number: "FC2PPV-999998", wantID: "db-fc2-2"},
	} {
		pid, err := app.ResolveAggregatedProviderID(tc.number, routing.CategoryUncensored, config)
		if err != nil {
			t.Fatalf("resolve %q: %v", tc.number, err)
		}
		if pid.ID != tc.wantID {
			t.Fatalf("resolve %q = %+v, want id %q", tc.number, pid, tc.wantID)
		}
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 0 {
		t.Fatalf("FC2 database hits performed %d live searches, want 0", got)
	}
}

func TestResolveAggregatedProviderIDIgnoresProvidersOutsideRoute(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, aggregateSecondProvider, "db-outside-1", "ROUTE-DB-001")

	pid, err := app.ResolveAggregatedProviderID("ROUTE-DB-001", routing.CategoryCensored, config)
	if err != nil {
		t.Fatalf("resolve with out-of-route database row: %v", err)
	}
	if pid.Provider != cacheCountingProvider {
		t.Fatalf("resolved provider = %q, want route provider %q", pid.Provider, cacheCountingProvider)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("live fallback performed %d searches, want 1", got)
	}
}

func TestAggregatedMovieInfoByNumberUsesDatabaseWhenLazy(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)
	cacheCountingProviderInstance.detailCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, cacheCountingProvider, "db-agg-1", "DB-AGG-001")

	got, err := app.GetAggregatedMovieInfoByNumber("DB-AGG-001", routing.CategoryCensored, true, config)
	if err != nil {
		t.Fatalf("aggregate from database: %v", err)
	}
	if got.Provider != cacheCountingProvider || got.ID != "db-agg-1" {
		t.Fatalf("aggregated identity = %s/%s, want %s/db-agg-1", got.Provider, got.ID, cacheCountingProvider)
	}
	if got.Category != string(routing.CategoryCensored) {
		t.Fatalf("category = %q, want censored", got.Category)
	}
	if len(got.Sources) != 1 || got.Sources[0].Status != "success" ||
		got.Sources[0].ID != "db-agg-1" {
		t.Fatalf("unexpected sources: %+v", got.Sources)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 0 {
		t.Fatalf("lazy database hit performed %d searches, want 0", got)
	}
	if got := cacheCountingProviderInstance.detailCalls.Load(); got != 0 {
		t.Fatalf("lazy database hit performed %d detail lookups, want 0", got)
	}
}

func TestAggregatedMovieInfoByNumberSkipsDatabaseWhenNotLazy(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)
	cacheCountingProviderInstance.detailCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, cacheCountingProvider, "db-agg-2", "DB-AGG-002")

	got, err := app.GetAggregatedMovieInfoByNumber("DB-AGG-002", routing.CategoryCensored, false, config)
	if err != nil {
		t.Fatalf("live aggregate: %v", err)
	}
	if got.ID != cacheCountingID("DB-AGG-002") {
		t.Fatalf("aggregated id = %q, want live id %q", got.ID, cacheCountingID("DB-AGG-002"))
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("non-lazy aggregate performed %d searches, want 1", got)
	}
	if got := cacheCountingProviderInstance.detailCalls.Load(); got != 1 {
		t.Fatalf("non-lazy aggregate performed %d detail lookups, want 1", got)
	}
}

func TestAggregatedMovieInfoByNumberFallsBackToLiveSearchOnDatabaseMiss(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)
	cacheCountingProviderInstance.detailCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}

	got, err := app.GetAggregatedMovieInfoByNumber("LIVE-DB-001", routing.CategoryCensored, true, config)
	if err != nil {
		t.Fatalf("live fallback aggregate: %v", err)
	}
	if got.ID != cacheCountingID("LIVE-DB-001") {
		t.Fatalf("aggregated id = %q, want live id %q", got.ID, cacheCountingID("LIVE-DB-001"))
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("database miss performed %d searches, want 1", got)
	}
	if got := cacheCountingProviderInstance.detailCalls.Load(); got != 1 {
		t.Fatalf("database miss performed %d detail lookups, want 1", got)
	}
}

func TestDatabaseLookupPrefersRouteOrder(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {aggregateSecondProvider, cacheCountingProvider},
	}}
	seedAggregateDBMovie(t, app, cacheCountingProvider, "cache-order", "ORDER-DB-001")
	seedAggregateDBMovie(t, app, aggregateSecondProvider, "second-order", "ORDER-DB-001")

	pid, err := app.ResolveAggregatedProviderID("ORDER-DB-001", routing.CategoryCensored, config)
	if err != nil {
		t.Fatalf("resolve route order: %v", err)
	}
	if pid.Provider != aggregateSecondProvider || pid.ID != "second-order" {
		t.Fatalf("resolved identity = %+v, want %s/second-order", pid, aggregateSecondProvider)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 0 {
		t.Fatalf("route-order database hit performed %d searches, want 0", got)
	}
}
