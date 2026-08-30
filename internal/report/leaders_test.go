package report

import (
	"fmt"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestSelectLiveLeadersFiltersAndBreaksTies(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	snapshot := &domain.Snapshot{PublishedAt: now.Add(-time.Second), Aircraft: []domain.Aircraft{
		{ICAO: "BBBBBB", HasGroundSpeed: true, GroundSpeedKts: 500, HasAltitude: true, AltitudeFeet: 30_000, Seen: 2 * time.Second},
		{ICAO: "AAAAAA", HasGroundSpeed: true, GroundSpeedKts: 500, HasAltitude: true, AltitudeFeet: 40_000, Seen: time.Second},
		{ICAO: "CCCCCC", HasGroundSpeed: true, GroundSpeedKts: 120, HasAltitude: true, AltitudeFeet: 2_000},
		{ICAO: "GROUND", OnGround: true, HasGroundSpeed: true, GroundSpeedKts: 1, HasAltitude: true, AltitudeFeet: 0},
		{ICAO: "STALE1", HasGroundSpeed: true, GroundSpeedKts: 700, HasAltitude: true, AltitudeFeet: 50_000, Seen: 20 * time.Second},
	}}
	leaders := SelectLiveLeaders(snapshot, now)
	if leaders.Eligible != 3 || leaders.Fastest.Aircraft.ICAO != "AAAAAA" || leaders.Slowest.Aircraft.ICAO != "CCCCCC" || leaders.Highest.Aircraft.ICAO != "AAAAAA" || leaders.Lowest.Aircraft.ICAO != "CCCCCC" {
		t.Fatalf("unexpected leaders: %+v", leaders)
	}
}

func BenchmarkSelectLiveLeaders100000(b *testing.B) {
	now := time.Now().UTC()
	aircraft := make([]domain.Aircraft, 100_000)
	for i := range aircraft {
		aircraft[i] = domain.Aircraft{
			ICAO: fmt.Sprintf("%06X", i), HasGroundSpeed: true, GroundSpeedKts: float64(100 + i%500),
			HasAltitude: true, AltitudeFeet: 1_000 + i%45_000, Seen: time.Duration(i%10) * time.Millisecond,
		}
	}
	snapshot := &domain.Snapshot{PublishedAt: now, Aircraft: aircraft}
	b.ReportAllocs()
	for b.Loop() {
		result := SelectLiveLeaders(snapshot, now)
		if !result.Fastest.Found {
			b.Fatal("missing leader")
		}
	}
}
