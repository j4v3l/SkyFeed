package render

import (
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestObservationRowsPreserveMeaningWithMissingValues(t *testing.T) {
	got := alertObservationFacts(domain.AlertObservation{HasAltitude: true, AltitudeFeet: 3850, HasGroundSpeed: true, GroundSpeedKts: 100}, domain.UnitsImperial)
	if got != "Altitude 3,850 ft\nSpeed 115 mph" {
		t.Fatalf("unexpected rows: %q", got)
	}
	if got := alertObservationFacts(domain.AlertObservation{}, domain.UnitsImperial); got != "" {
		t.Fatalf("empty facts: %q", got)
	}
}

func TestFeedCardsAvoidRepeatedExplanations(t *testing.T) {
	now := time.Unix(1700000000, 0)
	alert := domain.Alert{Type: domain.RuleApproach, AircraftICAO: "ABC123", Callsign: "SKY123", ObservedAt: now, Description: "SKY123 appears to be approaching the airport.\nThree samples matched.\nThis is an inferred trend."}
	card := Alert(alert)
	if len(card.Fields) > 3 || strings.Contains(card.Description, "Three samples") {
		t.Fatalf("dense alert: %#v", card)
	}
	if !strings.Contains(embedFieldValues(card), "inferred trend") {
		t.Fatal("missing inference explanation")
	}
	alert.InterestingOperator = "Example operator"
	alert.Title = alert.InterestingOperator
	alert.InterestingGroup = "Mil"
	alert.InterestingTags = "Research • Survey"
	card = InterestingAlert(alert)
	text := card.Description + embedFieldValues(card)
	if strings.Count(text, "Example operator") != 1 || !strings.Contains(text, "Military") || len(card.Fields) > 3 {
		t.Fatalf("duplicated or dense metadata: %q", text)
	}
}

func BenchmarkRenderMovementAlert(b *testing.B) {
	alert := domain.Alert{Type: domain.RuleApproach, AircraftICAO: "ABC123", Callsign: "SKY123", ObservedAt: time.Unix(1700000000, 0), Description: "SKY123 appears to be approaching the airport.\nThree samples matched.", Observation: domain.AlertObservation{HasDistance: true, DistanceNM: 8, HasAltitude: true, AltitudeFeet: 3850, HasGroundSpeed: true, GroundSpeedKts: 160, HasVerticalRate: true, VerticalRateFPM: -500}}
	b.ReportAllocs()
	for b.Loop() {
		_ = Alert(alert)
	}
}
