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
			HasPosition: true, Latitude: 26 + float64(index)/10_000, Longitude: -80 + float64(index)/10_000,
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

func TestTrackUsesAbsolutePositionAcrossFeederSwitches(t *testing.T) {
	store := NewStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	store.clock = func() time.Time { return now.Add(15 * time.Second) }
	for index, sample := range []domain.Aircraft{
		{ICAO: "ABC123", HasDistance: true, DistanceNM: 2, BearingDegrees: 20, HasPosition: true, Latitude: 26.00, Longitude: -80.00},
		{ICAO: "ABC123", HasDistance: true, DistanceNM: 80, BearingDegrees: 250, HasPosition: true, Latitude: 26.01, Longitude: -79.99},
		{ICAO: "ABC123", HasDistance: true, DistanceNM: 3, BearingDegrees: 40, HasPosition: true, Latitude: 26.02, Longitude: -79.98},
	} {
		store.Observe(&domain.Snapshot{PublishedAt: now.Add(time.Duration(index) * 5 * time.Second), Aircraft: []domain.Aircraft{sample}})
	}
	points, err := store.points("ABC123")
	if err != nil {
		t.Fatal(err)
	}
	coordinates := plotCoordinates(points, plotSize/2)
	if len(coordinates) != 3 || coordinates[0].X >= coordinates[1].X || coordinates[1].X >= coordinates[2].X {
		t.Fatalf("absolute track should progress smoothly east: %#v", coordinates)
	}
	summary, err := store.Summary("ABC123")
	if err != nil || summary.Direction != "tracking NE" {
		t.Fatalf("summary=%+v err=%v", summary, err)
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
