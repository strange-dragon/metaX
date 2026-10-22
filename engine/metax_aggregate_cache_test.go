package engine

import (
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

const cacheCountingProvider = "MetaXCacheCountingProvider"

type cacheCountingMovieProvider struct {
	searchCalls atomic.Int64
	detailCalls atomic.Int64
}

var cacheCountingProviderInstance = &cacheCountingMovieProvider{}

func (*cacheCountingMovieProvider) Name() string           { return cacheCountingProvider }
func (*cacheCountingMovieProvider) Priority() float64      { return 1 }
func (*cacheCountingMovieProvider) SetPriority(float64)    {}
func (*cacheCountingMovieProvider) Language() language.Tag { return language.English }
func (*cacheCountingMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://cache-counting.invalid")
	return u
}
func (*cacheCountingMovieProvider) NormalizeMovieID(id string) string { return id }
func (*cacheCountingMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}
func (p *cacheCountingMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	p.detailCalls.Add(1)
	return cacheCountingMovieInfo(id), nil
}
func (p *cacheCountingMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*cacheCountingMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (p *cacheCountingMovieProvider) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	p.searchCalls.Add(1)
	return []*model.MovieSearchResult{cacheCountingMovieInfo(cacheCountingID(keyword)).ToSearchResult()}, nil
}

func cacheCountingID(number string) string { return "cache-" + number }

func cacheCountingMovieInfo(id string) *model.MovieInfo {
	number := strings.TrimPrefix(id, "cache-")
	return &model.MovieInfo{
		ID:       id,
		Number:   number,
		Title:    "Cache title",
		Provider: cacheCountingProvider,
		Homepage: "https://cache-counting.invalid/" + id,
		CoverURL: "https://cache-counting.invalid/cover.jpg",
	}
}

func init() {
	mt.Register(cacheCountingProvider, func() *cacheCountingMovieProvider {
		return cacheCountingProviderInstance
	})
}

func resetAggregateCaches(ttl time.Duration) {
	aggregateMovieCache = newTTLCache[*AggregatedMovieInfo](ttl, 16)
	aggregateResolveCache = newTTLCache[providerid.ProviderID](ttl, 16)
}

func restoreAggregateCaches() {
	aggregateMovieCache = newTTLCache[*AggregatedMovieInfo](defaultAggregateCacheTTL, defaultAggregateCacheMax)
	aggregateResolveCache = newTTLCache[providerid.ProviderID](defaultAggregateCacheTTL, defaultAggregateCacheMax)
}

func TestTTLCacheReturnsValueBeforeExpiry(t *testing.T) {
	cache := newTTLCache[string](time.Minute, 4)
	cache.put("key", "value")
	if value, ok := cache.get("key"); !ok || value != "value" {
		t.Fatalf("get = (%q, %v), want (value, true)", value, ok)
	}
}

func TestTTLCacheExpires(t *testing.T) {
	cache := newTTLCache[string](10*time.Millisecond, 4)
	cache.put("key", "value")
	time.Sleep(25 * time.Millisecond)
	if value, ok := cache.get("key"); ok {
		t.Fatalf("get = (%q, true), want cache miss after TTL", value)
	}
}

func TestTTLCacheEvictsOldestEntry(t *testing.T) {
	cache := newTTLCache[string](time.Minute, 2)
	cache.put("first", "1")
	cache.put("second", "2")
	cache.put("third", "3")
	if _, ok := cache.get("first"); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if value, ok := cache.get("third"); !ok || value != "3" {
		t.Fatalf("third entry = (%q, %v), want (3, true)", value, ok)
	}
}

func TestTTLCacheCoalescesConcurrentLoads(t *testing.T) {
	cache := newTTLCache[string](time.Minute, 4)
	var calls atomic.Int64
	load := func() (string, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return "loaded", nil
	}

	var wg sync.WaitGroup
	values := make([]string, 8)
	for i := 0; i < len(values); i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			value, err := cache.do("same-key", load)
			if err != nil {
				t.Errorf("do: %v", err)
				return
			}
			values[index] = value
		}(i)
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("load calls = %d, want 1", got)
	}
	for i, value := range values {
		if value != "loaded" {
			t.Fatalf("values[%d] = %q, want loaded", i, value)
		}
	}
}

func TestAggregateCacheKeyMergesFC2AndUncensored(t *testing.T) {
	pid := providerid.ProviderID{Provider: cacheCountingProvider, ID: "cache-1"}
	config := routing.DefaultConfig()
	fc2 := aggregateMovieCacheKey(pid, false, routing.CategoryFC2, config)
	uncensored := aggregateMovieCacheKey(pid, false, routing.CategoryUncensored, config)
	if fc2 != uncensored {
		t.Fatalf("FC2 and uncensored must share a cache key: %q != %q", fc2, uncensored)
	}
	otherConfig := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {cacheCountingProvider},
	}}
	if fc2 == aggregateMovieCacheKey(pid, false, routing.CategoryFC2, otherConfig) {
		t.Fatal("different route tables must not share a cache key")
	}
}

func TestCloneAggregatedMovieInfoIsolatesMutableFields(t *testing.T) {
	source := &AggregatedMovieInfo{
		MovieInfo: &model.MovieInfo{Provider: "original", ID: "original-id"},
		Category:  "censored",
		Sources: []MovieSourceRecord{{
			Provider: "original",
			ID:       "original-id",
			Status:   "success",
			Metadata: &model.MovieInfo{Provider: "original", ID: "original-id"},
		}},
	}

	clone := cloneAggregatedMovieInfo(source)
	clone.Provider = "mutated"
	clone.ID = "mutated-id"
	clone.Sources[0].Provider = "mutated"
	clone.Sources[0].Metadata.Provider = "mutated"

	if source.Provider != "original" || source.ID != "original-id" {
		t.Fatalf("top-level cached identity was mutated: %+v", source.MovieInfo)
	}
	if source.Sources[0].Provider != "original" || source.Sources[0].Metadata.Provider != "original" {
		t.Fatalf("cached source record was mutated: %+v", source.Sources[0])
	}
}

func TestAggregatedMovieInfoCacheHitAvoidsSecondLookup(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)
	cacheCountingProviderInstance.detailCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}

	first, err := app.GetAggregatedMovieInfoByNumber("CACHE-001", routing.CategoryCensored, false, config)
	if err != nil {
		t.Fatalf("first aggregate: %v", err)
	}
	if first.Provider != cacheCountingProvider || first.ID != cacheCountingID("CACHE-001") {
		t.Fatalf("unexpected first identity: %s/%s", first.Provider, first.ID)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("search calls after first aggregate = %d, want 1", got)
	}
	if got := cacheCountingProviderInstance.detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls after first aggregate = %d, want 1", got)
	}

	// The route rewrites the public identity for MetaX responses. The cache must
	// not observe that mutation.
	first.Provider = "mutated"
	first.ID = "mutated"

	second, err := app.GetAggregatedMovieInfoByNumber("CACHE-001", routing.CategoryCensored, false, config)
	if err != nil {
		t.Fatalf("second aggregate: %v", err)
	}
	if second.Provider != cacheCountingProvider || second.ID != cacheCountingID("CACHE-001") {
		t.Fatalf("cached result was corrupted: %s/%s", second.Provider, second.ID)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("second aggregate performed another search: %d", got)
	}
	if got := cacheCountingProviderInstance.detailCalls.Load(); got != 1 {
		t.Fatalf("second aggregate performed another detail lookup: %d", got)
	}
}

func TestResolveAggregatedProviderIDCacheAvoidsRepeatedSearch(t *testing.T) {
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	cacheCountingProviderInstance.searchCalls.Store(0)

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {cacheCountingProvider},
	}}

	first, err := app.ResolveAggregatedProviderID("CACHE-RESOLVE-001", routing.CategoryCensored, config)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := app.ResolveAggregatedProviderID("CACHE-RESOLVE-001", routing.CategoryCensored, config)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first != second {
		t.Fatalf("resolved identities differ: %+v != %+v", first, second)
	}
	if got := cacheCountingProviderInstance.searchCalls.Load(); got != 1 {
		t.Fatalf("resolve performed %d searches, want 1", got)
	}
}
