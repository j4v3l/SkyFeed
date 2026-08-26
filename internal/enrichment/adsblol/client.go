package adsblol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
)

const (
	Attribution      = "adsb.lol route and airport data (ODbL)"
	maxRequestBytes  = 32 << 10
	maxResponseBytes = 512 << 10
	maxRetryAfter    = 30 * time.Second
)

type RequestError struct {
	StatusCode int
	RetryAfter time.Duration
	Transient  bool
	Cause      error
}

func (requestError *RequestError) Error() string {
	if requestError.StatusCode != 0 {
		return fmt.Sprintf("adsb.lol returned HTTP %d", requestError.StatusCode)
	}
	return "adsb.lol request failed"
}

func (requestError *RequestError) Unwrap() error             { return requestError.Cause }
func (requestError *RequestError) Retryable() bool           { return requestError.Transient }
func (requestError *RequestError) RetryDelay() time.Duration { return requestError.RetryAfter }

type Client struct {
	base       *url.URL
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(base *url.URL, timeout time.Duration) (*Client, error) {
	if err := validateBaseURL(base, false); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 750 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: 1500 * time.Millisecond,
	}
	copyBase := *base
	return &Client{
		base:       &copyBase,
		httpClient: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: rejectRedirect},
		now:        time.Now,
	}, nil
}

// NewClientWithHTTP permits plain HTTP only for loopback httptest servers.
func NewClientWithHTTP(base *url.URL, httpClient *http.Client) (*Client, error) {
	if err := validateBaseURL(base, true); err != nil {
		return nil, err
	}
	if httpClient == nil {
		return nil, errors.New("adsb.lol HTTP client is required")
	}
	copyBase := *base
	copyHTTP := *httpClient
	copyHTTP.CheckRedirect = rejectRedirect
	return &Client{base: &copyBase, httpClient: &copyHTTP, now: time.Now}, nil
}

func rejectRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (client *Client) LookupRoutes(ctx context.Context, requests []enrichment.RouteRequest) (map[string]domain.Route, error) {
	planes, requested, err := normalizeRequests(requests)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(routesRequestDTO{Planes: planes})
	if err != nil {
		return nil, errors.New("encode adsb.lol route request")
	}
	if len(body) > maxRequestBytes {
		return nil, errors.New("adsb.lol route request is too large")
	}

	endpoint := client.endpoint("api", "0", "routeset")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("build adsb.lol route request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, &RequestError{Transient: true, Cause: err}
	}
	defer response.Body.Close()
	if err := responseError(response, client.now()); err != nil {
		return nil, err
	}

	payload, err := decodeRoutes(response.Body)
	if err != nil {
		return nil, fmt.Errorf("decode adsb.lol routes response: %w", err)
	}
	routes := make(map[string]domain.Route, len(payload))
	for _, value := range payload {
		callsign, ok := enrichment.NormalizeCallsign(value.Callsign)
		if !ok {
			continue
		}
		if _, expected := requested[callsign]; !expected {
			continue
		}
		route, ok := mapRoute(callsign, value)
		if ok {
			routes[callsign] = route
		}
	}
	return routes, nil
}

func (client *Client) LookupAirport(ctx context.Context, code string) (domain.Airport, error) {
	code, ok := enrichment.NormalizeAirportCode(code)
	if !ok {
		return domain.Airport{}, errors.New("valid ICAO airport code is required")
	}
	endpoint := client.endpoint("api", "0", "airport", url.PathEscape(code))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return domain.Airport{}, errors.New("build adsb.lol airport request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return domain.Airport{}, &RequestError{Transient: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		discard(response.Body)
		return domain.Airport{}, enrichment.ErrNotFound
	}
	if err := responseError(response, client.now()); err != nil {
		return domain.Airport{}, err
	}

	var payload *airportDTO
	if err := decode(response.Body, maxResponseBytes, &payload); err != nil {
		return domain.Airport{}, fmt.Errorf("decode adsb.lol airport response: %w", err)
	}
	if payload == nil {
		return domain.Airport{}, enrichment.ErrNotFound
	}
	airport, ok := mapAirport(*payload)
	if !ok || airport.ICAO != code {
		return domain.Airport{}, enrichment.ErrNotFound
	}
	return airport, nil
}

func (client *Client) endpoint(parts ...string) url.URL {
	endpoint := *client.base
	allParts := append([]string{endpoint.Path}, parts...)
	endpoint.Path = path.Join(allParts...)
	endpoint.RawPath = ""
	return endpoint
}

func validateBaseURL(base *url.URL, allowLoopbackHTTP bool) error {
	if base == nil || !base.IsAbs() || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("adsb.lol base URL must be absolute and contain no credentials, query, or fragment")
	}
	host := strings.ToLower(base.Hostname())
	if base.Scheme == "https" {
		if host != "api.adsb.lol" {
			return errors.New("adsb.lol base URL host must be api.adsb.lol")
		}
		if port := base.Port(); port != "" && port != "443" {
			return errors.New("adsb.lol base URL must use the HTTPS default port")
		}
		return nil
	}
	if allowLoopbackHTTP && base.Scheme == "http" && isLoopbackHost(host) {
		return nil
	}
	return errors.New("adsb.lol base URL must use HTTPS")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func normalizeRequests(requests []enrichment.RouteRequest) ([]planeDTO, map[string]struct{}, error) {
	if len(requests) == 0 {
		return nil, nil, errors.New("at least one route request is required")
	}
	if len(requests) > enrichment.MaxRouteBatchSize {
		return nil, nil, fmt.Errorf("route batch exceeds %d aircraft", enrichment.MaxRouteBatchSize)
	}
	planes := make([]planeDTO, 0, len(requests))
	requested := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		callsign, ok := enrichment.NormalizeCallsign(request.Callsign)
		if !ok || !validPosition(request.Latitude, request.Longitude) {
			continue
		}
		if _, duplicate := requested[callsign]; duplicate {
			continue
		}
		requested[callsign] = struct{}{}
		planes = append(planes, planeDTO{Callsign: callsign, Latitude: request.Latitude, Longitude: request.Longitude})
	}
	if len(planes) == 0 {
		return nil, nil, errors.New("at least one unique route request is required")
	}
	return planes, requested, nil
}

func mapRoute(callsign string, value routeDTO) (domain.Route, bool) {
	if strings.EqualFold(strings.TrimSpace(value.AirportCodes), "unknown") || len(value.Airports) < 2 {
		return domain.Route{}, false
	}
	airports := make([]domain.Airport, 0, len(value.Airports))
	for _, item := range value.Airports {
		airport, ok := mapAirport(item)
		if !ok {
			return domain.Route{}, false
		}
		airports = append(airports, airport)
	}
	route := domain.Route{
		Source:      domain.DataSourceADSBLOL,
		Callsign:    callsign,
		AirlineICAO: sanitizeCode(value.AirlineCode, 4),
		Origin:      airports[0],
		Destination: airports[len(airports)-1],
		Airports:    airports,
		Attribution: Attribution,
	}
	if len(airports) > 2 {
		midpoint := airports[1]
		route.Midpoint = &midpoint
	}
	if value.Plausible != nil {
		route.Plausible = *value.Plausible
		route.PlausibilityKnown = true
	}
	return route, true
}

func mapAirport(value airportDTO) (domain.Airport, bool) {
	code, ok := enrichment.NormalizeAirportCode(value.ICAO)
	if !ok {
		return domain.Airport{}, false
	}
	airport := domain.Airport{
		CountryCode:  sanitizeCode(value.CountryISO2, 2),
		Municipality: sanitizeText(value.Location, 120),
		Name:         sanitizeText(value.Name, 160),
		IATA:         sanitizeCode(value.IATA, 3),
		ICAO:         code,
		Attribution:  Attribution,
	}
	if value.Latitude != nil && value.Longitude != nil && validPosition(*value.Latitude, *value.Longitude) {
		airport.Latitude = *value.Latitude
		airport.Longitude = *value.Longitude
		airport.HasPosition = true
	}
	if value.AltitudeFeet != nil && !math.IsNaN(*value.AltitudeFeet) && !math.IsInf(*value.AltitudeFeet, 0) {
		airport.ElevationFeet = *value.AltitudeFeet
		airport.HasElevation = true
	}
	return airport, true
}

func validPosition(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsInf(latitude, 0) &&
		!math.IsNaN(longitude) && !math.IsInf(longitude, 0) &&
		latitude >= -90 && latitude <= 90 &&
		longitude >= -180 && longitude <= 180
}

func sanitizeCode(value string, maxLength int) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || len(value) > maxLength {
		return ""
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return ""
		}
	}
	return value
}

func sanitizeText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			continue
		}
		if count >= maxRunes {
			break
		}
		if character == '@' {
			character = '＠'
		}
		builder.WriteRune(character)
		count++
	}
	return strings.TrimSpace(builder.String())
}

func responseError(response *http.Response, now time.Time) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	discard(response.Body)
	return &RequestError{
		StatusCode: response.StatusCode,
		RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), now),
		Transient:  response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
	}
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	} else if value, err := http.ParseTime(raw); err == nil && value.After(now) {
		delay = value.Sub(now)
	}
	return min(delay, maxRetryAfter)
}

func decodeRoutes(reader io.Reader) ([]routeDTO, error) {
	limited := io.LimitReader(reader, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("adsb.lol routes response is too large")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var payload []routeDTO
	if err := decode(bytes.NewReader(body), maxResponseBytes, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func decode(reader io.Reader, maxBytes int64, destination any) error {
	limited := http.MaxBytesReader(nil, io.NopCloser(reader), maxBytes)
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func discard(reader io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, maxResponseBytes))
}

var _ enrichment.RouteUpstream = (*Client)(nil)
