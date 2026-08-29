package enrichment

import (
	"fmt"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestDiscoveryTrackerAdmitsNewAndChangedCallsigns(t *testing.T) {
	tracker := NewDiscoveryTracker()
	now := time.Unix(1_700_000_000, 0)
	admitted := make([]string, 0, 2)
	admit := func(icao, callsign string) { admitted = append(admitted, icao+":"+callsign) }
	tracker.Observe([]domain.Aircraft{{ICAO: "abc123", Callsign: " sky1 "}}, now, admit)
	tracker.Observe([]domain.Aircraft{{ICAO: "ABC123", Callsign: "SKY1"}}, now.Add(time.Second), admit)
	tracker.Observe([]domain.Aircraft{{ICAO: "ABC123", Callsign: "SKY2"}}, now.Add(2*time.Second), admit)
	if len(admitted) != 2 || admitted[0] != "ABC123:SKY1" || admitted[1] != "ABC123:SKY2" {
		t.Fatalf("admitted = %v", admitted)
	}
}

func TestDiscoveryTrackerUniqueICAOChurnIsBounded(t *testing.T) {
	tracker := NewDiscoveryTracker()
	tracker.limit = 100
	now := time.Unix(1_700_000_000, 0)
	aircraft := make([]domain.Aircraft, 1_000)
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index)}
	}
	tracker.Observe(aircraft, now, func(string, string) {})
	if got := tracker.Len(); got != 100 {
		t.Fatalf("entries = %d", got)
	}
}
