package readsb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/source"
)

func TestNormalizeSyntheticFixtures(t *testing.T) {
	var aircraftPayload aircraftResponse
	decodeFixture(t, "aircraft.json", &aircraftPayload)
	batch := normalizeAircraft(aircraftPayload)
	if len(batch.Aircraft) != 3 {
		t.Fatalf("aircraft count = %d", len(batch.Aircraft))
	}
	if batch.Aircraft[0].ICAO != "ABC123" || batch.Aircraft[0].Callsign != "SKY123" || !batch.Aircraft[0].HasAltitude {
		t.Fatalf("unexpected normalized aircraft: %#v", batch.Aircraft[0])
	}
	if !batch.Aircraft[1].OnGround || batch.Aircraft[1].HasAltitude || batch.Aircraft[1].Emergency != "general" {
		t.Fatalf("unexpected ground aircraft: %#v", batch.Aircraft[1])
	}

	var receiverPayload receiverResponse
	decodeFixture(t, "receiver.json", &receiverPayload)
	receiver := normalizeReceiver(receiverPayload, time.Unix(1, 0))
	if !receiver.HasPosition || receiver.Refresh != time.Second {
		t.Fatalf("unexpected receiver: %#v", receiver)
	}

	var statsPayload statsResponse
	decodeFixture(t, "stats.json", &statsPayload)
	statistics := normalizeStats(statsPayload, time.Unix(2, 0))
	if statistics.Messages != 900 || statistics.MessageRate != 30 || statistics.MaxRangeNM != 100 {
		t.Fatalf("unexpected statistics: %#v", statistics)
	}
}

func TestNormalizeCurrentStatsFixture(t *testing.T) {
	var payload statsResponse
	decodeFixture(t, "stats-current.json", &payload)
	if err := validateStatsResponse(payload); err != nil {
		t.Fatalf("validate current stats: %v", err)
	}
	statistics := normalizeStats(payload, time.Unix(1787414405, 0))
	if statistics.Messages != 1800 || statistics.MessageRate != 30 || statistics.MaxRangeNM != 110 {
		t.Fatalf("unexpected current statistics: %#v", statistics)
	}
	if statistics.TrackedAircraft != 6 || statistics.SingleMessageOnly != 1 {
		t.Fatalf("unexpected current track counts: %#v", statistics)
	}
	if got := statistics.WindowEnd.Unix(); got != 1787414400 {
		t.Fatalf("window end = %d", got)
	}
}

func TestClientUsesOnlyFixedPaths(t *testing.T) {
	fixtures := map[string]string{
		"/data/aircraft.json": "aircraft.json",
		"/data/receiver.json": "receiver.json",
		"/data/stats.json":    "stats.json",
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture, ok := fixtures[request.URL.Path]
		if !ok {
			t.Errorf("unexpected path %q", request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "readsb", fixture))
		if err != nil {
			t.Errorf("read fixture: %v", err)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(data)
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL + "/data")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(baseURL, time.Second)
	if _, err := client.FetchAircraft(context.Background()); err != nil {
		t.Fatalf("aircraft: %v", err)
	}
	if _, err := client.FetchReceiver(context.Background()); err != nil {
		t.Fatalf("receiver: %v", err)
	}
	if _, err := client.FetchStats(context.Background()); err != nil {
		t.Fatalf("stats: %v", err)
	}
}

func TestClientRejectsEmptyAndInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "invalid", body: "{"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			baseURL, err := url.Parse(server.URL + "/data")
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewClient(baseURL, time.Second).FetchAircraft(context.Background())
			if err == nil || source.ClassifyError(err) != source.ErrorPayload {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestClientRejectsSemanticallyInvalidJSON(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     string
		fetch    func(*Client) error
	}{
		{
			name:     "aircraft missing array",
			endpoint: "/data/aircraft.json",
			body:     `{"now":1787414400,"messages":1}`,
			fetch: func(client *Client) error {
				_, err := client.FetchAircraft(context.Background())
				return err
			},
		},
		{
			name:     "receiver missing refresh",
			endpoint: "/data/receiver.json",
			body:     `{"version":"synthetic"}`,
			fetch: func(client *Client) error {
				_, err := client.FetchReceiver(context.Background())
				return err
			},
		},
		{
			name:     "stats missing period",
			endpoint: "/data/stats.json",
			body:     `{}`,
			fetch: func(client *Client) error {
				_, err := client.FetchStats(context.Background())
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.endpoint {
					t.Errorf("path = %q, want %q", request.URL.Path, test.endpoint)
					http.NotFound(writer, request)
					return
				}
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			baseURL, err := url.Parse(server.URL + "/data")
			if err != nil {
				t.Fatal(err)
			}
			err = test.fetch(NewClient(baseURL, time.Second))
			if err == nil || source.ClassifyError(err) != source.ErrorPayload {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func decodeFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "readsb", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}

func BenchmarkDecodeAircraftJSON(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "readsb", "aircraft.json"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		var response aircraftResponse
		if err := json.Unmarshal(data, &response); err != nil {
			b.Fatal(err)
		}
		_ = normalizeAircraft(response)
	}
}
