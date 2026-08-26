package report

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestAggregatorUsesMessageDeltasWithoutCountingEmergencySnapshotsAsEvents(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	aggregator := NewAggregator()
	snapshot := &domain.Snapshot{
		PublishedAt:         now,
		ActiveProvider:      domain.ProviderReadsb,
		ReceiverMessages:    100,
		MessageCounterValid: true,
		Aircraft:            []domain.Aircraft{{ICAO: "ABC123", Squawk: "7700"}},
		ByICAO:              map[string]int{"ABC123": 0},
	}
	first := aggregator.Observe(1, snapshot)
	if first.Messages != 0 || first.EmergencyEvents != 0 || first.AircraftObservations != 1 || first.PeakTracked != 1 {
		t.Fatalf("first=%+v", first)
	}
	snapshot.PublishedAt = now.Add(time.Second)
	snapshot.ReceiverMessages = 125
	second := aggregator.Observe(1, snapshot)
	if second.Messages != 25 || second.EmergencyEvents != 0 {
		t.Fatalf("second=%+v", second)
	}

	event := EmergencyEvent(1, now)
	if event.EmergencyEvents != 1 || event.BucketStart != now.UTC().Truncate(time.Hour) {
		t.Fatalf("event=%+v", event)
	}
}

func TestAggregatorResetsMessageBaselineAcrossProvidersAndInvalidCounters(t *testing.T) {
	aggregator := NewAggregator()
	snapshot := &domain.Snapshot{
		PublishedAt:         time.Unix(1_700_000_000, 0),
		ActiveProvider:      domain.ProviderReadsb,
		ReceiverMessages:    100,
		MessageCounterValid: true,
	}
	_ = aggregator.Observe(1, snapshot)

	snapshot.ActiveProvider = domain.ProviderAirplanesLive
	snapshot.ReceiverMessages = 500
	if rollup := aggregator.Observe(1, snapshot); rollup.Messages != 0 {
		t.Fatalf("provider switch produced delta %d", rollup.Messages)
	}
	snapshot.ReceiverMessages = 510
	if rollup := aggregator.Observe(1, snapshot); rollup.Messages != 10 {
		t.Fatalf("same-provider delta = %d", rollup.Messages)
	}

	snapshot.MessageCounterValid = false
	snapshot.ReceiverMessages = 0
	if rollup := aggregator.Observe(1, snapshot); rollup.Messages != 0 {
		t.Fatalf("invalid counter produced delta %d", rollup.Messages)
	}
	snapshot.ActiveProvider = domain.ProviderReadsb
	snapshot.MessageCounterValid = true
	snapshot.ReceiverMessages = 125
	if rollup := aggregator.Observe(1, snapshot); rollup.Messages != 0 {
		t.Fatalf("counter recovery produced discontinuity %d", rollup.Messages)
	}
}
