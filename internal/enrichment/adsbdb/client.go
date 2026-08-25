package adsbdb

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
	"strconv"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/enrichment"
)

const maxResponseBytes = 1 << 20

type RequestError struct {
	StatusCode int
	RetryAfter time.Duration
	Transient  bool
	Cause      error
}

func (err *RequestError) Error() string {
	if err.StatusCode != 0 {
		return fmt.Sprintf("ADSBDB returned HTTP %d", err.StatusCode)
	}
	return fmt.Sprintf("ADSBDB request: %v", err.Cause)
}

func (err *RequestError) Unwrap() error             { return err.Cause }
func (err *RequestError) Retryable() bool           { return err.Transient }
func (err *RequestError) RetryDelay() time.Duration { return err.RetryAfter }

type Client struct {
	base       *url.URL
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(base *url.URL, timeout time.Duration) *Client {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 750 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: 1500 * time.Millisecond,
	}
	copyBase := *base
	return &Client{base: &copyBase, httpClient: &http.Client{Transport: transport, Timeout: timeout}, now: time.Now}
}

func NewClientWithHTTP(base *url.URL, client *http.Client) *Client {
	copyBase := *base
	return &Client{base: &copyBase, httpClient: client, now: time.Now}
}

func (client *Client) Lookup(ctx context.Context, icao, callsign string) (domain.Enrichment, error) {
	icao, callsign, _ = enrichment.NormalizeKey(icao, callsign)
	if icao == "" {
		return domain.Enrichment{}, errors.New("icao is required")
	}
	endpoint := *client.base
	endpoint.Path = path.Join(endpoint.Path, "aircraft", url.PathEscape(icao))
	query := endpoint.Query()
	if callsign != "" {
		query.Set("callsign", callsign)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return domain.Enrichment{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return domain.Enrichment{}, &RequestError{Transient: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return domain.Enrichment{}, enrichment.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return domain.Enrichment{}, &RequestError{StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), client.now()), Transient: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}
	}
	limited := http.MaxBytesReader(nil, response.Body, maxResponseBytes)
	decoder := json.NewDecoder(limited)
	var dto responseDTO
	if err := decoder.Decode(&dto); err != nil {
		return domain.Enrichment{}, fmt.Errorf("decode ADSBDB response: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return domain.Enrichment{}, err
	}
	result := mapResponse(icao, callsign, dto.Response)
	if !result.Found {
		return domain.Enrichment{}, enrichment.ErrNotFound
	}
	result.FetchedAt = client.now().UTC()
	result.Attribution = "Aircraft and route enrichment by ADSBDB"
	return result, nil
}

func (client *Client) LookupAirline(ctx context.Context, code string) (domain.Airline, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < 2 || len(code) > 3 {
		return domain.Airline{}, errors.New("airline code must be 2 or 3 characters")
	}
	var dto airlineListResponseDTO
	if err := client.get(ctx, []string{"airline", code}, &dto); err != nil {
		return domain.Airline{}, err
	}
	if len(dto.Response) == 0 {
		return domain.Airline{}, enrichment.ErrNotFound
	}
	value := dto.Response[0]
	return domain.Airline{
		Name: value.Name, ICAO: value.ICAO, IATA: value.IATA, Country: value.Country, CountryISO: value.CountryISO,
		RadioCallsign: value.Callsign, Attribution: "Airline data by ADSBDB",
	}, nil
}

func (client *Client) LookupCallsign(ctx context.Context, callsign string) (domain.Enrichment, error) {
	_, callsign, _ = enrichment.NormalizeKey("", callsign)
	if callsign == "" {
		return domain.Enrichment{}, errors.New("callsign is required")
	}
	var dto responseDTO
	if err := client.get(ctx, []string{"callsign", callsign}, &dto); err != nil {
		return domain.Enrichment{}, err
	}
	result := mapResponse("", callsign, dto.Response)
	if !result.Found {
		return domain.Enrichment{}, enrichment.ErrNotFound
	}
	result.FetchedAt = client.now().UTC()
	result.Attribution = "Aircraft and route enrichment by ADSBDB"
	return result, nil
}

func (client *Client) LookupModeS(ctx context.Context, hex string) (string, error) {
	hex = strings.ToUpper(strings.TrimSpace(hex))
	if len(hex) != 6 {
		return "", errors.New("mode S hex must be 6 characters")
	}
	return client.lookupString(ctx, []string{"mode-s", hex})
}

func (client *Client) LookupNNumber(ctx context.Context, registration string) (string, error) {
	registration = strings.ToUpper(strings.TrimSpace(registration))
	if registration == "" {
		return "", errors.New("n-number is required")
	}
	return client.lookupString(ctx, []string{"n-number", registration})
}

func (client *Client) lookupString(ctx context.Context, segments []string) (string, error) {
	var dto stringResponseDTO
	if err := client.get(ctx, segments, &dto); err != nil {
		return "", err
	}
	value := strings.ToUpper(strings.TrimSpace(dto.Response))
	if value == "" {
		return "", enrichment.ErrNotFound
	}
	return value, nil
}

func (client *Client) get(ctx context.Context, segments []string, dest any) error {
	endpoint := *client.base
	parts := append([]string{endpoint.Path}, segments...)
	for index, part := range parts {
		if index == 0 {
			continue
		}
		parts[index] = url.PathEscape(part)
	}
	endpoint.Path = path.Join(parts...)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return &RequestError{Transient: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return enrichment.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return &RequestError{StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), client.now()), Transient: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}
	}
	limited := http.MaxBytesReader(nil, response.Body, maxResponseBytes)
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("decode ADSBDB response: %w", err)
	}
	return ensureEOF(decoder)
}

func mapResponse(icao, callsign string, payload payloadDTO) domain.Enrichment {
	result := domain.Enrichment{ICAO: icao, Callsign: callsign, Found: payload.Aircraft != nil || payload.FlightRoute != nil}
	if value := payload.Aircraft; value != nil {
		result.Aircraft = &domain.AircraftMetadata{Registration: value.Registration, AircraftType: value.Type, ICAOType: value.ICAOType, Manufacturer: value.Manufacturer, Owner: value.RegisteredOwner, OwnerCountry: firstNonEmpty(value.RegisteredOwnerCountryName, value.RegisteredOwnerCountryISOName), OperatorFlag: value.RegisteredOwnerOperatorFlagCode, PhotoURL: allowPhoto(value.PhotoURL), ThumbnailURL: allowPhoto(value.ThumbnailURL)}
	}
	if value := payload.FlightRoute; value != nil {
		route := &domain.Route{Callsign: value.Callsign}
		if value.Airline != nil {
			route.AirlineName, route.AirlineICAO, route.AirlineIATA = value.Airline.Name, value.Airline.ICAO, value.Airline.IATA
		}
		if value.Origin != nil {
			route.Origin = mapAirport(*value.Origin)
		}
		if value.Midpoint != nil {
			midpoint := mapAirport(*value.Midpoint)
			route.Midpoint = &midpoint
		}
		if value.Destination != nil {
			route.Destination = mapAirport(*value.Destination)
		}
		result.Route = route
	}
	return result
}

func mapAirport(value airportDTO) domain.Airport {
	return domain.Airport{CountryCode: value.CountryISOName, Municipality: value.Municipality, Name: value.Name, IATA: value.IATACode, ICAO: value.ICAOCode}
}

func allowPhoto(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "www.planespotters.net" && host != "planespotters.net" && !strings.HasSuffix(host, ".planespotters.net") {
		return ""
	}
	return parsed.String()
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if value, err := http.ParseTime(raw); err == nil && value.After(now) {
		return value.Sub(now)
	}
	return 0
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode ADSBDB response: multiple JSON values")
		}
		return fmt.Errorf("decode ADSBDB response trailer: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ enrichment.Enricher = (*Client)(nil)
