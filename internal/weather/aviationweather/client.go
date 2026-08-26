package aviationweather

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	DefaultBaseURL   = "https://aviationweather.gov/api/data"
	maxResponseBytes = 1 << 20
	defaultCacheTTL  = 10 * time.Minute
	defaultErrorTTL  = time.Minute
	defaultStaleTTL  = 30 * time.Minute
	defaultCacheMax  = 500
	Attribution      = "Weather by aviationweather.gov"
)

type Observation struct {
	METAR                string
	TAF                  string
	FlightCategory       string
	METARStatus          string
	TAFStatus            string
	FetchedAt            time.Time
	Attribution          string
	Stale                bool
	WindDirectionDegrees int
	WindVariable         bool
	WindSpeedKts         int
	WindGustKts          int
	HasWind              bool
	VisibilitySM         float64
	VisibilityAtLeast    bool
	HasVisibility        bool
	TemperatureC         int
	DewpointC            int
	HasTemperature       bool
	HasDewpoint          bool
	AltimeterInHg        float64
	HasAltimeter         bool
	Clouds               []CloudLayer
	Conditions           []string
}

type CloudLayer struct {
	Cover    string
	BaseFeet int
	HasBase  bool
}

type Client struct {
	base       *url.URL
	httpClient *http.Client
	ttl        time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]*list.Element
	order *list.List
	max   int
	group singleflight.Group
}

type cachedObservation struct {
	code         string
	value        Observation
	err          error
	expires      time.Time
	staleExpires time.Time
}

func NewClient(timeout time.Duration) (*Client, error) {
	base, err := url.Parse(DefaultBaseURL)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 750 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: 1500 * time.Millisecond,
	}
	return &Client{
		base:       base,
		httpClient: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		ttl:        defaultCacheTTL,
		now:        time.Now,
		cache:      make(map[string]*list.Element),
		order:      list.New(),
		max:        defaultCacheMax,
	}, nil
}

func (client *Client) Lookup(ctx context.Context, icao string) (Observation, error) {
	code := strings.ToUpper(strings.TrimSpace(icao))
	if len(code) != 4 {
		return Observation{}, errors.New("airport ICAO must be four characters")
	}
	if value, err, ok := client.cached(code); ok {
		return value, err
	}
	result, err, _ := client.group.Do(code, func() (any, error) {
		if value, cachedErr, ok := client.cached(code); ok {
			return value, cachedErr
		}
		metar, lookupErr := client.fetchRaw(ctx, "metar", code)
		if lookupErr != nil {
			if stale, ok := client.stale(code); ok {
				return stale, nil
			}
			client.store(code, Observation{}, lookupErr, defaultErrorTTL)
			return Observation{}, lookupErr
		}
		taf, tafErr := client.fetchRaw(ctx, "taf", code)
		observation := Observation{METAR: metar.Raw, TAF: taf.Raw, FlightCategory: metar.FlightCategory, FetchedAt: client.now().UTC(), Attribution: Attribution}
		populateMETAR(&observation)
		if observation.METAR == "" {
			observation.METARStatus = "not-found"
		} else {
			observation.METARStatus = "available"
		}
		switch {
		case tafErr != nil:
			observation.TAFStatus = "unavailable"
		case observation.TAF == "":
			observation.TAFStatus = "not-found"
		default:
			observation.TAFStatus = "available"
		}
		client.store(code, observation, nil, client.ttl)
		return observation, nil
	})
	if result == nil {
		return Observation{}, err
	}
	return result.(Observation), err
}

type weatherPayload struct {
	Raw            string
	FlightCategory string
}

type responseDTO struct {
	RawObservation string `json:"rawOb"`
	RawTAF         string `json:"rawTAF"`
	FlightCategory string `json:"fltCat"`
}

func (client *Client) fetchRaw(ctx context.Context, kind, icao string) (weatherPayload, error) {
	endpoint := *client.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/" + kind
	query := endpoint.Query()
	query.Set("ids", icao)
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return weatherPayload{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return weatherPayload{}, sanitizeError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return weatherPayload{}, fmt.Errorf("aviation weather returned HTTP %d", response.StatusCode)
	}
	limited := http.MaxBytesReader(nil, response.Body, maxResponseBytes)
	var payload []responseDTO
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&payload); err != nil {
		return weatherPayload{}, fmt.Errorf("decode aviation weather response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return weatherPayload{}, errors.New("decode aviation weather response: multiple JSON values")
		}
		return weatherPayload{}, fmt.Errorf("decode aviation weather response: %w", err)
	}
	if len(payload) == 0 {
		return weatherPayload{}, nil
	}
	raw := payload[0].RawObservation
	if raw == "" {
		raw = payload[0].RawTAF
	}
	return weatherPayload{Raw: strings.TrimSpace(raw), FlightCategory: strings.ToUpper(strings.TrimSpace(payload[0].FlightCategory))}, nil
}

func (client *Client) cached(code string) (Observation, error, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	element, ok := client.cache[code]
	if !ok {
		return Observation{}, nil, false
	}
	entry := element.Value.(cachedObservation)
	if !client.now().Before(entry.expires) {
		if client.now().Before(entry.staleExpires) {
			return Observation{}, nil, false
		}
		client.order.Remove(element)
		delete(client.cache, code)
		return Observation{}, nil, false
	}
	client.order.MoveToFront(element)
	return entry.value, entry.err, true
}

func (client *Client) stale(code string) (Observation, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	element, ok := client.cache[code]
	if !ok {
		return Observation{}, false
	}
	entry := element.Value.(cachedObservation)
	if entry.err != nil || entry.value.METAR == "" || !client.now().Before(entry.staleExpires) {
		return Observation{}, false
	}
	value := entry.value
	value.Stale = true
	client.order.MoveToFront(element)
	return value, true
}

func (client *Client) store(code string, value Observation, err error, ttl time.Duration) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if element, ok := client.cache[code]; ok {
		element.Value = cachedObservation{code: code, value: value, err: err, expires: client.now().Add(ttl), staleExpires: client.staleExpiry(err, ttl)}
		client.order.MoveToFront(element)
		return
	}
	element := client.order.PushFront(cachedObservation{code: code, value: value, err: err, expires: client.now().Add(ttl), staleExpires: client.staleExpiry(err, ttl)})
	client.cache[code] = element
	for client.order.Len() > client.max {
		oldest := client.order.Back()
		entry := oldest.Value.(cachedObservation)
		delete(client.cache, entry.code)
		client.order.Remove(oldest)
	}
}

func (client *Client) staleExpiry(err error, ttl time.Duration) time.Time {
	if err != nil {
		return client.now().Add(ttl)
	}
	return client.now().Add(ttl + defaultStaleTTL)
}

func (client *Client) CacheLen() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return len(client.cache)
}

func sanitizeError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("aviation weather request failed: %v", urlErr.Err)
	}
	return err
}
