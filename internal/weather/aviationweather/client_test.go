package aviationweather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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

func TestLookupSingleflightAndBoundedCache(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		time.Sleep(5 * time.Millisecond)
		if strings.HasSuffix(request.URL.Path, "/metar") {
			_, _ = writer.Write([]byte(`[{"rawOb":"TEST 231453Z 14008KT 10SM FEW040","fltCat":"VFR","additive":true}]`))
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	client.max = 2
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := client.Lookup(context.Background(), "KAAA"); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if hits.Load() != 2 {
		t.Fatalf("singleflight hits = %d", hits.Load())
	}
	for _, code := range []string{"KBBB", "KCCC"} {
		if _, err := client.Lookup(context.Background(), code); err != nil {
			t.Fatal(err)
		}
	}
	if client.CacheLen() != 2 {
		t.Fatalf("cache length = %d", client.CacheLen())
	}
}

func TestLookupNegativeCachesUpstreamFailure(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	for range 2 {
		if _, err := client.Lookup(context.Background(), "KAAA"); err == nil {
			t.Fatal("expected weather failure")
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("negative cache hits = %d", hits.Load())
	}
}

func TestLookupServesStaleCacheAfterTransientFailure(t *testing.T) {
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failing.Load() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if strings.HasSuffix(request.URL.Path, "/metar") {
			_, _ = writer.Write([]byte(`[{"rawOb":"KAAA 231453Z 14008KT 10SM FEW040","fltCat":"VFR"}]`))
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	now := time.Unix(1_700_000_000, 0)
	client.now = func() time.Time { return now }
	client.ttl = time.Minute
	if _, err := client.Lookup(context.Background(), "KAAA"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	failing.Store(true)
	value, err := client.Lookup(context.Background(), "KAAA")
	if err != nil || !value.Stale || value.METAR == "" {
		t.Fatalf("stale=%+v err=%v", value, err)
	}
}

func TestLookupRejectsMalformedOversizedRedirectAndTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{name: "malformed", handler: func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(`{`)) }, timeout: time.Second},
		{name: "oversized", handler: func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`[]` + strings.Repeat(" ", maxResponseBytes)))
		}, timeout: time.Second},
		{name: "timeout", handler: func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(25 * time.Millisecond)
			_, _ = writer.Write([]byte(`[]`))
		}, timeout: time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, _ := NewClient(test.timeout)
			client.base, _ = url.Parse(server.URL)
			if _, err := client.Lookup(context.Background(), "KAAA"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		var followed atomic.Bool
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			followed.Store(true)
			_, _ = writer.Write([]byte(`[]`))
		}))
		defer target.Close()
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL, http.StatusFound)
		}))
		defer server.Close()
		client, _ := NewClient(time.Second)
		client.base, _ = url.Parse(server.URL)
		if _, err := client.Lookup(context.Background(), "KAAA"); err == nil || followed.Load() {
			t.Fatalf("err=%v followed=%t", err, followed.Load())
		}
	})
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
