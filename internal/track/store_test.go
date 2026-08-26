package track

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func TestStoreSamplesBoundsAndSummarizes(t *testing.T) {
	store := NewStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	store.clock = func() time.Time { return now }
	for index := 0; index < 200; index++ {
		observedAt := now.Add(time.Duration(index) * 5 * time.Second)
		store.Observe(&domain.Snapshot{PublishedAt: observedAt, Aircraft: []domain.Aircraft{{
			ICAO: "ABC123", HasDistance: true, DistanceNM: 50 - float64(index)/10, BearingDegrees: 45,
			HasAltitude: true, AltitudeFeet: 10_000 + index*10,
		}}})
	}
	store.clock = func() time.Time { return now.Add(200 * 5 * time.Second) }
	summary, err := store.Summary("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Points != DefaultMaxPoints || !summary.HasClosestApproach || !summary.HasAltitudeChange || summary.AltitudeChangeFeet <= 0 || summary.Direction == "" {
		t.Fatalf("summary = %+v", summary)
	}
	data, plotted, err := store.Plot("ABC123")
	if err != nil {
		t.Fatal(err)
	}
	if plotted.Points != DefaultMaxPoints || len(data) == 0 {
		t.Fatalf("plot summary=%+v bytes=%d", plotted, len(data))
	}
	imageValue, err := png.Decode(bytes.NewReader(data))
	if err != nil || imageValue.Bounds().Dx() != plotSize || imageValue.Bounds().Dy() != plotSize {
		t.Fatalf("invalid PNG: %v bounds=%v", err, imageValue.Bounds())
	}
}

func TestStoreUniqueICAOChurnRemainsBoundedAndExpires(t *testing.T) {
	store := NewStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	store.clock = func() time.Time { return now }
	aircraft := make([]domain.Aircraft, 0, DefaultMaxAircraft+500)
	for index := 0; index < DefaultMaxAircraft+500; index++ {
		aircraft = append(aircraft, domain.Aircraft{ICAO: icaoFor(index), HasDistance: true, DistanceNM: 10})
	}
	store.Observe(&domain.Snapshot{PublishedAt: now, Aircraft: aircraft})
	if got := store.Len(); got != DefaultMaxAircraft {
		t.Fatalf("track count = %d", got)
	}
	now = now.Add(DefaultRetention + time.Second)
	if got := store.Len(); got != 0 {
		t.Fatalf("expired track count = %d", got)
	}
}

func icaoFor(value int) string {
	const hex = "0123456789ABCDEF"
	result := []byte("000000")
	for index := len(result) - 1; index >= 0; index-- {
		result[index] = hex[value&15]
		value >>= 4
	}
	return string(result)
}
