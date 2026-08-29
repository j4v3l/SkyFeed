package enrichment

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	defaultDiscoveryEntries = 50_000
	defaultDiscoveryTTL     = 30 * time.Minute
)

type discoveryEntry struct {
	icao     string
	callsign string
	seenAt   time.Time
	element  *list.Element
}

// DiscoveryTracker admits enrichment only for newly observed aircraft or a
// meaningful callsign change. Its LRU bound prevents unique-ICAO churn from
// retaining unbounded state across community feeders.
type DiscoveryTracker struct {
	mu      sync.Mutex
	entries map[string]*discoveryEntry
	order   *list.List
	limit   int
	ttl     time.Duration
}

func NewDiscoveryTracker() *DiscoveryTracker {
	return &DiscoveryTracker{
		entries: make(map[string]*discoveryEntry),
		order:   list.New(),
		limit:   defaultDiscoveryEntries,
		ttl:     defaultDiscoveryTTL,
	}
}

func (tracker *DiscoveryTracker) Observe(aircraft []domain.Aircraft, observedAt time.Time, admit func(icao, callsign string)) {
	if tracker == nil || admit == nil {
		return
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for _, item := range aircraft {
		icao := strings.ToUpper(strings.TrimSpace(item.ICAO))
		if icao == "" {
			continue
		}
		callsign := strings.ToUpper(strings.TrimSpace(item.Callsign))
		entry := tracker.entries[icao]
		if entry == nil {
			entry = &discoveryEntry{icao: icao, callsign: callsign, seenAt: observedAt}
			entry.element = tracker.order.PushFront(entry)
			tracker.entries[icao] = entry
			admit(icao, callsign)
		} else {
			tracker.order.MoveToFront(entry.element)
			entry.seenAt = observedAt
			if entry.callsign != callsign {
				entry.callsign = callsign
				admit(icao, callsign)
			}
		}
	}
	cutoff := observedAt.Add(-tracker.ttl)
	for tracker.order.Len() > tracker.limit || tracker.oldestBefore(cutoff) {
		oldest := tracker.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*discoveryEntry)
		delete(tracker.entries, entry.icao)
		tracker.order.Remove(oldest)
	}
}

func (tracker *DiscoveryTracker) oldestBefore(cutoff time.Time) bool {
	oldest := tracker.order.Back()
	return oldest != nil && oldest.Value.(*discoveryEntry).seenAt.Before(cutoff)
}

func (tracker *DiscoveryTracker) Len() int {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return len(tracker.entries)
}
