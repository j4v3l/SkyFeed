package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsUseOnlyFixedLowCardinalityLabels(t *testing.T) {
	metrics := NewMetrics(time.Unix(1_700_000_000, 0))
	metrics.ObserveSource("aircraft", 5*time.Millisecond, 1024, true, time.Unix(1_700_000_001, 0))
	metrics.ObserveSnapshot(42, time.Second)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{"skyfeed_source_requests_total{source=\"aircraft\"} 1", "skyfeed_snapshot_aircraft 42", "go_goroutines"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics", expected)
		}
	}
	for _, forbidden := range []string{"icao=", "callsign=", "guild=", "user="} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("high-cardinality label %q found", forbidden)
		}
	}
}
