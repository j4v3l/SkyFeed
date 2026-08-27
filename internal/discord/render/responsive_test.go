package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestResponsiveFactsUseSemanticBreaks(t *testing.T) {
	got := Facts("one", "two", "three", "four", "five", "six", "seven")
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	for _, line := range lines {
		if facts := strings.Count(line, " · ") + 1; facts > 3 {
			t.Fatalf("line contains %d facts: %q", facts, line)
		}
	}
}

func TestResponsiveRecordPagesDoNotLoseRecords(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	items := make([]FeederListItem, 12)
	for index := range items {
		items[index] = FeederListItem{Name: fmt.Sprintf("Feeder %02d", index+1), Area: "Approved area", State: "healthy", Aircraft: index}
	}
	seen := make(map[string]bool, len(items))
	for page := 0; page < 3; page++ {
		embed := FeedersPage(items, page, DefaultPageSize, now)
		wantFields := DefaultPageSize
		if page == 2 {
			wantFields = 2
		}
		if len(embed.Fields) != wantFields {
			t.Fatalf("page %d fields = %d, want %d", page+1, len(embed.Fields), wantFields)
		}
		for _, field := range embed.Fields {
			for _, item := range items {
				if strings.Contains(field.Name, item.Name) {
					if seen[item.Name] {
						t.Fatalf("duplicate record %q", item.Name)
					}
					seen[item.Name] = true
				}
			}
		}
	}
	if len(seen) != len(items) {
		t.Fatalf("rendered %d of %d records", len(seen), len(items))
	}
}

func TestBoundEmbedAlwaysUsesFullWidthFields(t *testing.T) {
	inline := true
	embed := BoundEmbed(discord.Embed{Fields: []discord.EmbedField{{Name: "A", Value: "B", Inline: &inline}}})
	if len(embed.Fields) != 1 || embed.Fields[0].Inline == nil || *embed.Fields[0].Inline {
		t.Fatalf("field is not explicitly full width: %#v", embed.Fields)
	}
}

func TestResponsivePresentationSnapshotsSerialize(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	snapshot := &domain.Snapshot{
		PublishedAt: now, FetchedAt: now.Add(-2 * time.Second), ActiveProvider: domain.ProviderReadsb,
		Aircraft: []domain.Aircraft{{ICAO: "ABC123", Callsign: "SKY123", Provider: domain.ProviderReadsb}},
		Health:   domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy, LastSuccess: now}},
	}
	views := map[string]discord.Embed{
		"aircraft": AircraftSummary(snapshot.Aircraft[0], snapshot, domain.UnitsAviation, now),
		"status":   StatusWithUnits(snapshot, time.Hour, now, true, domain.UnitsAviation),
		"airport": AirportDashboard(domain.Airport{ICAO: "KJFK", Name: "John F. Kennedy International"}, WeatherView{
			RequestedICAO: "KJFK", ReportingICAO: "KJFK", METARStatus: "available", FlightCategory: "VFR", FetchedAt: now,
		}, domain.AirportActivity{AirportCode: "KJFK", Configured: true}, "", now, domain.UnitsAviation),
		"alerts":     AlertConfigsPage([]storage.AlertConfig{{Category: "emergency", Enabled: true, Cooldown: time.Minute}}, 0, DefaultPageSize, now),
		"report":     ReportWithUnits(storage.ReportSummary{From: now.Add(-time.Hour), To: now, AircraftObservations: 9, PeakTracked: 3}, domain.UnitsAviation),
		"feeders":    FeedersPage([]FeederListItem{{Name: "Home radar", Area: "Palm Beach", State: "healthy", Aircraft: 3}}, 0, DefaultPageSize, now),
		"moderation": ModerationHistoryPage([]storage.ModerationCase{{ID: 7, Action: "timeout", Status: "succeeded", Reason: "Spam", CreatedAt: now}}, 0, DefaultPageSize, now),
		"help":       Help(now, true),
		"audit": SystemAudit(SystemAuditData{
			GeneratedAt: now, OverallStatus: "healthy", Live: true, Ready: true, ActiveProvider: "readsb",
		}),
	}
	for name, embed := range views {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(embed)
			if err != nil {
				t.Fatal(err)
			}
			serialized := string(payload)
			if !strings.Contains(serialized, `"title":"SkyFeed • `) {
				t.Fatalf("serialized snapshot has no SkyFeed hierarchy: %s", serialized)
			}
			if len(payload) > 32_000 {
				t.Fatalf("serialized payload is unexpectedly large: %d bytes", len(payload))
			}
			for _, field := range embed.Fields {
				if field.Inline == nil || *field.Inline {
					t.Fatalf("snapshot field %q is not full width", field.Name)
				}
			}
		})
	}
}
