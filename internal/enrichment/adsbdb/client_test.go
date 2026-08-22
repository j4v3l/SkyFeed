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
