package discord

import (
	"errors"
	"testing"

	"github.com/disgoorg/disgo/rest"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestBoundedNonce(t *testing.T) {
	t.Parallel()

	first := boundedNonce("skyfeed-dashboard-1540822642976751696")
	second := boundedNonce("skyfeed-dashboard-1540822642976751696")
	different := boundedNonce("skyfeed-dashboard-1540822642976751697")

	if len(first) > 25 {
		t.Fatalf("nonce length = %d, want at most 25", len(first))
	}
	if first != second {
		t.Fatal("nonce is not deterministic")
	}
	if first == different {
		t.Fatal("distinct inputs produced the same nonce")
	}
}

func TestBoundAlertDestinationFallsBackToInterestingChannel(t *testing.T) {
	bindings := []storage.ChannelBinding{{Purpose: "interesting", ChannelID: 42}}
	destination, purpose := boundAlertDestination(bindings, "high-interest")
	if destination != 42 || purpose != "interesting" {
		t.Fatalf("destination = %d/%q", destination, purpose)
	}

	bindings = append(bindings, storage.ChannelBinding{Purpose: "high-interest", ChannelID: 84})
	destination, purpose = boundAlertDestination(bindings, "high-interest")
	if destination != 84 || purpose != "high-interest" {
		t.Fatalf("dedicated destination = %d/%q", destination, purpose)
	}
}

func TestAlertDestinationRoutesPriorityInterestingAircraft(t *testing.T) {
	purpose, category := alertDestination(domain.Alert{Type: domain.RuleInteresting, InterestingPriority: true})
	if purpose != "high-interest" || category != "high-interest" {
		t.Fatalf("destination = %q/%q", purpose, category)
	}
	purpose, category = alertDestination(domain.Alert{Type: domain.RuleInteresting})
	if purpose != "interesting" || category != "interesting" {
		t.Fatalf("ordinary destination = %q/%q", purpose, category)
	}
}

func TestUnknownDiscordMessageClassification(t *testing.T) {
	if !isUnknownDiscordMessage(&rest.Error{Code: rest.JSONErrorCodeUnknownMessage}) {
		t.Fatal("unknown-message response was not recognized")
	}
	if isUnknownDiscordMessage(&rest.Error{Code: rest.JSONErrorCodeUnknownChannel}) {
		t.Fatal("unrelated Discord error was treated as an unknown message")
	}
	if isUnknownDiscordMessage(errors.New("network failure")) {
		t.Fatal("non-Discord error was treated as an unknown message")
	}
}
