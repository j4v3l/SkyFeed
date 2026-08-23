package readsb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

const (
	aircraftPath     = "aircraft.json"
	receiverPath     = "receiver.json"
	statsPath        = "stats.json"
	maxAircraftBytes = 16 << 20
	maxReceiverBytes = 256 << 10
	maxStatsBytes    = 4 << 20
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	now        func() time.Time
	observer   func(Observation)
}

type Observation struct {
	Provider   domain.ProviderID
	Capability domain.Capability
	Duration   time.Duration
	Bytes      int
	Success    bool
	At         time.Time
}

func NewClient(baseURL *url.URL, timeout time.Duration) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   750 * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
		now: time.Now,
	}
}

func (client *Client) SetObserver(observer func(Observation)) { client.observer = observer }

func (*Client) ProviderID() domain.ProviderID { return domain.ProviderReadsb }

func (*Client) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(
		domain.CapabilityAircraft,
		domain.CapabilityReceiver,
		domain.CapabilityStatistics,
	)
}

func (client *Client) FetchAircraft(ctx context.Context) (source.Frame[domain.AircraftBatch], error) {
	var response aircraftResponse
	fetchedAt, err := fetchJSON(ctx, client, aircraftPath, maxAircraftBytes, &response, validateAircraftResponse)
	if err != nil {
		return source.Frame[domain.AircraftBatch]{}, err
	}
	return source.Frame[domain.AircraftBatch]{FetchedAt: fetchedAt, Provider: domain.ProviderReadsb, Value: normalizeAircraft(response)}, nil
}

func (client *Client) FetchReceiver(ctx context.Context) (source.Frame[domain.Receiver], error) {
	var response receiverResponse
	fetchedAt, err := fetchJSON(ctx, client, receiverPath, maxReceiverBytes, &response, validateReceiverResponse)
	if err != nil {
		return source.Frame[domain.Receiver]{}, err
	}
	return source.Frame[domain.Receiver]{FetchedAt: fetchedAt, Provider: domain.ProviderReadsb, Value: normalizeReceiver(response, fetchedAt)}, nil
}

func (client *Client) FetchStats(ctx context.Context) (source.Frame[domain.Statistics], error) {
	var response statsResponse
	fetchedAt, err := fetchJSON(ctx, client, statsPath, maxStatsBytes, &response, validateStatsResponse)
	if err != nil {
		return source.Frame[domain.Statistics]{}, err
	}
	return source.Frame[domain.Statistics]{FetchedAt: fetchedAt, Provider: domain.ProviderReadsb, Value: normalizeStats(response, fetchedAt)}, nil
}

func fetchJSON[T any](ctx context.Context, client *Client, endpoint string, maximum int64, output *T, validate func(T) error) (time.Time, error) {
	started := time.Now()
	observation := Observation{Provider: domain.ProviderReadsb, Capability: sourceCapability(endpoint)}
	defer func() {
		if client.observer != nil {
			observation.Duration = time.Since(started)
			observation.At = time.Now().UTC()
			client.observer(observation)
		}
	}()
	requestURL := *client.baseURL
	requestURL.Path = path.Join(client.baseURL.Path, endpoint)
	requestURL.RawQuery = ""
	requestURL.Fragment = ""

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: err}
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	fetchedAt := client.now()
	if err != nil {
		class := source.ErrorNetwork
		if errors.Is(err, context.DeadlineExceeded) {
			class = source.ErrorTimeout
		}
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: class, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorStatus, Err: fmt.Errorf("HTTP %s", response.Status)}
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	observation.Bytes = len(data)
	if err != nil {
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: err}
	}
	if int64(len(data)) > maximum {
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: fmt.Errorf("payload exceeds %d bytes", maximum)}
	}
	if len(data) == 0 {
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: errors.New("empty payload")}
	}
	if err := json.Unmarshal(data, output); err != nil {
		return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: fmt.Errorf("decode JSON: %w", err)}
	}
	if validate != nil {
		if err := validate(*output); err != nil {
			return time.Time{}, &source.FetchError{Endpoint: endpoint, Class: source.ErrorPayload, Err: fmt.Errorf("validate JSON: %w", err)}
		}
	}
	observation.Success = true
	return fetchedAt, nil
}

func sourceCapability(endpoint string) domain.Capability {
	switch endpoint {
	case receiverPath:
		return domain.CapabilityReceiver
	case statsPath:
		return domain.CapabilityStatistics
	default:
		return domain.CapabilityAircraft
	}
}
