package report

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestAggregatorUsesDeltasAndDoesNotRepeatEmergency(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	aggregator := NewAggregator()
	snapshot := &domain.Snapshot{PublishedAt: now, ReceiverMessages: 100, Aircraft: []domain.Aircraft{{ICAO: "ABC123", Squawk: "7700"}}, ByICAO: map[string]int{"ABC123": 0}}
	first := aggregator.Observe(1, snapshot)
	if first.Messages != 0 || first.Emergencies != 1 {
		t.Fatalf("first=%+v", first)
	}
	snapshot.PublishedAt = now.Add(time.Second)
	snapshot.ReceiverMessages = 125
	second := aggregator.Observe(1, snapshot)
	if second.Messages != 25 || second.Emergencies != 0 {
		t.Fatalf("second=%+v", second)
	}
}
