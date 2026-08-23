package enrichment

import (
	"container/list"
	"sync"
	"time"
)

type ttlLRUEntry[T any] struct {
	key       string
	value     T
	err       error
	expiresAt time.Time
	staleAt   time.Time
}

// ttlLRU is a bounded cache with stale-while-revalidate support. Cache keys are
// supplied by RouteService and are limited to normalized callsigns or airport
// codes; aircraft and receiver positions are never retained here.
type ttlLRU[T any] struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	max     int
	now     func() time.Time
}

func newTTLLRU[T any](maxEntries int) *ttlLRU[T] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &ttlLRU[T]{
		entries: make(map[string]*list.Element, maxEntries),
		order:   list.New(),
		max:     maxEntries,
		now:     time.Now,
	}
}

func (cache *ttlLRU[T]) get(key string) (value T, ok, stale bool, err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	element, exists := cache.entries[key]
	if !exists {
		return value, false, false, nil
	}
	entry := element.Value.(*ttlLRUEntry[T])
	now := cache.now()
	if !now.Before(entry.staleAt) {
		cache.remove(element)
		return value, false, false, nil
	}
	cache.order.MoveToFront(element)
	return entry.value, true, !now.Before(entry.expiresAt), entry.err
}

func (cache *ttlLRU[T]) set(key string, value T, err error, ttl, staleTTL time.Duration) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	now := cache.now()
	entry := &ttlLRUEntry[T]{
		key:       key,
		value:     value,
		err:       err,
		expiresAt: now.Add(ttl),
		staleAt:   now.Add(ttl + staleTTL),
	}
	if element, exists := cache.entries[key]; exists {
		element.Value = entry
		cache.order.MoveToFront(element)
		return
	}
	element := cache.order.PushFront(entry)
	cache.entries[key] = element
	for len(cache.entries) > cache.max {
		cache.remove(cache.order.Back())
	}
}

func (cache *ttlLRU[T]) len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}

func (cache *ttlLRU[T]) remove(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*ttlLRUEntry[T])
	delete(cache.entries, entry.key)
	cache.order.Remove(element)
}
