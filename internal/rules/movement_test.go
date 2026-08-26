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
	monitor := NewMovementMonitor(MovementConfig{AirportCode: "KXYZ", Latitude: 0, Longitude: 0, HasCenter: true})
	now := time.Unix(1_700_000_000, 0)
	ground := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY1", Provider: domain.ProviderReadsb, OnGround: true, HasPosition: true, Latitude: 0.001, Longitude: 0}
	air := domain.Aircraft{ICAO: "ABC123", Callsign: "SKY1", Provider: domain.ProviderReadsb, OnGround: false, HasPosition: true, Latitude: 0.002, Longitude: 0, HasAltitude: true, AltitudeFeet: 400, HasVerticalRate: true, VerticalRateFPM: 900, HasTrack: true, TrackDegrees: 0, HasGroundSpeed: true, GroundSpeedKts: 120}
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{ground}})
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(time.Second), Aircraft: []domain.Aircraft{air}})
	air.Latitude = 0.003
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(2 * time.Second), Aircraft: []domain.Aircraft{air}})
	air.Latitude = 0.004
	takeoff := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(3 * time.Second), Aircraft: []domain.Aircraft{air}})
	if len(takeoff) != 1 || takeoff[0].Type != domain.RuleTakeoff {
		t.Fatalf("takeoff=%+v", takeoff)
	}
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(4 * time.Second), Aircraft: []domain.Aircraft{ground}})
	_ = monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(5 * time.Second), Aircraft: []domain.Aircraft{ground}})
	landing := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(6 * time.Second), Aircraft: []domain.Aircraft{ground}})
	if len(landing) != 1 || landing[0].Type != domain.RuleLanding {
		t.Fatalf("landing=%+v", landing)
	}
	again := monitor.Evaluate(9, &domain.Snapshot{PublishedAt: now.Add(7 * time.Second), Aircraft: []domain.Aircraft{air}})
	if len(again) != 0 {
		t.Fatalf("takeoff cooldown=%+v", again)
	}
}

func TestMovementMonitorRequiresConfiguredAirportCenter(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{})
	now := time.Unix(1_700_000_000, 0)
	ground := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, OnGround: true, HasPosition: true}
	air := domain.Aircraft{ICAO: "ABC123", Provider: domain.ProviderReadsb, HasPosition: true, HasAltitude: true, AltitudeFeet: 500, HasVerticalRate: true, VerticalRateFPM: 900}
	_ = monitor.Evaluate(1, &domain.Snapshot{PublishedAt: now, Aircraft: []domain.Aircraft{ground}})
	for sample := 1; sample <= 4; sample++ {
		if alerts := monitor.Evaluate(1, &domain.Snapshot{PublishedAt: now.Add(time.Duration(sample) * time.Second), Aircraft: []domain.Aircraft{air}}); len(alerts) != 0 {
			t.Fatalf("unconfigured movement alerts = %+v", alerts)
		}
	}
	if monitor.Activity().Configured {
		t.Fatal("unconfigured activity reported as configured")
	}
}

func TestMovementMonitorApproachUsesPublicCenter(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{AirportCode: "KPBI", Latitude: 26.683, Longitude: -80.096, HasCenter: true})
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
	_ = monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(time.Second), Aircraft: []domain.Aircraft{near}})
	_ = monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(2 * time.Second), Aircraft: []domain.Aircraft{near}})
	alerts := monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(3 * time.Second), Aircraft: []domain.Aircraft{near}})
	if len(alerts) != 1 || alerts[0].Type != domain.RuleApproach {
		t.Fatalf("approach=%+v", alerts)
	}
	activity := monitor.Activity()
	if !activity.Configured || activity.AirportCode != "KPBI" || len(activity.Movements) != 1 || activity.Movements[0].Phase != domain.MovementApproach {
		t.Fatalf("activity=%+v", activity)
	}
	again := monitor.Evaluate(3, &domain.Snapshot{PublishedAt: now.Add(4 * time.Second), Aircraft: []domain.Aircraft{near}})
	if len(again) != 0 {
		t.Fatalf("rearmed too soon=%+v", again)
	}
}

func TestMovementMonitorRejectsAircraftMovingAwayFromAirport(t *testing.T) {
	monitor := NewMovementMonitor(MovementConfig{AirportCode: "KXYZ", Latitude: 0, Longitude: 0, HasCenter: true})
	now := time.Unix(1_700_000_000, 0)
	for sample := 0; sample < 5; sample++ {
		aircraft := domain.Aircraft{
			ICAO: "DEF456", Provider: domain.ProviderReadsb, HasPosition: true, Latitude: 0.03 + float64(sample)*0.01,
			HasAltitude: true, AltitudeFeet: 2000 - sample*100, HasVerticalRate: true, VerticalRateFPM: -600,
			HasTrack: true, TrackDegrees: 0, HasGroundSpeed: true, GroundSpeedKts: 120,
		}
		if alerts := monitor.Evaluate(2, &domain.Snapshot{PublishedAt: now.Add(time.Duration(sample) * time.Second), Aircraft: []domain.Aircraft{aircraft}}); len(alerts) != 0 {
			t.Fatalf("moving-away alerts = %+v", alerts)
		}
	}
}
