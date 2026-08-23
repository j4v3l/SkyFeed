package adsblol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/enrichment"
)

func TestClientRoutesAndAirport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/0/routeset":
			if request.Method != http.MethodPost {
				t.Fatalf("method=%s", request.Method)
			}
			var payload struct {
				Planes []map[string]any `json:"planes"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Planes) != 1 {
				t.Fatalf("planes=%v", payload.Planes)
			}
			plane := payload.Planes[0]
			if len(plane) != 3 || plane["callsign"] != "SKY123" || plane["lat"] != 12.5 || plane["lng"] != -45.25 {
				t.Fatalf("route payload contains unexpected data: %#v", plane)
			}
			_, _ = writer.Write([]byte(`[
				{
					"_airport_codes_iata":"AAA-BBB",
					"_airports":[
						{"alt_feet":10,"alt_meters":3.05,"countryiso2":"US","iata":"AAA","icao":"KAAA","lat":10,"location":"Origin","lon":-40,"name":"Origin @everyone"},
						{"alt_feet":20,"alt_meters":6.1,"countryiso2":"US","iata":"BBB","icao":"KBBB","lat":20,"location":"Destination","lon":-50,"name":"Destination"}
					],
					"airline_code":"SKY",
					"airport_codes":"KAAA-KBBB",
					"callsign":"SKY123",
					"number":"123",
					"plausible":true
				}
			]`))
		case "/api/0/airport/KBBB":
			if request.Method != http.MethodGet {
				t.Fatalf("method=%s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"alt_feet":20,"alt_meters":6.1,"countryiso2":"US","iata":"BBB","icao":"KBBB","lat":20,"location":"Destination","lon":-50,"name":"Destination"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	routes, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{
		{Callsign: " sky123 ", Latitude: 12.5, Longitude: -45.25},
		{Callsign: "SKY123", Latitude: 1, Longitude: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	route, ok := routes["SKY123"]
	if !ok || route.Origin.ICAO != "KAAA" || route.Destination.ICAO != "KBBB" ||
		!route.PlausibilityKnown || !route.Plausible || route.Attribution != Attribution {
		t.Fatalf("route=%+v", route)
	}
	if route.Origin.Name != "Origin ＠everyone" || !route.Origin.HasPosition || !route.Origin.HasElevation {
		t.Fatalf("origin=%+v", route.Origin)
	}

	airport, err := client.LookupAirport(context.Background(), " kbbb ")
	if err != nil {
		t.Fatal(err)
	}
	if airport.ICAO != "KBBB" || airport.ElevationFeet != 20 || airport.Attribution != Attribution {
		t.Fatalf("airport=%+v", airport)
	}
}

func TestClientUnknownRouteAndAirport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/0/routeset" {
			_, _ = writer.Write([]byte(`[{"_airport_codes_iata":"","_airports":[],"airline_code":"ZZZ","airport_codes":"unknown","callsign":"ZZZ1","number":"1"}]`))
			return
		}
		_, _ = writer.Write([]byte(`null`))
	}))
	defer server.Close()
	client := testClient(t, server)

	routes, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{{Callsign: "ZZZ1", Latitude: 1, Longitude: 2}})
	if err != nil || len(routes) != 0 {
		t.Fatalf("routes=%v err=%v", routes, err)
	}
	if _, err := client.LookupAirport(context.Background(), "KZZZ"); !errors.Is(err, enrichment.ErrNotFound) {
		t.Fatalf("airport err=%v", err)
	}
}

func TestClientClassifiesAndSanitizesErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "120")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"provider":"detail"}`))
	}))
	defer server.Close()
	client := testClient(t, server)

	_, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{{Callsign: "SECRET1", Latitude: 12.345, Longitude: -67.89}})
	var requestError *RequestError
	if !errors.As(err, &requestError) || !requestError.Retryable() || requestError.RetryDelay() != maxRetryAfter {
		t.Fatalf("err=%v", err)
	}
	for _, forbidden := range []string{server.URL, "SECRET1", "12.345", "-67.89", "detail"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error leaked %q: %v", forbidden, err)
		}
	}
}

func TestClientDoesNotForwardRoutePayloadOnRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client := testClient(t, server)
	_, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{{Callsign: "SKY789", Latitude: 5, Longitude: 6}})
	var requestError *RequestError
	if !errors.As(err, &requestError) || requestError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("err=%v", err)
	}
	if redirected.Load() {
		t.Fatal("route payload was forwarded to redirect target")
	}
}

func TestClientRejectsMalformedOversizedAndUnexpectedFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `[`},
		{name: "oversized", body: `[]` + strings.Repeat(" ", maxResponseBytes)},
		{name: "unexpected field", body: `[{"callsign":"SKY1","airport_codes":"unknown","_airports":[],"unexpected":true}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client := testClient(t, server)
			if _, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{{Callsign: "SKY1", Latitude: 1, Longitude: 2}}); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestClientValidatesBaseAndBatch(t *testing.T) {
	for _, raw := range []string{
		"http://api.adsb.lol",
		"https://example.com",
		"https://user:pass@api.adsb.lol",
		"https://api.adsb.lol?secret=value",
	} {
		base, _ := url.Parse(raw)
		if _, err := NewClient(base, time.Second); err == nil {
			t.Fatalf("accepted base %q", raw)
		}
	}
	base, _ := url.Parse("https://api.adsb.lol")
	if _, err := NewClient(base, time.Second); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	client := testClient(t, server)
	requests := make([]enrichment.RouteRequest, enrichment.MaxRouteBatchSize+1)
	for index := range requests {
		requests[index] = enrichment.RouteRequest{Callsign: "SKY123", Latitude: 1, Longitude: 2}
	}
	if _, err := client.LookupRoutes(context.Background(), requests); err == nil {
		t.Fatal("expected batch limit error")
	}
	if _, err := client.LookupRoutes(context.Background(), []enrichment.RouteRequest{{Callsign: "BAD CALL", Latitude: 1, Longitude: 2}}); err == nil {
		t.Fatal("expected callsign validation error")
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClientWithHTTP(base, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}
