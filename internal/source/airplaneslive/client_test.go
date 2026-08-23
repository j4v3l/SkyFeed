package airplaneslive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

func TestNormalizeSyntheticPointFixture(t *testing.T) {
	payload := decodePointFixture(t, "point.json")
	if err := validatePointResponse(payload); err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	batch := normalizePoint(payload)
	if batch.Provider != domain.ProviderAirplanesLive || batch.MessageCounterValid {
		t.Fatalf("provider state = %+v", batch)
	}
	if got := batch.GeneratedAt.UnixMilli(); got != 1_787_414_400_123 {
		t.Fatalf("generated milliseconds = %d", got)
	}
	if len(batch.Aircraft) != 2 {
		t.Fatalf("aircraft count = %d", len(batch.Aircraft))
	}
	first := batch.Aircraft[0]
	if first.ICAO != "ABC123" || first.Callsign != "SKY123" || first.Provider != domain.ProviderAirplanesLive {
		t.Fatalf("first aircraft = %+v", first)
	}
	if !first.HasPosition || !first.HasAltitude || !first.HasGroundSpeed || !first.HasTrack || !first.HasVerticalRate || !first.HasRSSI {
		t.Fatalf("first aircraft fields = %+v", first)
	}
	second := batch.Aircraft[1]
	if second.ICAO != "~DEF456" || !second.OnGround || second.HasAltitude || second.TrackDegrees != 0 {
		t.Fatalf("second aircraft = %+v", second)
	}
}

func TestEmptyPointFixtureIsValid(t *testing.T) {
	payload := decodePointFixture(t, "point-empty.json")
	if err := validatePointResponse(payload); err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	if batch := normalizePoint(payload); batch.Aircraft == nil || len(batch.Aircraft) != 0 {
		t.Fatalf("empty batch = %+v", batch)
	}
}

func TestClientUsesFixedPointPathAndBoundedJSON(t *testing.T) {
	fixture, err := os.ReadFile(fixturePath("point.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v2/point/1.25/-2.5/50" || request.URL.RawQuery != "" {
			t.Fatalf("request target = %s %s", request.Method, request.URL.RequestURI())
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/v2", server.Client(), Config{
		Latitude: 1.25, Longitude: -2.5, RadiusNM: 50,
		Timeout: time.Second, MinimumInterval: time.Second,
	})
	frame, err := client.FetchAircraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frame.Provider != domain.ProviderAirplanesLive || len(frame.Value.Aircraft) != 2 {
		t.Fatalf("frame = %+v", frame)
	}
}

func TestClientRejectsMalformedOversizedAndSemanticPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "malformed", body: `{"ac":`},
		{name: "multiple values", body: `{"ac":[],"msg":"No error","now":1787414400000,"total":0} {}`},
		{name: "missing array", body: `{"msg":"No error","now":1787414400000,"total":0}`},
		{name: "seconds timestamp", body: `{"ac":[],"msg":"No error","now":1787414400,"total":0}`},
		{name: "mismatched total", body: `{"ac":[],"msg":"No error","now":1787414400000,"total":1}`},
		{name: "incomplete position", body: `{"ac":[{"hex":"abc123","lat":1,"seen":0}],"msg":"No error","now":1787414400000,"total":1}`},
		{name: "provider error", body: `{"ac":[],"msg":"rate limited","now":1787414400000,"total":0}`},
		{name: "oversized", body: `{"ac":[],"msg":"No error","now":1787414400000,"total":0}` + strings.Repeat(" ", maxResponseBytes)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/v2", server.Client(), testConfig())
			_, err := client.FetchAircraft(context.Background())
			if err == nil || source.ClassifyError(err) != source.ErrorPayload {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestClientHandlesRateLimitWithoutRetryAndBoundsBackoff(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Retry-After", "999999")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"message":"do not expose provider text"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/v2", server.Client(), testConfig())
	now := time.Unix(1_787_414_400, 0)
	client.now = func() time.Time { return now }
	_, err := client.FetchAircraft(context.Background())
	if err == nil || source.ClassifyError(err) != source.ErrorStatus {
		t.Fatalf("error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
	client.rateMu.Lock()
	backoff := client.retryNotBefore.Sub(now)
	client.rateMu.Unlock()
	if backoff != maxRetryAfter {
		t.Fatalf("backoff = %s", backoff)
	}
	if strings.Contains(err.Error(), "do not expose") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("unsanitized error = %q", err)
	}
}

func TestClientRateWaitIsCancellationSafe(t *testing.T) {
	fixture, err := os.ReadFile(fixturePath("point-empty.json"))
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/v2", server.Client(), testConfig())
	if _, err := client.FetchAircraft(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FetchAircraft(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests after cancellation = %d", requests.Load())
	}
}

func TestClientReservesAtLeastOneSecondBetweenRequests(t *testing.T) {
	now := time.Unix(1_787_414_400, 0)
	var waited time.Duration
	client := &Client{
		minimumInterval: time.Second,
		now:             func() time.Time { return now },
		wait: func(_ context.Context, delay time.Duration) error {
			waited = delay
			return context.Canceled
		},
	}
	if err := client.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.acquire(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("second acquire error = %v", err)
	}
	if waited != time.Second {
		t.Fatalf("rate-limit wait = %s", waited)
	}
}

func TestClientSanitizesNetworkErrorsAndCoordinates(t *testing.T) {
	base, err := url.Parse("https://sensitive.invalid/v2")
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(`GET "https://sensitive.invalid/v2/point/12.345/-67.89/50": dial failed`)
	})}
	client, err := newClient(base, httpClient, Config{
		Latitude: 12.345, Longitude: -67.89, RadiusNM: 50,
		Timeout: time.Second, MinimumInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.FetchAircraft(context.Background())
	if err == nil || source.ClassifyError(err) != source.ErrorNetwork {
		t.Fatalf("error = %v", err)
	}
	for _, private := range []string{"sensitive.invalid", "12.345", "-67.89", "/point/"} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("error contains %q: %q", private, err)
		}
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := newTestClient(t, origin.URL+"/v2", origin.Client(), testConfig())
	if _, err := client.FetchAircraft(context.Background()); err == nil || source.ClassifyError(err) != source.ErrorStatus {
		t.Fatalf("error = %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirected requests = %d", redirected.Load())
	}
}

func TestBoundedRetryAfterSupportsDeltaAndDate(t *testing.T) {
	now := time.Unix(1_787_414_400, 0).UTC()
	if got := boundedRetryAfter("0", now); got != time.Second {
		t.Fatalf("zero delay = %s", got)
	}
	if got := boundedRetryAfter("999999", now); got != maxRetryAfter {
		t.Fatalf("large delay = %s", got)
	}
	if got := boundedRetryAfter(now.Add(5*time.Second).Format(http.TimeFormat), now); got != 5*time.Second {
		t.Fatalf("date delay = %s", got)
	}
}

func decodePointFixture(t *testing.T, name string) pointResponse {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatal(err)
	}
	var payload pointResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "..", "test", "fixtures", "airplaneslive", name)
}

func newTestClient(t *testing.T, rawURL string, httpClient *http.Client, config Config) *Client {
	t.Helper()
	base, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newClient(base, httpClient, config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testConfig() Config {
	return Config{
		Latitude: 1.25, Longitude: -2.5, RadiusNM: 50,
		Timeout: time.Second, MinimumInterval: time.Second,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
