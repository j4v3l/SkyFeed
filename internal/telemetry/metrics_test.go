package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestMetricsUseOnlyFixedLowCardinalityLabels(t *testing.T) {
	metrics := NewMetrics(time.Unix(1_700_000_000, 0))
	metrics.SetProviderCapabilities(domain.ProviderReadsb, domain.CapabilitiesOf(
		domain.CapabilityAircraft,
		domain.CapabilityReceiver,
		domain.CapabilityStatistics,
	))
	metrics.SetProviderCapabilities(domain.ProviderAirplanesLive, domain.CapabilitiesOf(domain.CapabilityAircraft))
	metrics.SetActiveAircraftProvider(domain.ProviderAirplanesLive)
	metrics.SetSourceHealth(domain.Health{
		Aircraft: domain.SourceHealth{Provider: domain.ProviderAirplanesLive, Status: domain.HealthHealthy},
		Receiver: domain.SourceHealth{Provider: domain.ProviderReadsb, Status: domain.HealthHealthy},
		Stats:    domain.SourceHealth{Provider: domain.ProviderReadsb, Status: domain.HealthDisabled},
	})
	metrics.ObserveSource(domain.ProviderReadsb, domain.CapabilityAircraft, 5*time.Millisecond, 1024, true, time.Unix(1_700_000_001, 0))
	metrics.ObserveSource(domain.ProviderID("unbounded-provider"), domain.CapabilityAircraft, time.Second, 1, false, time.Now())
	metrics.ObserveSnapshot(42, time.Second)
	metrics.SetRouteEnrichment(3, 1, 4, 0, 0, 0, 2, 5, 2)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{
		"skyfeed_source_requests_total{provider=\"readsb\",capability=\"aircraft\"} 1",
		"skyfeed_source_capability_supported{provider=\"airplanes-live\",capability=\"receiver\"} 0",
		"skyfeed_aircraft_provider_active{provider=\"airplanes-live\"} 1",
		"skyfeed_source_health{provider=\"airplanes-live\",capability=\"aircraft\"} 1",
		"skyfeed_adsblol_cache_total{result=\"hit\"} 3",
		"skyfeed_adsblol_cache_entries{kind=\"route\"} 5",
		"skyfeed_snapshot_aircraft 42",
		"go_goroutines",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics", expected)
		}
	}
	for _, forbidden := range []string{"unbounded-provider", "icao=", "callsign=", "guild=", "user="} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("high-cardinality label %q found", forbidden)
		}
	}
}
