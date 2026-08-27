package rules

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/planealert"
)

func TestInterestingMonitorFiresOncePerICAO(t *testing.T) {
	monitor := NewInterestingMonitor(func(icao string) (planealert.Record, bool) {
		if icao == "AE1234" {
			return planealert.Record{ICAO: "AE1234", Group: "Mil", Operator: "USAF", Tag1: "Heavy"}, true
		}
		return planealert.Record{}, false
	})
	now := time.Now()
	snapshot := &domain.Snapshot{
		PublishedAt: now,
		Aircraft: []domain.Aircraft{
			{ICAO: "AE1234", Provider: domain.ProviderReadsb, Callsign: "RCH123", DistanceNM: 12.5, HasDistance: true},
			{ICAO: "ABCDEF", Provider: domain.ProviderReadsb, Callsign: "UAL1"},
		},
	}
	alerts := monitor.Evaluate(42, snapshot)
	if len(alerts) != 1 {
		t.Fatalf("alerts=%d", len(alerts))
	}
	if alerts[0].Type != domain.RuleInteresting || alerts[0].InterestingGroup != "Mil" {
		t.Fatalf("alert=%+v", alerts[0])
	}
	if repeat := monitor.Evaluate(42, snapshot); len(repeat) != 0 {
		t.Fatalf("repeat alerts=%d", len(repeat))
	}
}

func TestInterestingMonitorRestoreSkipsSeen(t *testing.T) {
	monitor := NewInterestingMonitor(func(icao string) (planealert.Record, bool) {
		return planealert.Record{ICAO: icao, Group: "Gov"}, true
	})
	monitor.Restore([]string{"AE1234"})
	snapshot := &domain.Snapshot{
		PublishedAt: time.Now(),
		Aircraft:    []domain.Aircraft{{ICAO: "AE1234", Provider: domain.ProviderReadsb}},
	}
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 0 {
		t.Fatalf("alerts=%d", len(alerts))
	}
}

func TestInterestingMonitorIgnoresNonReadsbProvider(t *testing.T) {
	monitor := NewInterestingMonitor(func(icao string) (planealert.Record, bool) {
		return planealert.Record{ICAO: icao, Group: "Mil"}, true
	})
	snapshot := &domain.Snapshot{
		PublishedAt: time.Now(),
		Aircraft: []domain.Aircraft{
			{ICAO: "AE1234", Provider: domain.ProviderAirplanesLive},
		},
	}
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 0 {
		t.Fatalf("alerts=%d", len(alerts))
	}
}

func TestInterestingMonitorMarksPriorityTags(t *testing.T) {
	monitor := NewInterestingMonitor(func(string) (planealert.Record, bool) {
		return planealert.Record{ICAO: "AE9999", Tag1: "Guantanamo", Tag2: "ICE", Tag3: "Deportation Flight"}, true
	})
	alerts := monitor.Evaluate(1, &domain.Snapshot{
		PublishedAt: time.Now(), Aircraft: []domain.Aircraft{{ICAO: "AE9999", Provider: domain.ProviderReadsb}},
	})
	if len(alerts) != 1 || !alerts[0].InterestingPriority {
		t.Fatalf("priority alert = %#v", alerts)
	}
}
