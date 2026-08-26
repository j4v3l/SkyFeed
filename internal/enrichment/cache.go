package enrichment

import (
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type Cache struct {
	entries *ttlLRU[domain.Enrichment]
}

func NewCache(maxEntries int) *Cache {
	return &Cache{entries: newTTLLRU[domain.Enrichment](maxEntries)}
}

func (cache *Cache) Get(key string) (domain.Enrichment, bool, bool, error) {
	value, ok, stale, err := cache.entries.get(key)
	value.Stale = stale
	return value, ok, stale, err
}

func (cache *Cache) Set(key string, value domain.Enrichment, err error, ttl, staleTTL time.Duration) {
	now := cache.entries.now()
	value.ExpiresAt = now.Add(ttl)
	cache.entries.set(key, value, err, ttl, staleTTL)
}

func (cache *Cache) Len() int {
	return cache.entries.len()
}
