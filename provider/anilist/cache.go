package anilist

import (
	"sync"
	"time"
)

const cacheTTL = 10 * time.Minute

type searchCacheEntry struct {
	expires time.Time
	media   []animeMedia
}

type detailCacheEntry struct {
	expires time.Time
	media   *animeMedia
}

type aliasCacheEntry struct {
	expires time.Time
	aliases []string
}

var (
	searchCache sync.Map // map[string]searchCacheEntry
	detailCache sync.Map // map[string]detailCacheEntry
	aliasCache  sync.Map // map[string]aliasCacheEntry
)

func cachedSearch(key string) ([]animeMedia, bool) {
	value, ok := searchCache.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := value.(searchCacheEntry)
	if !ok || time.Now().After(entry.expires) {
		searchCache.Delete(key)
		return nil, false
	}
	return entry.media, true
}

func storeSearch(key string, media []animeMedia) {
	searchCache.Store(key, searchCacheEntry{expires: time.Now().Add(cacheTTL), media: media})
}

func cachedDetail(key string) (*animeMedia, bool) {
	value, ok := detailCache.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := value.(detailCacheEntry)
	if !ok || time.Now().After(entry.expires) {
		detailCache.Delete(key)
		return nil, false
	}
	return entry.media, true
}

func storeDetail(key string, media *animeMedia) {
	if media == nil {
		return
	}
	detailCache.Store(key, detailCacheEntry{expires: time.Now().Add(cacheTTL), media: media})
}

func cachedAliases(key string) ([]string, bool) {
	value, ok := aliasCache.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := value.(aliasCacheEntry)
	if !ok || time.Now().After(entry.expires) {
		aliasCache.Delete(key)
		return nil, false
	}
	return entry.aliases, true
}

func storeAliases(key string, aliases []string) {
	aliasCache.Store(key, aliasCacheEntry{expires: time.Now().Add(cacheTTL), aliases: aliases})
}
