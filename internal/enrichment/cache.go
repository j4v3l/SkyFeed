package enrichment

import (
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type cacheEntry struct {
	value     domain.Enrichment
	err       error
	expiresAt time.Time
	staleAt   time.Time
}

type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	now     func() time.Time
	max     int
}

func NewCache(maxEntries int) *Cache {
	return &Cache{entries: make(map[string]cacheEntry), now: time.Now, max: maxEntries}
}

func (cache *Cache) Get(key string) (domain.Enrichment, bool, bool, error) {
	cache.mu.RLock()
	entry, ok := cache.entries[key]
	cache.mu.RUnlock()
	if !ok || !cache.now().Before(entry.staleAt) {
		return domain.Enrichment{}, false, false, nil
	}
	value := entry.value
	stale := !cache.now().Before(entry.expiresAt)
	value.Stale = stale
	return value, true, stale, entry.err
}

func (cache *Cache) Set(key string, value domain.Enrichment, err error, ttl, staleTTL time.Duration) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) >= cache.max {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range cache.entries {
			if oldestKey == "" || entry.staleAt.Before(oldest) {
				oldestKey, oldest = candidate, entry.staleAt
			}
		}
		delete(cache.entries, oldestKey)
	}
	now := cache.now()
	value.ExpiresAt = now.Add(ttl)
	cache.entries[key] = cacheEntry{value: value, err: err, expiresAt: now.Add(ttl), staleAt: now.Add(ttl + staleTTL)}
}

func (cache *Cache) Len() int {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return len(cache.entries)
}
