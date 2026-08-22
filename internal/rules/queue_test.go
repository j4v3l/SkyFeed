package rules

import (
	"context"
	"testing"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestEmergencyCapacityIsReserved(t *testing.T) {
	queue := NewQueue(1, 1)
	if err := queue.Enqueue(context.Background(), domain.Alert{AircraftICAO: "A", ConditionFingerprint: "normal"}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), domain.Alert{AircraftICAO: "B", ConditionFingerprint: "normal2"}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), domain.Alert{AircraftICAO: "E", Priority: domain.AlertEmergency}); err != nil {
		t.Fatal(err)
	}
	emergency, normal := queue.Depth()
	if emergency != 1 || normal != 1 || queue.Dropped() != 1 {
		t.Fatalf("depth=%d/%d dropped=%d", emergency, normal, queue.Dropped())
	}
}

func TestNormalDuplicatesCoalesce(t *testing.T) {
	queue := NewQueue(1, 2)
	alert := domain.Alert{AircraftICAO: "A", ConditionFingerprint: "rule"}
	if err := queue.Enqueue(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if queue.Coalesced() != 1 {
		t.Fatalf("coalesced=%d", queue.Coalesced())
	}
}
