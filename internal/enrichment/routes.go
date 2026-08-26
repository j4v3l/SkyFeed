package enrichment

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const MaxRouteBatchSize = 50

var ErrRouteCircuitOpen = errors.New("route enrichment circuit is open")

type RouteRequest struct {
	Callsign  string
	Latitude  float64
	Longitude float64
}

type RouteUpstream interface {
	LookupRoutes(context.Context, []RouteRequest) (map[string]domain.Route, error)
	LookupAirport(context.Context, string) (domain.Airport, error)
}

type RouteConfig struct {
	QueueSize          int
	AirportQueueSize   int
	BatchSize          int
	BatchWait          time.Duration
	PrefetchLimit      int
	RequestBudget      time.Duration
	RouteTTL           time.Duration
	AirportTTL         time.Duration
	NotFoundTTL        time.Duration
	ErrorTTL           time.Duration
	StaleTTL           time.Duration
	MaxRouteEntries    int
	MaxAirportEntries  int
	RequestsPerSecond  float64
	Burst              float64
	MaxConcurrent      int
	MaxAttempts        int
	MaxRetryDelay      time.Duration
	CircuitFailures    int
	CircuitOpenTimeout time.Duration
}

func DefaultRouteConfig() RouteConfig {
	return RouteConfig{
		QueueSize:          256,
		AirportQueueSize:   64,
		BatchSize:          MaxRouteBatchSize,
		BatchWait:          25 * time.Millisecond,
		PrefetchLimit:      MaxRouteBatchSize,
		RequestBudget:      4 * time.Second,
		RouteTTL:           8 * time.Hour,
		AirportTTL:         7 * 24 * time.Hour,
		NotFoundTTL:        time.Hour,
		ErrorTTL:           30 * time.Second,
		StaleTTL:           24 * time.Hour,
		MaxRouteEntries:    10_000,
		MaxAirportEntries:  2_000,
		RequestsPerSecond:  2,
		Burst:              1,
		MaxConcurrent:      2,
		MaxAttempts:        2,
		MaxRetryDelay:      2 * time.Second,
		CircuitFailures:    5,
		CircuitOpenTimeout: 30 * time.Second,
	}
}

type RouteServiceStats struct {
	Enqueued, Dropped, Coalesced, Batches, Requests, Hits, Misses, Failures, CircuitRejects uint64
}

type RouteService struct {
	upstream RouteUpstream
	config   RouteConfig

	routes   *ttlLRU[domain.Route]
	airports *ttlLRU[domain.Airport]

	routeQueue   chan RouteRequest
	airportQueue chan string
	pendingMu    sync.Mutex
	pendingRoute map[string]struct{}
	pendingPort  map[string]struct{}

	routeGroup     singleflight.Group
	airportGroup   singleflight.Group
	limiter        *tokenBucket
	outbound       chan struct{}
	circuit        routeCircuit
	now            func() time.Time
	prefetchCursor atomic.Uint64

	enqueued       atomic.Uint64
	dropped        atomic.Uint64
	coalesced      atomic.Uint64
	batches        atomic.Uint64
	requests       atomic.Uint64
	hits           atomic.Uint64
	misses         atomic.Uint64
	failures       atomic.Uint64
	circuitRejects atomic.Uint64
}

func NewRouteService(upstream RouteUpstream, config RouteConfig) *RouteService {
	config = normalizeRouteConfig(config)
	return &RouteService{
		upstream:     upstream,
		config:       config,
		routes:       newTTLLRU[domain.Route](config.MaxRouteEntries),
		airports:     newTTLLRU[domain.Airport](config.MaxAirportEntries),
		routeQueue:   make(chan RouteRequest, config.QueueSize),
		airportQueue: make(chan string, config.AirportQueueSize),
		pendingRoute: make(map[string]struct{}),
		pendingPort:  make(map[string]struct{}),
		limiter:      newTokenBucket(config.RequestsPerSecond, config.Burst),
		outbound:     make(chan struct{}, config.MaxConcurrent),
		circuit:      newRouteCircuit(config.CircuitFailures, config.CircuitOpenTimeout),
		now:          time.Now,
	}
}

func normalizeRouteConfig(config RouteConfig) RouteConfig {
	defaults := DefaultRouteConfig()
	if config.QueueSize < 1 {
		config.QueueSize = defaults.QueueSize
	}
	if config.AirportQueueSize < 1 {
		config.AirportQueueSize = defaults.AirportQueueSize
	}
	if config.BatchSize < 1 || config.BatchSize > MaxRouteBatchSize {
		config.BatchSize = defaults.BatchSize
	}
	if config.BatchWait <= 0 {
		config.BatchWait = defaults.BatchWait
	}
	if config.PrefetchLimit < 1 || config.PrefetchLimit > MaxRouteBatchSize {
		config.PrefetchLimit = defaults.PrefetchLimit
	}
	if config.RequestBudget <= 0 {
		config.RequestBudget = defaults.RequestBudget
	}
	if config.RouteTTL <= 0 {
		config.RouteTTL = defaults.RouteTTL
	}
	if config.AirportTTL <= 0 {
		config.AirportTTL = defaults.AirportTTL
	}
	if config.NotFoundTTL <= 0 {
		config.NotFoundTTL = defaults.NotFoundTTL
	}
	if config.ErrorTTL <= 0 {
		config.ErrorTTL = defaults.ErrorTTL
	}
	if config.StaleTTL <= 0 {
		config.StaleTTL = defaults.StaleTTL
	}
	if config.MaxRouteEntries < 1 {
		config.MaxRouteEntries = defaults.MaxRouteEntries
	}
	if config.MaxAirportEntries < 1 {
		config.MaxAirportEntries = defaults.MaxAirportEntries
	}
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = defaults.RequestsPerSecond
	}
	if config.Burst < 1 {
		config.Burst = defaults.Burst
	}
	if config.MaxConcurrent < 1 {
		config.MaxConcurrent = defaults.MaxConcurrent
	}
	if config.MaxAttempts < 1 || config.MaxAttempts > 3 {
		config.MaxAttempts = defaults.MaxAttempts
	}
	if config.MaxRetryDelay <= 0 {
		config.MaxRetryDelay = defaults.MaxRetryDelay
	}
	if config.CircuitFailures < 1 {
		config.CircuitFailures = defaults.CircuitFailures
	}
	if config.CircuitOpenTimeout <= 0 {
		config.CircuitOpenTimeout = defaults.CircuitOpenTimeout
	}
	return config
}

// Prefetch queues at most PrefetchLimit visible aircraft. Only aircraft with a
// normalized callsign and their own public position are eligible.
func (service *RouteService) Prefetch(aircraft []domain.Aircraft) int {
	if len(aircraft) == 0 {
		return 0
	}
	queued := 0
	start := int(service.prefetchCursor.Load() % uint64(len(aircraft)))
	examined := 0
	for examined < len(aircraft) {
		if queued >= service.config.PrefetchLimit {
			break
		}
		item := aircraft[(start+examined)%len(aircraft)]
		examined++
		if !item.HasPosition {
			continue
		}
		if service.EnqueueRoute(RouteRequest{Callsign: item.Callsign, Latitude: item.Latitude, Longitude: item.Longitude}) == AdmissionEnqueued {
			queued++
		}
	}
	service.prefetchCursor.Store(uint64((start + examined) % len(aircraft)))
	return queued
}

func (service *RouteService) EnqueueRoute(request RouteRequest) AdmissionResult {
	normalized, ok := normalizeRouteRequest(request)
	if !ok {
		return AdmissionInvalid
	}
	if _, found, stale, _ := service.routes.get(normalized.Callsign); found && !stale {
		service.hits.Add(1)
		return AdmissionCached
	}

	service.pendingMu.Lock()
	if _, exists := service.pendingRoute[normalized.Callsign]; exists {
		service.pendingMu.Unlock()
		service.coalesced.Add(1)
		return AdmissionCoalesced
	}
	service.pendingRoute[normalized.Callsign] = struct{}{}
	select {
	case service.routeQueue <- normalized:
		service.pendingMu.Unlock()
		service.enqueued.Add(1)
		return AdmissionEnqueued
	default:
		select {
		case oldest := <-service.routeQueue:
			delete(service.pendingRoute, oldest.Callsign)
			service.dropped.Add(1)
		default:
		}
		select {
		case service.routeQueue <- normalized:
			service.pendingMu.Unlock()
			service.enqueued.Add(1)
			return AdmissionEnqueued
		default:
			delete(service.pendingRoute, normalized.Callsign)
			service.pendingMu.Unlock()
			service.dropped.Add(1)
			return AdmissionDropped
		}
	}
}

func (service *RouteService) EnqueueAirport(code string) AdmissionResult {
	code, ok := NormalizeAirportCode(code)
	if !ok {
		return AdmissionInvalid
	}
	if _, found, stale, _ := service.airports.get(code); found && !stale {
		service.hits.Add(1)
		return AdmissionCached
	}

	service.pendingMu.Lock()
	if _, exists := service.pendingPort[code]; exists {
		service.pendingMu.Unlock()
		service.coalesced.Add(1)
		return AdmissionCoalesced
	}
	service.pendingPort[code] = struct{}{}
	select {
	case service.airportQueue <- code:
		service.pendingMu.Unlock()
		service.enqueued.Add(1)
		return AdmissionEnqueued
	default:
		select {
		case oldest := <-service.airportQueue:
			delete(service.pendingPort, oldest)
			service.dropped.Add(1)
		default:
		}
		select {
		case service.airportQueue <- code:
			service.pendingMu.Unlock()
			service.enqueued.Add(1)
			return AdmissionEnqueued
		default:
			delete(service.pendingPort, code)
			service.pendingMu.Unlock()
			service.dropped.Add(1)
			return AdmissionDropped
		}
	}
}

func (service *RouteService) CachedRoute(callsign string) (domain.Route, bool, error) {
	callsign, ok := NormalizeCallsign(callsign)
	if !ok {
		return domain.Route{}, false, nil
	}
	value, found, stale, err := service.routes.get(callsign)
	if !found {
		service.misses.Add(1)
		return domain.Route{}, false, nil
	}
	service.hits.Add(1)
	value.Stale = stale
	return value, true, err
}

func (service *RouteService) CachedAirport(code string) (domain.Airport, bool, error) {
	code, ok := NormalizeAirportCode(code)
	if !ok {
		return domain.Airport{}, false, nil
	}
	value, found, stale, err := service.airports.get(code)
	if !found {
		service.misses.Add(1)
		return domain.Airport{}, false, nil
	}
	service.hits.Add(1)
	value.Stale = stale
	return value, true, err
}

// LookupRoute is cache-first. A stale value is returned immediately and queued
// for bounded background refresh; a miss is singleflight-coalesced.
func (service *RouteService) LookupRoute(ctx context.Context, request RouteRequest) (domain.Route, error) {
	request, ok := normalizeRouteRequest(request)
	if !ok {
		return domain.Route{}, errors.New("valid callsign and aircraft position are required")
	}
	if value, found, stale, err := service.routes.get(request.Callsign); found {
		service.hits.Add(1)
		value.Stale = stale
		if stale && err == nil {
			service.EnqueueRoute(request)
		}
		return value, err
	}
	service.misses.Add(1)

	result, err, _ := service.routeGroup.Do(request.Callsign, func() (any, error) {
		if value, found, stale, cachedErr := service.routes.get(request.Callsign); found && !stale {
			return value, cachedErr
		}
		if err := service.fetchRoutes(ctx, []RouteRequest{request}); err != nil {
			return domain.Route{}, err
		}
		value, found, _, cachedErr := service.routes.get(request.Callsign)
		if !found {
			return domain.Route{}, ErrNotFound
		}
		return value, cachedErr
	})
	if result == nil {
		return domain.Route{}, err
	}
	return result.(domain.Route), err
}

// LookupAirport is cache-first. Airport refreshes need only the public airport
// code, so stale values can be queued without any location data.
func (service *RouteService) LookupAirport(ctx context.Context, code string) (domain.Airport, error) {
	code, ok := NormalizeAirportCode(code)
	if !ok {
		return domain.Airport{}, errors.New("valid ICAO airport code is required")
	}
	if value, found, stale, err := service.airports.get(code); found {
		service.hits.Add(1)
		value.Stale = stale
		if stale && err == nil {
			service.EnqueueAirport(code)
		}
		return value, err
	}
	service.misses.Add(1)

	result, err, _ := service.airportGroup.Do(code, func() (any, error) {
		if value, found, stale, cachedErr := service.airports.get(code); found && !stale {
			return value, cachedErr
		}
		if err := service.fetchAirport(ctx, code); err != nil {
			return domain.Airport{}, err
		}
		value, found, _, cachedErr := service.airports.get(code)
		if !found {
			return domain.Airport{}, ErrNotFound
		}
		return value, cachedErr
	})
	if result == nil {
		return domain.Airport{}, err
	}
	return result.(domain.Airport), err
}

func (service *RouteService) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-service.routeQueue:
			batch := service.collectRouteBatch(ctx, request)
			_ = service.fetchRoutes(ctx, batch)
			service.clearPendingRoutes(batch)
		case code := <-service.airportQueue:
			_ = service.fetchAirport(ctx, code)
			service.clearPendingAirport(code)
		}
	}
}

func (service *RouteService) Stats() RouteServiceStats {
	return RouteServiceStats{
		Enqueued:       service.enqueued.Load(),
		Dropped:        service.dropped.Load(),
		Coalesced:      service.coalesced.Load(),
		Batches:        service.batches.Load(),
		Requests:       service.requests.Load(),
		Hits:           service.hits.Load(),
		Misses:         service.misses.Load(),
		Failures:       service.failures.Load(),
		CircuitRejects: service.circuitRejects.Load(),
	}
}

func (service *RouteService) RouteCacheLen() int   { return service.routes.len() }
func (service *RouteService) AirportCacheLen() int { return service.airports.len() }

func (service *RouteService) collectRouteBatch(ctx context.Context, first RouteRequest) []RouteRequest {
	batch := make([]RouteRequest, 0, service.config.BatchSize)
	batch = append(batch, first)
	if len(batch) == service.config.BatchSize {
		return batch
	}
	timer := time.NewTimer(service.config.BatchWait)
	defer timer.Stop()
	for len(batch) < service.config.BatchSize {
		select {
		case <-ctx.Done():
			return batch
		case request := <-service.routeQueue:
			batch = append(batch, request)
		case <-timer.C:
			return batch
		}
	}
	return batch
}

func (service *RouteService) fetchRoutes(ctx context.Context, requests []RouteRequest) error {
	missing := make([]RouteRequest, 0, len(requests))
	for _, request := range requests {
		if _, found, stale, _ := service.routes.get(request.Callsign); !found || stale {
			missing = append(missing, request)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !service.circuit.allow(service.now()) {
		service.circuitRejects.Add(1)
		return ErrRouteCircuitOpen
	}

	budgetContext, cancel := context.WithTimeout(ctx, service.config.RequestBudget)
	defer cancel()
	routes, err := service.requestRoutes(budgetContext, missing)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		service.recordFailure(err)
		for _, request := range missing {
			service.routes.set(request.Callsign, domain.Route{}, err, service.config.ErrorTTL, 0)
		}
		return err
	}

	service.circuit.success()
	service.batches.Add(1)
	now := service.now().UTC()
	for _, request := range missing {
		route, found := routes[request.Callsign]
		if !found {
			service.routes.set(request.Callsign, domain.Route{}, ErrNotFound, service.config.NotFoundTTL, 0)
			continue
		}
		route.Callsign = request.Callsign
		route.FetchedAt = now
		route.ExpiresAt = now.Add(service.config.RouteTTL)
		route.Stale = false
		service.routes.set(request.Callsign, route, nil, service.config.RouteTTL, service.config.StaleTTL)
		service.cacheRouteAirports(route, now)
	}
	return nil
}

func (service *RouteService) fetchAirport(ctx context.Context, code string) error {
	if _, found, stale, _ := service.airports.get(code); found && !stale {
		return nil
	}
	if !service.circuit.allow(service.now()) {
		service.circuitRejects.Add(1)
		return ErrRouteCircuitOpen
	}

	budgetContext, cancel := context.WithTimeout(ctx, service.config.RequestBudget)
	defer cancel()
	airport, err := service.requestAirport(budgetContext, code)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			service.circuit.success()
			service.airports.set(code, domain.Airport{}, ErrNotFound, service.config.NotFoundTTL, 0)
			return ErrNotFound
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		service.recordFailure(err)
		service.airports.set(code, domain.Airport{}, err, service.config.ErrorTTL, 0)
		return err
	}

	service.circuit.success()
	now := service.now().UTC()
	airport.ICAO = code
	airport.FetchedAt = now
	airport.ExpiresAt = now.Add(service.config.AirportTTL)
	airport.Stale = false
	service.airports.set(code, airport, nil, service.config.AirportTTL, service.config.StaleTTL)
	return nil
}

func (service *RouteService) requestRoutes(ctx context.Context, requests []RouteRequest) (map[string]domain.Route, error) {
	var routes map[string]domain.Route
	var err error
	for attempt := 0; attempt < service.config.MaxAttempts; attempt++ {
		if err = service.waitForRequest(ctx); err != nil {
			return nil, err
		}
		service.requests.Add(1)
		routes, err = service.upstream.LookupRoutes(ctx, requests)
		service.releaseRequest()
		if err == nil {
			return routes, nil
		}
		if !service.retry(ctx, err, attempt) {
			break
		}
	}
	return nil, err
}

func (service *RouteService) requestAirport(ctx context.Context, code string) (domain.Airport, error) {
	var airport domain.Airport
	var err error
	for attempt := 0; attempt < service.config.MaxAttempts; attempt++ {
		if err = service.waitForRequest(ctx); err != nil {
			return domain.Airport{}, err
		}
		service.requests.Add(1)
		airport, err = service.upstream.LookupAirport(ctx, code)
		service.releaseRequest()
		if err == nil || errors.Is(err, ErrNotFound) {
			return airport, err
		}
		if !service.retry(ctx, err, attempt) {
			break
		}
	}
	return domain.Airport{}, err
}

func (service *RouteService) waitForRequest(ctx context.Context) error {
	if err := service.limiter.wait(ctx); err != nil {
		return err
	}
	select {
	case service.outbound <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *RouteService) releaseRequest() { <-service.outbound }

func (service *RouteService) retry(ctx context.Context, err error, attempt int) bool {
	if attempt+1 >= service.config.MaxAttempts {
		return false
	}
	var retryable RetryableError
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		return false
	}
	delay := retryable.RetryDelay()
	if delay <= 0 {
		delay = time.Duration(100*(1<<attempt)) * time.Millisecond
	}
	delay = min(delay, service.config.MaxRetryDelay)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (service *RouteService) recordFailure(err error) {
	service.failures.Add(1)
	if !errors.Is(err, context.Canceled) {
		service.circuit.failure(service.now())
	}
}

func (service *RouteService) cacheRouteAirports(route domain.Route, now time.Time) {
	airports := route.Airports
	if len(airports) == 0 {
		airports = append(airports, route.Origin)
		if route.Midpoint != nil {
			airports = append(airports, *route.Midpoint)
		}
		airports = append(airports, route.Destination)
	}
	for _, airport := range airports {
		code, ok := NormalizeAirportCode(airport.ICAO)
		if !ok {
			continue
		}
		airport.ICAO = code
		airport.FetchedAt = now
		airport.ExpiresAt = now.Add(service.config.AirportTTL)
		airport.Stale = false
		service.airports.set(code, airport, nil, service.config.AirportTTL, service.config.StaleTTL)
	}
}

func (service *RouteService) clearPendingRoutes(requests []RouteRequest) {
	service.pendingMu.Lock()
	for _, request := range requests {
		delete(service.pendingRoute, request.Callsign)
	}
	service.pendingMu.Unlock()
}

func (service *RouteService) clearPendingAirport(code string) {
	service.pendingMu.Lock()
	delete(service.pendingPort, code)
	service.pendingMu.Unlock()
}

func NormalizeCallsign(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) < 2 || len(value) > 12 {
		return "", false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return "", false
		}
	}
	return value, true
}

func NormalizeAirportCode(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 4 {
		return "", false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return "", false
		}
	}
	return value, true
}

func normalizeRouteRequest(request RouteRequest) (RouteRequest, bool) {
	callsign, ok := NormalizeCallsign(request.Callsign)
	if !ok || math.IsNaN(request.Latitude) || math.IsInf(request.Latitude, 0) ||
		math.IsNaN(request.Longitude) || math.IsInf(request.Longitude, 0) ||
		request.Latitude < -90 || request.Latitude > 90 ||
		request.Longitude < -180 || request.Longitude > 180 {
		return RouteRequest{}, false
	}
	request.Callsign = callsign
	return request, true
}

type routeCircuit struct {
	mu        sync.Mutex
	threshold int
	openFor   time.Duration
	failures  int
	openUntil time.Time
	probing   bool
}

func newRouteCircuit(threshold int, openFor time.Duration) routeCircuit {
	return routeCircuit{threshold: threshold, openFor: openFor}
}

func (breaker *routeCircuit) allow(now time.Time) bool {
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

func (breaker *routeCircuit) success() {
	breaker.mu.Lock()
	breaker.failures, breaker.openUntil, breaker.probing = 0, time.Time{}, false
	breaker.mu.Unlock()
}

func (breaker *routeCircuit) failure(now time.Time) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	breaker.probing = false
	breaker.failures++
	if breaker.failures >= breaker.threshold {
		breaker.openUntil = now.Add(breaker.openFor)
	}
}
