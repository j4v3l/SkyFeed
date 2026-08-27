package aviationweather

import (
	"context"
	"errors"
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
	if !observation.HasWind || observation.WindDirectionDegrees != 140 || observation.WindSpeedKts != 8 ||
		!observation.HasVisibility || observation.VisibilitySM != 10 || !observation.HasTemperature || observation.TemperatureC != 28 ||
		!observation.HasDewpoint || observation.DewpointC != 22 || !observation.HasAltimeter || observation.AltimeterInHg != 30.01 ||
		observation.FlightCategory != "VFR" || len(observation.Clouds) != 1 || observation.Clouds[0].BaseFeet != 4000 {
		t.Fatalf("decoded human weather = %+v", observation)
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

func TestPopulateMETARHandlesVariableWindFractionsAndConditions(t *testing.T) {
	observation := Observation{METAR: "KXYZ 231453Z VRB12G20KT 1 1/2SM -RA BR BKN008 M02/M05 A2988"}
	populateMETAR(&observation)
	if !observation.WindVariable || observation.WindSpeedKts != 12 || observation.WindGustKts != 20 {
		t.Fatalf("wind = %+v", observation)
	}
	if observation.VisibilitySM != 1.5 || observation.TemperatureC != -2 || observation.DewpointC != -5 || observation.FlightCategory != "IFR" {
		t.Fatalf("conditions = %+v", observation)
	}
	joined := strings.Join(observation.Conditions, " ")
	if !strings.Contains(joined, "light rain") || !strings.Contains(joined, "mist") {
		t.Fatalf("weather labels = %v", observation.Conditions)
	}
}

func TestPopulateMETARHandlesLessThanVisibility(t *testing.T) {
	observation := Observation{METAR: "KXYZ 231453Z 00000KT M1/4SM FG VV002 12/12 A2992"}
	populateMETAR(&observation)
	if !observation.HasVisibility || !observation.VisibilityLessThan || observation.VisibilityAtLeast || observation.VisibilitySM != 0.25 {
		t.Fatalf("visibility = %+v", observation)
	}
	if observation.FlightCategory != "LIFR" {
		t.Fatalf("flight category = %q", observation.FlightCategory)
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

func TestLookupResolvesRenamedReportingStationAndUsesTypedFields(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path+"?"+request.URL.RawQuery)
		if !strings.HasPrefix(request.UserAgent(), "SkyFeed/") {
			t.Fatalf("user agent = %q", request.UserAgent())
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/metar") && request.URL.Query().Get("ids") == "KPBI":
			writer.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(request.URL.Path, "/airport"):
			_, _ = writer.Write([]byte(`[{"icaoId":"KPBI","lat":26.6832,"lon":-80.0956}]`))
		case strings.HasSuffix(request.URL.Path, "/metar") && request.URL.Query().Get("bbox") != "":
			_, _ = writer.Write([]byte(`[{"icaoId":"KDJT","rawOb":"KDJT 261153Z 00000KT 1SM OVC002 00/00 A2900","reportTime":"2026-08-26T11:53:00Z","temp":28.4,"dewp":22.1,"wdir":140,"wspd":8,"wgst":15,"visib":"10+","altim":1017.0,"wxString":"-RA BR","clouds":[{"cover":"SCT","base":4000}],"fltCat":"VFR","lat":26.6832,"lon":-80.0956}]`))
		case strings.HasSuffix(request.URL.Path, "/taf") && request.URL.Query().Get("ids") == "KDJT":
			_, _ = writer.Write([]byte(`[{"rawTAF":"KDJT 261120Z 2612/2712 14010KT P6SM SCT040"}]`))
		default:
			t.Fatalf("unexpected weather request %s?%s", request.URL.Path, request.URL.RawQuery)
		}
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	client.now = func() time.Time { return now }

	observation, err := client.Lookup(context.Background(), "KPBI")
	if err != nil {
		t.Fatal(err)
	}
	if observation.RequestedICAO != "KPBI" || observation.ReportingICAO != "KDJT" || observation.StationStatus != stationStatusAlias || !observation.HasStationDistance || observation.StationDistanceNM > 0.01 {
		t.Fatalf("station resolution = %+v", observation)
	}
	if observation.TemperatureC != 28 || observation.DewpointC != 22 || observation.WindDirectionDegrees != 140 || observation.WindSpeedKts != 8 || observation.WindGustKts != 15 {
		t.Fatalf("typed conditions = %+v", observation)
	}
	if observation.VisibilitySM != 10 || !observation.VisibilityAtLeast || observation.FlightCategory != "VFR" || len(observation.Clouds) != 1 || observation.Clouds[0].BaseFeet != 4000 {
		t.Fatalf("typed visibility/clouds = %+v", observation)
	}
	if observation.AltimeterInHg < 30.03 || observation.AltimeterInHg > 30.04 || observation.ObservedAt.IsZero() || observation.TAFStatus != "available" {
		t.Fatalf("typed pressure/time/taf = %+v", observation)
	}
	if len(requests) != 4 {
		t.Fatalf("requests = %v", requests)
	}
}

func TestLookupTreatsNoContentAsNoWeather(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	observation, err := client.Lookup(context.Background(), "KZZZ")
	if err != nil {
		t.Fatal(err)
	}
	if observation.METARStatus != "not-found" || observation.TAFStatus != "not-found" || observation.StationStatus != stationStatusUnavailable {
		t.Fatalf("no-content observation = %+v", observation)
	}
}

func TestLookupAtUsesApprovedReportingStationWithoutChangingAirport(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/metar") && request.URL.Query().Get("ids") == "KDJT":
			_, _ = writer.Write([]byte(`[{"icaoId":"KDJT","rawOb":"KDJT 261153Z 14008KT 10SM CLR 28/22 A3001","reportTime":"2026-08-26T11:53:00Z","lat":26.6832,"lon":-80.0956}]`))
		case strings.HasSuffix(request.URL.Path, "/airport") && request.URL.Query().Get("ids") == "KPBI":
			_, _ = writer.Write([]byte(`[{"icaoId":"KPBI","lat":26.6832,"lon":-80.0956}]`))
		case strings.HasSuffix(request.URL.Path, "/taf") && request.URL.Query().Get("ids") == "KDJT":
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s?%s", request.URL.Path, request.URL.RawQuery)
		}
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	client.now = func() time.Time { return now }

	observation, err := client.LookupAt(context.Background(), "KPBI", "KDJT")
	if err != nil {
		t.Fatal(err)
	}
	if observation.RequestedICAO != "KPBI" || observation.ReportingICAO != "KDJT" || observation.StationStatus != stationStatusAlias || observation.TAFStatus != "not-found" {
		t.Fatalf("override observation = %+v", observation)
	}
}

func TestLookupCancellationDoesNotCancelSharedFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/metar") {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			_, _ = writer.Write([]byte(`[{"icaoId":"KAAA","rawOb":"KAAA 261153Z 00000KT 10SM CLR 20/10 A3000"}]`))
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	client.base, _ = url.Parse(server.URL)
	firstContext, cancel := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := client.Lookup(firstContext, "KAAA")
		firstResult <- err
	}()
	<-started
	cancel()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("first lookup error = %v", err)
	}
	secondResult := make(chan error, 1)
	go func() {
		_, err := client.Lookup(context.Background(), "KAAA")
		secondResult <- err
	}()
	close(release)
	if err := <-secondResult; err != nil {
		t.Fatalf("shared lookup error = %v", err)
	}
}
