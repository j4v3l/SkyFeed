package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type routeUpstreamStub struct {
	mu           sync.Mutex
	routeCalls   [][]RouteRequest
	airportCalls []string
	lookupRoutes func([]RouteRequest) (map[string]domain.Route, error)
	lookupPort   func(string) (domain.Airport, error)
}

func (stub *routeUpstreamStub) LookupRoutes(_ context.Context, requests []RouteRequest) (map[string]domain.Route, error) {
	copyRequests := append([]RouteRequest(nil), requests...)
	stub.mu.Lock()
	stub.routeCalls = append(stub.routeCalls, copyRequests)
	lookup := stub.lookupRoutes
	stub.mu.Unlock()
	if lookup != nil {
		return lookup(copyRequests)
	}
	routes := make(map[string]domain.Route, len(copyRequests))
	for _, request := range copyRequests {
		routes[request.Callsign] = testRoute(request.Callsign)
	}
	return routes, nil
}

func (stub *routeUpstreamStub) LookupAirport(_ context.Context, code string) (domain.Airport, error) {
	stub.mu.Lock()
	stub.airportCalls = append(stub.airportCalls, code)
	lookup := stub.lookupPort
	stub.mu.Unlock()
	if lookup != nil {
		return lookup(code)
	}
	return domain.Airport{ICAO: code, Name: "Synthetic Airport"}, nil
}

func (stub *routeUpstreamStub) counts() (routes, airports int) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return len(stub.routeCalls), len(stub.airportCalls)
}

func TestRouteServicePrefetchBatchesFiftyVisibleAircraft(t *testing.T) {
	stub := &routeUpstreamStub{}
	config := fastRouteConfig()
	config.BatchSize = MaxRouteBatchSize
	config.PrefetchLimit = MaxRouteBatchSize
	service := NewRouteService(stub, config)

	aircraft := make([]domain.Aircraft, 65)
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{
			Callsign:    fmt.Sprintf("SF%04d", index),
			Latitude:    float64(index%80) + 0.25,
			Longitude:   -float64(index%170) - 0.5,
			HasPosition: true,
		}
	}
	aircraft[0].HasPosition = false
	if queued := service.Prefetch(aircraft); queued != MaxRouteBatchSize {
		t.Fatalf("queued=%d", queued)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	waitFor(t, func() bool { return service.RouteCacheLen() == MaxRouteBatchSize })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.routeCalls) != 1 || len(stub.routeCalls[0]) != MaxRouteBatchSize {
		t.Fatalf("route calls=%d batch=%d", len(stub.routeCalls), len(stub.routeCalls[0]))
	}
	if stub.routeCalls[0][0].Callsign != "SF0001" || stub.routeCalls[0][0].Latitude != aircraft[1].Latitude {
		t.Fatalf("first request=%+v", stub.routeCalls[0][0])
	}
}

func TestRouteServiceQueueCoalescesNormalizedCallsigns(t *testing.T) {
	stub := &routeUpstreamStub{}
	service := NewRouteService(stub, fastRouteConfig())
	for index := range 20 {
		callsign := " sky123 "
		if index%2 == 0 {
			callsign = "SKY123"
		}
		if !service.EnqueueRoute(RouteRequest{Callsign: callsign, Latitude: 1, Longitude: 2}) {
			t.Fatal("enqueue failed")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	waitFor(t, func() bool { return service.RouteCacheLen() == 1 })
	cancel()
	_ = <-done
	routes, _ := stub.counts()
	if routes != 1 || service.Stats().Coalesced != 19 {
		t.Fatalf("calls=%d stats=%+v", routes, service.Stats())
	}
}

func TestRouteServiceSingleflightAndAirportSeeding(t *testing.T) {
	stub := &routeUpstreamStub{}
	stub.lookupRoutes = func(requests []RouteRequest) (map[string]domain.Route, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]domain.Route{requests[0].Callsign: testRoute(requests[0].Callsign)}, nil
	}
	service := NewRouteService(stub, fastRouteConfig())
	request := RouteRequest{Callsign: "SKY456", Latitude: 3, Longitude: 4}

	var wait sync.WaitGroup
	errorsFound := make(chan error, 20)
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			route, err := service.LookupRoute(context.Background(), request)
			if err != nil {
				errorsFound <- err
				return
			}
			if route.Callsign != "SKY456" {
				errorsFound <- fmt.Errorf("route=%+v", route)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	routes, airports := stub.counts()
	if routes != 1 || airports != 0 {
		t.Fatalf("route calls=%d airport calls=%d", routes, airports)
	}
	if airport, ok, err := service.CachedAirport("KAAA"); err != nil || !ok || airport.Name != "Origin" {
		t.Fatalf("seeded airport=%+v ok=%t err=%v", airport, ok, err)
	}
}

func TestRouteServiceNegativeStaleAndLRUCache(t *testing.T) {
	stub := &routeUpstreamStub{}
	stub.lookupRoutes = func(requests []RouteRequest) (map[string]domain.Route, error) {
		if requests[0].Callsign == "NONE1" {
			return map[string]domain.Route{}, nil
		}
		return map[string]domain.Route{requests[0].Callsign: testRoute(requests[0].Callsign)}, nil
	}
	config := fastRouteConfig()
	config.RouteTTL = time.Minute
	config.NotFoundTTL = time.Minute
	config.StaleTTL = time.Minute
	service := NewRouteService(stub, config)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.routes.now = service.now
	service.airports.now = service.now

	request := RouteRequest{Callsign: "NONE1", Latitude: 1, Longitude: 2}
	if _, err := service.LookupRoute(context.Background(), request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("first negative err=%v", err)
	}
	if _, err := service.LookupRoute(context.Background(), request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cached negative err=%v", err)
	}
	routes, _ := stub.counts()
	if routes != 1 {
		t.Fatalf("negative calls=%d", routes)
	}

	good := RouteRequest{Callsign: "GOOD1", Latitude: 1, Longitude: 2}
	if _, err := service.LookupRoute(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	now = now.Add(90 * time.Second)
	route, err := service.LookupRoute(context.Background(), good)
	if err != nil || !route.Stale {
		t.Fatalf("stale route=%+v err=%v", route, err)
	}
	routes, _ = stub.counts()
	if routes != 2 {
		t.Fatalf("stale lookup blocked on provider; calls=%d", routes)
	}

	cache := newTTLLRU[int](2)
	cache.now = service.now
	cache.set("a", 1, nil, time.Minute, time.Minute)
	cache.set("b", 2, nil, time.Minute, time.Minute)
	_, _, _, _ = cache.get("a")
	cache.set("c", 3, nil, time.Minute, time.Minute)
	if _, ok, _, _ := cache.get("b"); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	now = now.Add(3 * time.Minute)
	if _, ok, _, _ := cache.get("a"); ok {
		t.Fatal("entry survived stale TTL")
	}
}

func TestRouteServiceRetriesAndOpensCircuit(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		stub := &routeUpstreamStub{}
		attempts := 0
		stub.lookupRoutes = func(requests []RouteRequest) (map[string]domain.Route, error) {
			attempts++
			if attempts == 1 {
				return nil, routeRetryError{}
			}
			return map[string]domain.Route{requests[0].Callsign: testRoute(requests[0].Callsign)}, nil
		}
		config := fastRouteConfig()
		config.MaxAttempts = 2
		service := NewRouteService(stub, config)
		if _, err := service.LookupRoute(context.Background(), RouteRequest{Callsign: "RETRY1", Latitude: 1, Longitude: 2}); err != nil {
			t.Fatal(err)
		}
		if attempts != 2 {
			t.Fatalf("attempts=%d", attempts)
		}
	})

	t.Run("circuit", func(t *testing.T) {
		stub := &routeUpstreamStub{lookupRoutes: func([]RouteRequest) (map[string]domain.Route, error) {
			return nil, errors.New("upstream unavailable")
		}}
		config := fastRouteConfig()
		config.CircuitFailures = 2
		service := NewRouteService(stub, config)
		for _, callsign := range []string{"FAIL1", "FAIL2"} {
			if _, err := service.LookupRoute(context.Background(), RouteRequest{Callsign: callsign, Latitude: 1, Longitude: 2}); err == nil {
				t.Fatal("expected provider error")
			}
		}
		if _, err := service.LookupRoute(context.Background(), RouteRequest{Callsign: "FAIL3", Latitude: 1, Longitude: 2}); !errors.Is(err, ErrRouteCircuitOpen) {
			t.Fatalf("circuit err=%v", err)
		}
		routes, _ := stub.counts()
		if routes != 2 || service.Stats().CircuitRejects != 1 {
			t.Fatalf("calls=%d stats=%+v", routes, service.Stats())
		}
	})
}

func TestRouteServiceAirportLookupCachesResultsAndMisses(t *testing.T) {
	stub := &routeUpstreamStub{}
	stub.lookupPort = func(code string) (domain.Airport, error) {
		if code == "KZZZ" {
			return domain.Airport{}, ErrNotFound
		}
		return domain.Airport{ICAO: code, Name: "Synthetic Airport", HasPosition: true, Latitude: 10, Longitude: 20}, nil
	}
	service := NewRouteService(stub, fastRouteConfig())
	for range 2 {
		airport, err := service.LookupAirport(context.Background(), " kaaa ")
		if err != nil || airport.ICAO != "KAAA" {
			t.Fatalf("airport=%+v err=%v", airport, err)
		}
	}
	for range 2 {
		if _, err := service.LookupAirport(context.Background(), "KZZZ"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("negative airport err=%v", err)
		}
	}
	_, airports := stub.counts()
	if airports != 2 {
		t.Fatalf("airport calls=%d", airports)
	}
}

type routeRetryError struct{}

func (routeRetryError) Error() string             { return "retry" }
func (routeRetryError) Retryable() bool           { return true }
func (routeRetryError) RetryDelay() time.Duration { return time.Millisecond }

func fastRouteConfig() RouteConfig {
	config := DefaultRouteConfig()
	config.BatchWait = 2 * time.Millisecond
	config.RequestBudget = time.Second
	config.RequestsPerSecond = 100_000
	config.Burst = 100_000
	config.MaxAttempts = 1
	config.MaxRetryDelay = time.Millisecond
	return config
}

func testRoute(callsign string) domain.Route {
	origin := domain.Airport{ICAO: "KAAA", Name: "Origin", HasPosition: true, Latitude: 10, Longitude: 20}
	destination := domain.Airport{ICAO: "KBBB", Name: "Destination", HasPosition: true, Latitude: 30, Longitude: 40}
	return domain.Route{
		Callsign:          callsign,
		Origin:            origin,
		Destination:       destination,
		Airports:          []domain.Airport{origin, destination},
		Plausible:         true,
		PlausibilityKnown: true,
		Attribution:       "synthetic",
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition not met before deadline")
	}
}
