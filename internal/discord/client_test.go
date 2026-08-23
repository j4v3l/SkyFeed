package discord

import (
	"errors"
	"testing"

	"github.com/disgoorg/disgo/rest"
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
