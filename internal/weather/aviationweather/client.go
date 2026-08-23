package aviationweather

import (
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
)

const (
	DefaultBaseURL   = "https://aviationweather.gov/api/data"
	maxResponseBytes = 1 << 20
	defaultCacheTTL  = 10 * time.Minute
	Attribution      = "Weather by aviationweather.gov"
)

type Observation struct {
	METAR       string
	TAF         string
	FetchedAt   time.Time
	Attribution string
}

type Client struct {
	base       *url.URL
	httpClient *http.Client
	ttl        time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]cachedObservation
}

type cachedObservation struct {
	value   Observation
	expires time.Time
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
		httpClient: &http.Client{Transport: transport, Timeout: timeout},
		ttl:        defaultCacheTTL,
		now:        time.Now,
		cache:      make(map[string]cachedObservation),
	}, nil
}

func (client *Client) Lookup(ctx context.Context, icao string) (Observation, error) {
	code := strings.ToUpper(strings.TrimSpace(icao))
	if len(code) != 4 {
		return Observation{}, errors.New("airport ICAO must be four characters")
	}
	client.mu.Lock()
	if entry, ok := client.cache[code]; ok && client.now().Before(entry.expires) {
		value := entry.value
		client.mu.Unlock()
		return value, nil
	}
	client.mu.Unlock()

	metar, err := client.fetchRaw(ctx, "metar", code)
	if err != nil {
		return Observation{}, err
	}
	taf, _ := client.fetchRaw(ctx, "taf", code)
	observation := Observation{METAR: metar, TAF: taf, FetchedAt: client.now().UTC(), Attribution: Attribution}
	client.mu.Lock()
	client.cache[code] = cachedObservation{value: observation, expires: client.now().Add(client.ttl)}
	client.mu.Unlock()
	return observation, nil
}

func (client *Client) fetchRaw(ctx context.Context, kind, icao string) (string, error) {
	endpoint := *client.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/" + kind
	query := endpoint.Query()
	query.Set("ids", icao)
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", sanitizeError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return "", fmt.Errorf("aviation weather returned HTTP %d", response.StatusCode)
	}
	limited := http.MaxBytesReader(nil, response.Body, maxResponseBytes)
	var payload []map[string]any
	if err := json.NewDecoder(limited).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode aviation weather response: %w", err)
	}
	if len(payload) == 0 {
		return "", nil
	}
	raw, _ := payload[0]["rawOb"].(string)
	if raw == "" {
		raw, _ = payload[0]["rawTAF"].(string)
	}
	return strings.TrimSpace(raw), nil
}

func sanitizeError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("aviation weather request failed: %v", urlErr.Err)
	}
	return err
}
