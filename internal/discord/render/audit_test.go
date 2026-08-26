package render

import (
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestSystemAuditAndAdminDigestEmbeds(t *testing.T) {
	data := SystemAuditData{
		GeneratedAt:    time.Unix(1_700_000_000, 0).UTC(),
		Uptime:         2 * time.Hour,
		OverallStatus:  "healthy",
		Live:           true,
		Ready:          true,
		AircraftCount:  12,
		ActiveProvider: "readsb",
		MessageRate:    80.5,
		MaxRangeNM:     90,
		Components: []AuditComponent{
			{Name: "discord", Status: "healthy", Message: "Gateway ready"},
			{Name: "database", Status: "healthy", Message: "SQLite initialized"},
		},
		Channels:          []string{"admin → `99`"},
		Roles:             []string{"admin → `88`"},
		WatchRules:        3,
		AlertConfigs:      2,
		ReportSchedules:   1,
		Report24h:         storage.ReportSummary{AircraftObservations: 100, PeakTracked: 40, Messages: 1000, MaximumRangeNM: 95, EmergencyEvents: 1},
		RouteCatalog:      20,
		RouteSightings24h: 15,
		ADSBDBEnabled:     true,
		AdsbLolEnabled:    true,
		AdsbLolRouteCache: 10,
		AdsbLolBatches:    4,
	}
	audit := SystemAudit(data)
	if !strings.Contains(audit.Title, "System audit") {
		t.Fatalf("title=%q", audit.Title)
	}
	if len(audit.Fields) < 5 {
		t.Fatalf("fields=%d", len(audit.Fields))
	}
	digest := AdminDigest(data, 6*time.Hour)
	if !strings.Contains(digest.Title, "Admin digest") {
		t.Fatalf("digest title=%q", digest.Title)
	}
	if digest.Footer == nil || !strings.Contains(digest.Footer.Text, "/audit") {
		t.Fatalf("digest footer=%v", digest.Footer)
	}
}
