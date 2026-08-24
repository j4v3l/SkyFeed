package rules

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestMovementMonitorIgnoresAirplanesLive(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{})
	now := time.Unix(1_700_000_000, 0)
	ground := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderAirplanesLive, OnGround: true}
	air := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderAirplanesLive, OnGround: false, HasVerticalRate: true, VerticalRateFPM: 800}
	_ = monitor.Evaluate(1, &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{ground}})
	alerts := monitor.Evaluate(1, &domain.Snapshot{PublishedAt: now.Add(time.Second), Aircraft: []domain.Aircraft{air}})
	if len(alerts) != 0 {
		t.Fatalf("alerts=%d", len(alerts))
	}
}

func TestMovementMonitorTakeoffAndLandingCooldown(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{})
	now := time.Unix(1_700_000_000, 0)
	ground := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, OnGround: true}
	air := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, OnGround: false, HasAltitude: true, AltitudeFeet: 400, HasVerticalRate: true, VerticalRateFPM: 900}
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{ground}})
	takeoff := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(time.Second), Aircraft: []domain.Aircraft{air}})
	if len(takeoff) != 1 || takeoff[0].Type != domain.RuleTakeoff {
		t.Fatalf("takeoff=%+v", takeoff)
	}
	landing := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(2 * time.Second), Aircraft: []domain.Aircraft{ground}})
	if len(landing) != 1 || landing[0].Type != domain.RuleLanding {
		t.Fatalf("landing=%+v", landing)
	}
	again := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(3 * time.Second), Aircraft: []domain.Aircraft{air}})
	if len(again) != 0 {
		t.Fatalf("takeoff cooldown=%+v", again)
	}
}

func TestMovementMonitorApproachUsesPublicCenter(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{Latitude: 26.683, Longitude: -80.096, HasCenter: true})
	now := time.Unix(1_700_000_000, 0)
	far := domain.Aircraft{
		ICAO: "DEF456", Provider: domain.ProviderReadsb, HasPosition: true, Latitude: 26.9, Longitude: -80.1,
		HasAltitude: true, AltitudeFeet: 2000, HasVerticalRate: true, VerticalRateFPM: -600, HasTrack: true, TrackDegrees: 180,
	}
	near := far
	near.Latitude, near.Longitude = 26.70, -80.10
	near.TrackDegrees = 190
	if alerts := monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{far}}); len(alerts) != 0 {
		t.Fatalf("far=%+v", alerts)
	}
	alerts := monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(time.Second), Aircraft: []domain.Aircraft{near}})
	if len(alerts) != 1 || alerts[0].Type != domain.RuleApproach {
		t.Fatalf("approach=%+v", alerts)
	}
	again := monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(2 * time.Second), Aircraft: []domain.Aircraft{near}})
	if len(again) != 0 {
		t.Fatalf("rearmed too soon=%+v", again)
	}
}
