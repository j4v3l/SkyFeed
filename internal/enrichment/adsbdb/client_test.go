package adsbdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/enrichment"
)

func TestClientMapsCombinedResponseAndNormalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v0/aircraft/ABC123" || request.URL.Query().Get("callsign") != "SKY123" {
			t.Fatalf("request URL = %s", request.URL)
		}
		_, _ = writer.Write([]byte(`{"response":{"aircraft":{"type":"Boeing 737","icao_type":"B738","manufacturer":"Boeing","registration":"N123SF","registered_owner":"SkyFeed Air"},"flightroute":{"callsign":"SKY123","airline":{"name":"SkyFeed Air","icao":"SKY"},"origin":{"name":"Origin","icao_code":"KAAA"},"destination":{"name":"Destination","icao_code":"KBBB"}}}}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL + "/v0")
	client := NewClientWithHTTP(base, server.Client())
	result, err := client.Lookup(context.Background(), "abc123", " sky123 ")
	if err != nil {
		t.Fatal(err)
	}
	if result.Aircraft == nil || result.Aircraft.Registration != "N123SF" || result.Route == nil || result.Route.Destination.ICAO != "KBBB" {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientNotFoundRateLimitAndMalformed(t *testing.T) {
	for _, test := range []struct {
		name                        string
		status                      int
		body                        string
		wantNotFound, wantTransient bool
	}{
		{name: "not found", status: 404, wantNotFound: true},
		{name: "rate limit", status: 429, body: `{}`, wantTransient: true},
		{name: "malformed", status: 200, body: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			_, err := NewClientWithHTTP(base, server.Client()).Lookup(context.Background(), "ABC123", "")
			if test.wantNotFound && !errors.Is(err, enrichment.ErrNotFound) {
				t.Fatalf("err=%v", err)
			}
			if test.wantTransient {
				var requestErr *RequestError
				if !errors.As(err, &requestErr) || !requestErr.Transient {
					t.Fatalf("err=%v", err)
				}
			}
			if !test.wantNotFound && !test.wantTransient && err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"response":{}}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := NewClientWithHTTP(base, &http.Client{Timeout: 5 * time.Millisecond})
	_, err := client.Lookup(context.Background(), "ABC123", "")
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || !requestErr.Transient {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"response":{}}` + strings.Repeat(" ", maxResponseBytes)))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	if _, err := NewClientWithHTTP(base, server.Client()).Lookup(context.Background(), "ABC123", ""); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestClientAirlineCallsignAndModeS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v0/airline/JBU":
			_, _ = writer.Write([]byte(`{"response":[{"name":"JetBlue","icao":"JBU","iata":"B6","country":"United States","callsign":"JETBLUE"}]}`))
		case "/v0/callsign/JBU123":
			_, _ = writer.Write([]byte(`{"response":{"flightroute":{"callsign":"JBU123","origin":{"icao_code":"KJFK"},"destination":{"icao_code":"KPBI"}}}}`))
		case "/v0/mode-s/ABC123":
			_, _ = writer.Write([]byte(`{"response":"N123SF"}`))
		case "/v0/n-number/N123SF":
			_, _ = writer.Write([]byte(`{"response":"ABC123"}`))
		default:
			t.Fatalf("path=%s", request.URL.Path)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL + "/v0")
	client := NewClientWithHTTP(base, server.Client())
	airline, err := client.LookupAirline(context.Background(), "jbu")
	if err != nil || airline.ICAO != "JBU" || airline.RadioCallsign != "JETBLUE" {
		t.Fatalf("airline=%+v err=%v", airline, err)
	}
	route, err := client.LookupCallsign(context.Background(), "jbu123")
	if err != nil || route.Route == nil || route.Route.Destination.ICAO != "KPBI" {
		t.Fatalf("callsign=%+v err=%v", route, err)
	}
	registration, err := client.LookupModeS(context.Background(), "abc123")
	if err != nil || registration != "N123SF" {
		t.Fatalf("mode-s=%q err=%v", registration, err)
	}
	hex, err := client.LookupNNumber(context.Background(), "n123sf")
	if err != nil || hex != "ABC123" {
		t.Fatalf("n-number=%q err=%v", hex, err)
	}
}
