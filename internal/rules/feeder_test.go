package rules

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestFeederMonitorDeduplicatesAndRequiresStableRecovery(t *testing.T) {
	monitor := NewFeederMonitor()
	snapshot := &domain.Snapshot{PublishedAt: time.Now(), Health: domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthOffline}}}
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 1 || alerts[0].Priority != domain.AlertEmergency {
		t.Fatalf("offline alerts=%+v", alerts)
	}
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 0 {
		t.Fatalf("duplicate alerts=%+v", alerts)
	}
	snapshot.Health.Aircraft.Status = domain.HealthHealthy
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 0 {
		t.Fatalf("early recovery=%+v", alerts)
	}
	if alerts := monitor.Evaluate(1, snapshot); len(alerts) != 1 || alerts[0].Title != "Feeder source recovered" {
		t.Fatalf("recovery alerts=%+v", alerts)
	}
}

func TestFeederMonitorRestoresNewestDurableState(t *testing.T) {
	monitor := NewFeederMonitor()
	monitor.Restore([]string{"feeder:offline:aircraft", "feeder:recovered:aircraft"})
	snapshot := &domain.Snapshot{PublishedAt: time.Now(), Health: domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthOffline}}}
	if alerts := monitor.Evaluate(42, snapshot); len(alerts) != 0 {
		t.Fatalf("restored outage refired: %+v", alerts)
	}
}

func TestFeederMonitorIgnoresUnsupportedCapabilities(t *testing.T) {
	monitor := NewFeederMonitor()
	snapshot := &domain.Snapshot{
		PublishedAt: time.Now(),
		Health: domain.Health{
			Aircraft: domain.SourceHealth{Status: domain.HealthHealthy},
			Receiver: domain.SourceHealth{Status: domain.HealthDisabled},
			Stats:    domain.SourceHealth{Status: domain.HealthDisabled},
		},
	}
	if alerts := monitor.Evaluate(42, snapshot); len(alerts) != 0 {
		t.Fatalf("disabled capabilities generated alerts: %+v", alerts)
	}
}
