package enrichment

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var ErrPending = errors.New("enrichment lookup queued")

type Config struct {
	Workers           int
	QueueSize         int
	RouteEnabled      bool
	RequestBudget     time.Duration
	AircraftTTL       time.Duration
	RouteTTL          time.Duration
	NotFoundTTL       time.Duration
	ErrorTTL          time.Duration
	StaleTTL          time.Duration
	RequestsPerSecond float64
	Burst             float64
}

func DefaultConfig() Config {
	return Config{Workers: 2, QueueSize: 256, RequestBudget: 4 * time.Second, AircraftTTL: 14 * 24 * time.Hour, RouteTTL: 8 * time.Hour, NotFoundTTL: 3 * time.Hour, ErrorTTL: 30 * time.Second, StaleTTL: 24 * time.Hour, RequestsPerSecond: 5, Burst: 2}
}

type Request struct{ ICAO, Callsign, key string }

type ServiceStats struct {
	Enqueued, Dropped, Coalesced, Hits, Misses, Requests, Failures, CircuitRejects uint64
}

type Service struct {
	upstream  Enricher
	config    Config
	cache     *Cache
	queue     chan Request
	pendingMu sync.Mutex
	pending   map[string]struct{}
	group     singleflight.Group
	limiter   *tokenBucket
	circuit   circuitBreaker
	now       func() time.Time
	observer  func(domain.Enrichment, error)

	enqueued       atomic.Uint64
	dropped        atomic.Uint64
	coalesced      atomic.Uint64
	hits           atomic.Uint64
	misses         atomic.Uint64
	requests       atomic.Uint64
	failures       atomic.Uint64
	circuitRejects atomic.Uint64
}

func NewService(upstream Enricher, config Config) *Service {
	if config.Workers < 1 {
		config.Workers = 2
	}
	if config.QueueSize < 1 {
		config.QueueSize = 256
	}
	if config.RequestBudget <= 0 {
		config.RequestBudget = 4 * time.Second
	}
	return &Service{upstream: upstream, config: config, cache: NewCache(10_000), queue: make(chan Request, config.QueueSize), pending: make(map[string]struct{}), limiter: newTokenBucket(config.RequestsPerSecond, config.Burst), now: time.Now}
}

func (service *Service) Enqueue(icao, callsign string) bool {
	icao, callsign, key := NormalizeKey(icao, callsign)
	if icao == "" {
		return false
	}
	if _, ok, stale, _ := service.cache.Get(key); ok && !stale {
		service.hits.Add(1)
		return true
	}
	service.pendingMu.Lock()
	if _, exists := service.pending[key]; exists {
		service.pendingMu.Unlock()
		service.coalesced.Add(1)
		return true
	}
	service.pending[key] = struct{}{}
	request := Request{ICAO: icao, Callsign: callsign, key: key}
	select {
	case service.queue <- request:
		service.pendingMu.Unlock()
		service.enqueued.Add(1)
		return true
	default:
		select {
		case oldest := <-service.queue:
			delete(service.pending, oldest.key)
			service.dropped.Add(1)
		default:
		}
		select {
		case service.queue <- request:
			service.pendingMu.Unlock()
			service.enqueued.Add(1)
			return true
		default:
			delete(service.pending, key)
			service.pendingMu.Unlock()
			service.dropped.Add(1)
			return false
		}
	}
}

func (service *Service) Cached(icao, callsign string) (domain.Enrichment, bool, error) {
	_, _, key := NormalizeKey(icao, callsign)
	value, ok, stale, err := service.cache.Get(key)
	if !ok {
		service.misses.Add(1)
		return domain.Enrichment{}, false, nil
	}
	service.hits.Add(1)
	if stale {
		service.Enqueue(icao, callsign)
	}
	return value, true, err
}

func (service *Service) Lookup(ctx context.Context, icao, callsign string) (domain.Enrichment, error) {
	icao, callsign, key := NormalizeKey(icao, callsign)
	if value, ok, stale, err := service.cache.Get(key); ok && !stale {
		service.hits.Add(1)
		return value, err
	}
	service.misses.Add(1)
	return service.lookup(ctx, Request{ICAO: icao, Callsign: callsign, key: key})
}

func (service *Service) Run(ctx context.Context) error {
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(service.config.Workers)
	for range service.config.Workers {
		group.Go(func() error { return service.worker(groupContext) })
	}
	return group.Wait()
}

func (service *Service) Stats() ServiceStats {
	return ServiceStats{Enqueued: service.enqueued.Load(), Dropped: service.dropped.Load(), Coalesced: service.coalesced.Load(), Hits: service.hits.Load(), Misses: service.misses.Load(), Requests: service.requests.Load(), Failures: service.failures.Load(), CircuitRejects: service.circuitRejects.Load()}
}

func (service *Service) CacheLen() int { return service.cache.Len() }

func (service *Service) SetObserver(observer func(domain.Enrichment, error)) {
	service.observer = observer
}

func (service *Service) worker(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-service.queue:
			service.pendingMu.Lock()
			delete(service.pending, request.key)
			service.pendingMu.Unlock()
			_, _ = service.lookup(ctx, request)
		}
	}
}

func (service *Service) lookup(ctx context.Context, request Request) (domain.Enrichment, error) {
	result, err, _ := service.group.Do(request.key, func() (any, error) {
		if value, ok, stale, cachedErr := service.cache.Get(request.key); ok && !stale {
			return value, cachedErr
		}
		if !service.circuit.allow(service.now()) {
			service.circuitRejects.Add(1)
			return domain.Enrichment{}, errors.New("ADSBDB circuit is open")
		}
		budgetContext, cancel := context.WithTimeout(ctx, service.config.RequestBudget)
		defer cancel()
		var value domain.Enrichment
		var lookupErr error
		for attempt := 0; attempt < 3; attempt++ {
			if err := service.limiter.wait(budgetContext); err != nil {
				lookupErr = err
				break
			}
			service.requests.Add(1)
			value, lookupErr = service.upstream.Lookup(budgetContext, request.ICAO, request.Callsign)
			if lookupErr == nil || errors.Is(lookupErr, ErrNotFound) {
				break
			}
			var requestErr RetryableError
			if !errors.As(lookupErr, &requestErr) || !requestErr.Retryable() {
				break
			}
			delay := requestErr.RetryDelay()
			if delay <= 0 {
				delay = time.Duration(100*(1<<attempt))*time.Millisecond + time.Duration(rand.Int64N(75))*time.Millisecond
			}
			timer := time.NewTimer(delay)
			select {
			case <-budgetContext.Done():
				timer.Stop()
				lookupErr = budgetContext.Err()
				attempt = 3
			case <-timer.C:
			}
		}
		if lookupErr == nil {
			service.circuit.success()
			if !service.config.RouteEnabled {
				value.Route = nil
			}
			ttl := service.config.AircraftTTL
			if value.Route != nil && service.config.RouteTTL < ttl {
				ttl = service.config.RouteTTL
			}
			service.cache.Set(request.key, value, nil, ttl, service.config.StaleTTL)
			service.notify(value, nil)
			return value, nil
		}
		if errors.Is(lookupErr, ErrNotFound) {
			service.circuit.success()
			notFound := domain.Enrichment{ICAO: request.ICAO, Callsign: request.Callsign, Found: false}
			service.cache.Set(request.key, notFound, ErrNotFound, service.config.NotFoundTTL, 0)
			service.notify(notFound, ErrNotFound)
			return domain.Enrichment{}, ErrNotFound
		}
		service.failures.Add(1)
		service.circuit.failure(service.now())
		service.cache.Set(request.key, domain.Enrichment{}, lookupErr, service.config.ErrorTTL, 0)
		return domain.Enrichment{}, lookupErr
	})
	if result == nil {
		return domain.Enrichment{}, err
	}
	return result.(domain.Enrichment), err
}

func (service *Service) notify(value domain.Enrichment, err error) {
	if service.observer != nil {
		service.observer(value, err)
	}
}

type tokenBucket struct {
	mu                  sync.Mutex
	rate, burst, tokens float64
	last                time.Time
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	if rate <= 0 {
		rate = 5
	}
	if burst < 1 {
		burst = 1
	}
	return &tokenBucket{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}

func (bucket *tokenBucket) wait(ctx context.Context) error {
	for {
		bucket.mu.Lock()
		now := time.Now()
		bucket.tokens = min(bucket.burst, bucket.tokens+now.Sub(bucket.last).Seconds()*bucket.rate)
		bucket.last = now
		if bucket.tokens >= 1 {
			bucket.tokens--
			bucket.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - bucket.tokens) / bucket.rate * float64(time.Second))
		bucket.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type circuitBreaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	probing   bool
}

func (breaker *circuitBreaker) allow(now time.Time) bool {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	if breaker.openUntil.IsZero() {
		return true
	}
	if now.Before(breaker.openUntil) {
		return false
	}
	if breaker.probing {
		return false
	}
	breaker.probing = true
	return true
}

func (breaker *circuitBreaker) success() {
	breaker.mu.Lock()
	breaker.failures, breaker.openUntil, breaker.probing = 0, time.Time{}, false
	breaker.mu.Unlock()
}

func (breaker *circuitBreaker) failure(now time.Time) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	breaker.probing = false
	breaker.failures++
	if breaker.failures >= 5 {
		breaker.openUntil = now.Add(30 * time.Second)
	}
}

var _ Enricher = (*Service)(nil)
