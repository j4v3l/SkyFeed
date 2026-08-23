package aviationweather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLookupFetchesMETARAndTAF(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		switch {
		case strings.HasSuffix(request.URL.Path, "/metar"):
			if request.URL.Query().Get("ids") != "KPBI" {
				t.Fatalf("metar ids = %q", request.URL.Query().Get("ids"))
			}
			_, _ = writer.Write([]byte(`[{"rawOb":"KPBI 231453Z 14008KT 10SM FEW040 28/22 A3001"}]`))
		case strings.HasSuffix(request.URL.Path, "/taf"):
			_, _ = writer.Write([]byte(`[{"rawTAF":"KPBI 231120Z 2312/2412 14010KT P6SM SCT040"}]`))
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.base = base

	observation, err := client.Lookup(context.Background(), "kpbi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(observation.METAR, "KPBI 231453Z") || !strings.Contains(observation.TAF, "KPBI 231120Z") {
		t.Fatalf("observation = %+v", observation)
	}
	if observation.Attribution != Attribution {
		t.Fatalf("attribution = %q", observation.Attribution)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}

	cached, err := client.Lookup(context.Background(), "KPBI")
	if err != nil {
		t.Fatal(err)
	}
	if cached.METAR != observation.METAR || hits != 2 {
		t.Fatalf("cache miss: hits=%d cached=%+v", hits, cached)
	}
}

func TestLookupRejectsNonICAO(t *testing.T) {
	client, err := NewClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lookup(context.Background(), "PBI"); err == nil {
		t.Fatal("expected ICAO validation error")
	}
}
