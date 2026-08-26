package app

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestRunStopsAfterCancellation(t *testing.T) {
	sourceURL, err := url.Parse("http://receiver.invalid/data")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ADSB:       config.ADSB{BaseURL: sourceURL},
		HealthAddr: "127.0.0.1:0",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("application did not stop after cancellation")
	}
}

func TestSourcesInitializedRequiresSuccessfulFetches(t *testing.T) {
	now := time.Unix(1_787_414_400, 0)
	health := domain.Health{
		Aircraft: domain.SourceHealth{Status: domain.HealthDegraded},
		Receiver: domain.SourceHealth{Status: domain.HealthDegraded},
		Stats:    domain.SourceHealth{Status: domain.HealthDegraded},
	}
	if sourcesInitialized(health) {
		t.Fatal("startup failures incorrectly initialized the sources")
	}
	health.Aircraft.LastSuccess = now
	health.Receiver.Status = domain.HealthDisabled
	health.Stats.Status = domain.HealthDisabled
	if !sourcesInitialized(health) {
		t.Fatal("successful aircraft with disabled metadata did not initialize the sources")
	}
	health.Aircraft = domain.SourceHealth{Status: domain.HealthDisabled}
	if sourcesInitialized(health) {
		t.Fatal("disabled aircraft capability initialized the sources")
	}
}

func TestPrivacyDisclosureReflectsEnabledProvidersWithoutCoordinates(t *testing.T) {
	disclosure := privacyDisclosure(config.Config{ADSBDB: config.ADSBDB{Enabled: true}})
	if len(disclosure.Providers) != 3 ||
		disclosure.Providers[0] != "readsb" ||
		disclosure.Providers[1] != "ADSBDB" ||
		disclosure.Providers[2] != "aviationweather.gov" {
		t.Fatalf("providers = %v", disclosure.Providers)
	}
	if disclosure.PublicAirportCode != "" || disclosure.RadiusNM != 0 {
		t.Fatalf("unexpected point-query disclosure: %+v", disclosure)
	}
	if len(disclosure.Attribution) != 2 ||
		disclosure.Attribution[0].Provider != "ADSBDB" ||
		disclosure.Attribution[1].Provider != "aviationweather.gov" {
		t.Fatalf("attribution = %+v", disclosure.Attribution)
	}
}

func TestPrivacyDisclosureIncludesExplicitPublicPointProvider(t *testing.T) {
	latitude, longitude := 1.25, -2.5
	cfg := config.Config{
		ADSB: config.ADSB{ProviderOrder: []domain.ProviderID{
			domain.ProviderReadsb,
			domain.ProviderAirplanesLive,
		}},
		AirplanesLive: config.AirplanesLive{
			PublicAirportCode: "KXYZ",
			Latitude:          &latitude,
			Longitude:         &longitude,
			RadiusNM:          50,
		},
		ADSBDB: config.ADSBDB{Enabled: true},
	}
	disclosure := privacyDisclosure(cfg)
	if len(disclosure.Providers) != 4 ||
		disclosure.Providers[0] != "readsb" ||
		disclosure.Providers[1] != "airplanes.live" ||
		disclosure.Providers[2] != "ADSBDB" ||
		disclosure.Providers[3] != "aviationweather.gov" {
		t.Fatalf("providers = %v", disclosure.Providers)
	}
	if disclosure.PublicAirportCode != "KXYZ" || disclosure.RadiusNM != 50 {
		t.Fatalf("point-query disclosure = %+v", disclosure)
	}
	if len(disclosure.Attribution) != 3 || disclosure.Attribution[0].Provider != "airplanes.live" {
		t.Fatalf("attribution = %+v", disclosure.Attribution)
	}
}

func TestPrivacyDisclosureIncludesLocalAirportWithoutExternalFallback(t *testing.T) {
	latitude, longitude := 1.25, -2.5
	disclosure := privacyDisclosure(config.Config{AirplanesLive: config.AirplanesLive{
		PublicAirportCode: "KXYZ", Latitude: &latitude, Longitude: &longitude, RadiusNM: 25,
	}})
	if disclosure.PublicAirportCode != "KXYZ" || disclosure.RadiusNM != 25 {
		t.Fatalf("local airport disclosure = %+v", disclosure)
	}
	for _, provider := range disclosure.Providers {
		if provider == "airplanes.live" {
			t.Fatalf("disabled external fallback disclosed: %v", disclosure.Providers)
		}
	}
}

func TestPrivacyDisclosureNamesTransientTracksAndProviderSpecificRetention(t *testing.T) {
	disclosure := privacyDisclosure(config.Config{
		ADSBDB:     config.ADSBDB{Enabled: true, RouteEnabled: true},
		AdsbLol:    config.AdsbLol{Enabled: true},
		PlaneAlert: config.PlaneAlert{Enabled: true},
	})
	joinedProviders := strings.Join(disclosure.Providers, " ")
	if !strings.Contains(joinedProviders, "plane-alert-db") {
		t.Fatalf("enabled providers = %v", disclosure.Providers)
	}
	wording := ""
	for _, value := range disclosure.Retention {
		wording += value.Category + ": " + value.Period + "\n"
	}
	for _, expected := range []string{"track points expire after 15 minutes", "route values are never stored in SQLite", "source-labeled catalog"} {
		if !strings.Contains(wording, expected) {
			t.Fatalf("retention wording %q missing %q", wording, expected)
		}
	}
}

func TestConfigureSourcesKeepsReadsbMetadataAndOrderedAircraftFallback(t *testing.T) {
	baseURL, err := url.Parse("http://receiver.invalid/data")
	if err != nil {
		t.Fatal(err)
	}
	latitude, longitude := 1.25, -2.5
	cfg := config.Config{
		ADSB: config.ADSB{
			BaseURL: baseURL,
			ProviderOrder: []domain.ProviderID{
				domain.ProviderReadsb,
				domain.ProviderAirplanesLive,
			},
		},
		AirplanesLive: config.AirplanesLive{
			Latitude:  &latitude,
			Longitude: &longitude,
			RadiusNM:  50,
			Timeout:   time.Second,
			Poll:      time.Second,
		},
	}
	configured, err := configureSources(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if configured.Set.Aircraft.ProviderID() != domain.ProviderReadsb ||
		configured.Set.Receiver.ProviderID() != domain.ProviderReadsb ||
		configured.Set.Stats.ProviderID() != domain.ProviderReadsb {
		t.Fatalf("source set = %+v", configured.Set)
	}
	if len(configured.AircraftChecks) != 2 ||
		configured.AircraftChecks[0].ProviderID() != domain.ProviderReadsb ||
		configured.AircraftChecks[1].ProviderID() != domain.ProviderAirplanesLive {
		t.Fatalf("aircraft checks = %+v", configured.AircraftChecks)
	}
	if configured.AirplanesLive == nil ||
		configured.AirplanesLive.Capabilities().Supports(domain.CapabilityReceiver) ||
		!configured.AirplanesLive.Capabilities().Supports(domain.CapabilityAircraft) {
		t.Fatalf("airplanes.live capabilities are incorrect")
	}
}
