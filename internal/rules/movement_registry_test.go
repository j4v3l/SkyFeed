package rules

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestMovementRegistryDoesNotMixFeederSamples(t *testing.T) {
	registry := NewMovementRegistry()
	registry.Register("east", MovementConfig{AirportCode: "KAAA", HasCenter: true})
	registry.Register("west", MovementConfig{AirportCode: "KBBB", Latitude: 40, Longitude: -100, HasCenter: true})
	now := time.Unix(1_700_000_000, 0)
	ground := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, OnGround: true, HasPosition: true}
	airborne := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, HasPosition: true, Latitude: 40.01, Longitude: -100, HasAltitude: true, AltitudeFeet: 500, HasVerticalRate: true, VerticalRateFPM: 900, HasTrack: true, TrackDegrees: 0}
	_ = registry.Evaluate(1, &domain.Snapshot{FeederID: "east", PublishedAt: now, Aircraft: []domain.Aircraft{ground}})
	for sample := 1; sample <= 4; sample++ {
		alerts := registry.Evaluate(1, &domain.Snapshot{FeederID: "west", PublishedAt: now.Add(time.Duration(sample) * time.Second), Aircraft: []domain.Aircraft{airborne}})
		if len(alerts) != 0 {
			t.Fatalf("cross-feeder movement inference = %+v", alerts)
		}
	}
	if registry.ActivityFor("east").AirportCode != "KAAA" || registry.ActivityFor("west").AirportCode != "KBBB" {
		t.Fatalf("activities were not scoped: east=%+v west=%+v", registry.ActivityFor("east"), registry.ActivityFor("west"))
	}
}
