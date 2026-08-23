package airplaneslive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

const (
	defaultBaseURL   = "https://api.airplanes.live/v2"
	maxResponseBytes = 16 << 20
	maxErrorBytes    = 4 << 10
	maxRetryAfter    = 30 * time.Second
	minimumRate      = time.Second
)

type Config struct {
	Latitude        float64
	Longitude       float64
	RadiusNM        int
	Timeout         time.Duration
	MinimumInterval time.Duration
}

type Observation struct {
	Provider   domain.ProviderID
	Capability domain.Capability
	Duration   time.Duration
	Bytes      int
	Success    bool
	At         time.Time
}

type Client struct {
	baseURL         *url.URL
	httpClient      *http.Client
	latitude        float64
	longitude       float64
	radiusNM        int
	minimumInterval time.Duration
	now             func() time.Time
	wait            func(context.Context, time.Duration) error
	observer        func(Observation)

	rateMu         sync.Mutex
	nextRequest    time.Time
	retryNotBefore time.Time
}

func NewClient(config Config) (*Client, error) {
	baseURL, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, errors.New("initialize airplanes.live endpoint")
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   750 * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		MaxConnsPerHost:       2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: config.Timeout,
	}
	return newClient(baseURL, &http.Client{Transport: transport, Timeout: config.Timeout}, config)
}

func newClient(baseURL *url.URL, httpClient *http.Client, config Config) (*Client, error) {
	if baseURL == nil || httpClient == nil {
		return nil, errors.New("airplanes.live client requires HTTP configuration")
	}
	if !validPosition(config.Latitude, config.Longitude) {
		return nil, errors.New("airplanes.live client requires a valid public center")
	}
	if config.RadiusNM < 1 || config.RadiusNM > 250 {
		return nil, errors.New("airplanes.live radius must be between 1 and 250 NM")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("airplanes.live timeout must be positive")
	}
	if config.MinimumInterval < minimumRate {
		return nil, errors.New("airplanes.live poll interval must be at least one second")
	}
	if baseURL.Scheme == "" || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("airplanes.live endpoint is invalid")
	}

	baseCopy := *baseURL
	httpCopy := *httpClient
	httpCopy.Timeout = config.Timeout
	httpCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		baseURL:         &baseCopy,
		httpClient:      &httpCopy,
		latitude:        config.Latitude,
		longitude:       config.Longitude,
		radiusNM:        config.RadiusNM,
		minimumInterval: config.MinimumInterval,
		now:             time.Now,
		wait:            waitContext,
	}, nil
}

func (client *Client) SetObserver(observer func(Observation)) {
	client.observer = observer
}

func (*Client) ProviderID() domain.ProviderID {
	return domain.ProviderAirplanesLive
}

func (*Client) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityAircraft)
}

func (client *Client) FetchAircraft(ctx context.Context) (source.Frame[domain.AircraftBatch], error) {
	started := time.Now()
	observation := Observation{Provider: domain.ProviderAirplanesLive, Capability: domain.CapabilityAircraft}
	defer func() {
		if client.observer != nil {
			observation.Duration = time.Since(started)
			observation.At = client.now().UTC()
			client.observer(observation)
		}
	}()

	if err := client.acquire(ctx); err != nil {
		return source.Frame[domain.AircraftBatch]{}, err
	}
	requestURL := *client.baseURL
	requestURL.Path = path.Join(
		client.baseURL.Path,
		"point",
		strconv.FormatFloat(client.latitude, 'f', -1, 64),
		strconv.FormatFloat(client.longitude, 'f', -1, 64),
		strconv.Itoa(client.radiusNM),
	)
	requestURL.RawQuery = ""
	requestURL.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return source.Frame[domain.AircraftBatch]{}, payloadError("construct request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "SkyFeed airplanes.live source")

	response, err := client.httpClient.Do(request)
	fetchedAt := client.now().UTC()
	if err != nil {
		if ctx.Err() != nil {
			return source.Frame[domain.AircraftBatch]{}, ctx.Err()
		}
		if timeoutError(err) {
			return source.Frame[domain.AircraftBatch]{}, &source.FetchError{
				Endpoint: "aircraft",
				Class:    source.ErrorTimeout,
				Err:      errors.New("request timed out"),
			}
		}
		return source.Frame[domain.AircraftBatch]{}, &source.FetchError{
			Endpoint: "aircraft",
			Class:    source.ErrorNetwork,
			Err:      errors.New("network request failed"),
		}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		bytesRead, _ := io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBytes))
		observation.Bytes = int(bytesRead)
		retryAfter := time.Duration(0)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			retryAfter = boundedRetryAfter(response.Header.Get("Retry-After"), fetchedAt)
			client.deferRequests(retryAfter)
		}
		message := fmt.Sprintf("HTTP %d", response.StatusCode)
		if retryAfter > 0 {
			message += fmt.Sprintf("; retry after %s", retryAfter)
		}
		return source.Frame[domain.AircraftBatch]{}, &source.FetchError{
			Endpoint: "aircraft",
			Class:    source.ErrorStatus,
			Err:      errors.New(message),
		}
	}
	if !jsonContentType(response.Header.Get("Content-Type")) {
		return source.Frame[domain.AircraftBatch]{}, payloadError("response content type is not JSON")
	}

	limited := &io.LimitedReader{R: response.Body, N: maxResponseBytes + 1}
	counted := &countingReader{reader: limited}
	decoder := json.NewDecoder(counted)
	var payload pointResponse
	decodeErr := decoder.Decode(&payload)
	if limited.N == 0 {
		observation.Bytes = counted.bytes
		return source.Frame[domain.AircraftBatch]{}, payloadError("payload exceeds size limit")
	}
	if decodeErr != nil {
		observation.Bytes = counted.bytes
		return source.Frame[domain.AircraftBatch]{}, payloadError("decode JSON")
	}
	if err := ensureEOF(decoder); err != nil {
		observation.Bytes = counted.bytes
		return source.Frame[domain.AircraftBatch]{}, err
	}
	if limited.N == 0 {
		observation.Bytes = counted.bytes
		return source.Frame[domain.AircraftBatch]{}, payloadError("payload exceeds size limit")
	}
	observation.Bytes = counted.bytes
	if err := validatePointResponse(payload); err != nil {
		return source.Frame[domain.AircraftBatch]{}, &source.FetchError{
			Endpoint: "aircraft",
			Class:    source.ErrorPayload,
			Err:      err,
		}
	}

	observation.Success = true
	return source.Frame[domain.AircraftBatch]{
		FetchedAt: fetchedAt,
		Provider:  domain.ProviderAirplanesLive,
		Value:     normalizePoint(payload),
	}, nil
}

func (client *Client) acquire(ctx context.Context) error {
	for {
		now := client.now()
		client.rateMu.Lock()
		notBefore := client.nextRequest
		if client.retryNotBefore.After(notBefore) {
			notBefore = client.retryNotBefore
		}
		if !notBefore.After(now) {
			client.nextRequest = now.Add(client.minimumInterval)
			client.rateMu.Unlock()
			return nil
		}
		delay := notBefore.Sub(now)
		client.rateMu.Unlock()
		if err := client.wait(ctx, delay); err != nil {
			return err
		}
	}
}

func (client *Client) deferRequests(delay time.Duration) {
	if delay <= 0 {
		return
	}
	notBefore := client.now().Add(delay)
	client.rateMu.Lock()
	if notBefore.After(client.retryNotBefore) {
		client.retryNotBefore = notBefore
	}
	client.rateMu.Unlock()
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func boundedRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	delay := minimumRate
	if asciiDigits(raw) {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || seconds >= uint64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		delay = time.Duration(seconds) * time.Second
	} else if value, err := http.ParseTime(raw); err == nil && value.After(now) {
		delay = value.Sub(now)
	}
	if delay < minimumRate {
		return minimumRate
	}
	if delay > maxRetryAfter {
		return maxRetryAfter
	}
	return delay
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func jsonContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return payloadError("response contains multiple JSON values")
	}
	return nil
}

func payloadError(message string) error {
	return &source.FetchError{
		Endpoint: "aircraft",
		Class:    source.ErrorPayload,
		Err:      errors.New(message),
	}
}

func timeoutError(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

type countingReader struct {
	reader io.Reader
	bytes  int
}

func (reader *countingReader) Read(data []byte) (int, error) {
	count, err := reader.reader.Read(data)
	reader.bytes += count
	return count, err
}

var _ source.AircraftSource = (*Client)(nil)
