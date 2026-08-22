package discord

import (
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestReportDue(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC) // Monday.
	tests := []struct {
		name     string
		cadence  string
		lastRun  time.Time
		expected bool
	}{
		{name: "daily due", cadence: "daily", lastRun: now.Add(-25 * time.Hour), expected: true},
		{name: "daily already sent", cadence: "daily", lastRun: now.Add(-time.Hour), expected: false},
		{name: "weekly due", cadence: "weekly", lastRun: now.AddDate(0, 0, -7), expected: true},
		{name: "weekly already sent", cadence: "weekly", lastRun: now.Add(-time.Hour), expected: false},
		{name: "unknown cadence", cadence: "hourly", expected: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reportDue(storage.ReportSchedule{Cadence: test.cadence, LastRun: test.lastRun}, now)
			if got != test.expected {
				t.Fatalf("reportDue() = %t, want %t", got, test.expected)
			}
		})
	}
}

func TestReportPeriodStartUsesMondayForWeeks(t *testing.T) {
	now := time.Date(2026, time.August, 27, 15, 30, 0, 0, time.FixedZone("local", -4*60*60))
	want := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	if got := reportPeriodStart("weekly", now); !got.Equal(want) {
		t.Fatalf("weekly start = %s, want %s", got, want)
	}
}
