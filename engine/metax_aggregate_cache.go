package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/routing"
)

const (
	defaultAggregateCacheTTL = 10 * time.Minute
	defaultAggregateCacheMax = 256
)

// ttlCache is a small bounded cache with request coalescing. It deliberately
// keeps successful values only: aggregation errors are transient and must not
// be replayed for the whole TTL.
type ttlCache[T any] struct {
	mu      sync.Mutex
	entries map[string]ttlCacheEntry[T]
	order   []string
	ttl     time.Duration
	max     int
	group   singleflight.Group
}

type ttlCacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

func newTTLCache[T any](ttl time.Duration, max int) *ttlCache[T] {
	if max <= 0 {
		max = defaultAggregateCacheMax
	}
	return &ttlCache[T]{
		entries: make(map[string]ttlCacheEntry[T]),
		ttl:     ttl,
		max:     max,
	}
}

func (c *ttlCache[T]) enabled() bool {
	return c != nil && c.ttl > 0
}

func (c *ttlCache[T]) get(key string) (T, bool) {
	var zero T
	if !c.enabled() {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) put(key string, value T) {
	if !c.enabled() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = ttlCacheEntry[T]{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	for len(c.entries) > c.max && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

// do returns a cached value when possible and otherwise runs load at most once
// for concurrent callers with the same key.
func (c *ttlCache[T]) do(key string, load func() (T, error)) (T, error) {
	if !c.enabled() {
		return load()
	}
	if value, ok := c.get(key); ok {
		return value, nil
	}

	value, err, _ := c.group.Do(key, func() (any, error) {
		if cached, ok := c.get(key); ok {
			return cached, nil
		}
		loaded, loadErr := load()
		if loadErr != nil {
			return nil, loadErr
		}
		c.put(key, loaded)
		return loaded, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return value.(T), nil
}

func aggregateCacheTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("METAX_AGGREGATE_CACHE_TTL"))
	if raw == "" {
		return defaultAggregateCacheTTL
	}
	if raw == "0" || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "disabled") {
		return 0
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 {
		return defaultAggregateCacheTTL
	}
	return ttl
}

func aggregateCacheMaxFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("METAX_AGGREGATE_CACHE_MAX"))
	if raw == "" {
		return defaultAggregateCacheMax
	}
	max, err := strconv.Atoi(raw)
	if err != nil || max <= 0 {
		return defaultAggregateCacheMax
	}
	return max
}

var (
	aggregateMovieCache   = newTTLCache[*AggregatedMovieInfo](aggregateCacheTTLFromEnv(), aggregateCacheMaxFromEnv())
	aggregateResolveCache = newTTLCache[providerid.ProviderID](aggregateCacheTTLFromEnv(), aggregateCacheMaxFromEnv())
)

func aggregateMovieCacheKey(pid providerid.ProviderID, lazy bool, category routing.Category, config routing.Config) string {
	return fmt.Sprintf("movie|%s|%s|%t|%s|%s",
		strings.ToLower(strings.TrimSpace(pid.Provider)),
		pid.ID,
		lazy,
		category.Canonical(),
		config.Fingerprint(),
	)
}

func aggregateNumberCacheKey(number string, lazy bool, category routing.Category, config routing.Config) string {
	return fmt.Sprintf("number|%s|%t|%s|%s",
		strings.ToLower(strings.TrimSpace(number)),
		lazy,
		category.Canonical(),
		config.Fingerprint(),
	)
}

func aggregateResolveCacheKey(number string, category routing.Category, config routing.Config) string {
	return fmt.Sprintf("resolve|%s|%s|%s",
		strings.ToLower(strings.TrimSpace(number)),
		category.Canonical(),
		config.Fingerprint(),
	)
}

// cloneAggregatedMovieInfo isolates the mutable top-level identity from the
// cached value. The detail route rewrites Provider/ID for the reserved MetaX
// response, so sharing the cached pointer directly would corrupt later reads.
func cloneAggregatedMovieInfo(source *AggregatedMovieInfo) *AggregatedMovieInfo {
	if source == nil {
		return nil
	}
	clone := *source
	if source.MovieInfo != nil {
		info := *source.MovieInfo
		clone.MovieInfo = &info
	}
	clone.Sources = append([]MovieSourceRecord(nil), source.Sources...)
	for i := range clone.Sources {
		if clone.Sources[i].Metadata == nil {
			continue
		}
		metadata := *clone.Sources[i].Metadata
		clone.Sources[i].Metadata = &metadata
	}
	return &clone
}
