package health

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpointsTrackReadiness(t *testing.T) {
	state := NewState(time.Now())
	server := NewServer("127.0.0.1:0", state, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertStatus(t, server.Handler(), "/livez", http.StatusOK)
	assertStatus(t, server.Handler(), "/readyz", http.StatusServiceUnavailable)
	assertStatus(t, server.Handler(), "/healthz", http.StatusServiceUnavailable)

	state.SetReady(true)
	state.SetComponent("bootstrap", "healthy", "ready")
	assertStatus(t, server.Handler(), "/readyz", http.StatusOK)
	assertStatus(t, server.Handler(), "/healthz", http.StatusOK)
}

func TestHealthEndpointReflectsComponentFailures(t *testing.T) {
	state := NewState(time.Now())
	state.SetReady(true)
	state.SetComponent("aircraft_source", "healthy", "")
	state.SetComponent("receiver_source", "healthy", "")
	state.SetComponent("stats_source", "degraded", "timeout")
	server := NewServer("127.0.0.1:0", state, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertStatus(t, server.Handler(), "/readyz", http.StatusOK)
	assertStatus(t, server.Handler(), "/healthz", http.StatusServiceUnavailable)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !strings.Contains(response.Body.String(), `"status":"degraded"`) {
		t.Fatalf("health body = %q", response.Body.String())
	}

	state.SetComponent("stats_source", "offline", "repeated timeout")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !strings.Contains(response.Body.String(), `"status":"offline"`) {
		t.Fatalf("health body = %q", response.Body.String())
	}
}

func TestCheck(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	address := strings.TrimPrefix(upstream.URL, "http://")
	if err := Check(context.Background(), address); err != nil {
		t.Fatalf("check healthy server: %v", err)
	}
}

func TestMetricsHandlerIsOptional(t *testing.T) {
	state := NewState(time.Now())
	server := NewServer("127.0.0.1:0", state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.SetMetrics(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("metric 1\n")) }))
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "metric 1\n" {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s status = %d, want %d", path, response.Code, want)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("%s content type = %q", path, contentType)
	}
}
