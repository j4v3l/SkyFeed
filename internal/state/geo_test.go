package state

import (
	"math"
	"testing"
)

func TestDistanceBearing(t *testing.T) {
	distance, bearing := DistanceBearing(40, -75, 41, -75)
	if math.Abs(distance-60.04) > 0.1 {
		t.Fatalf("distance = %.3f NM", distance)
	}
	if math.Abs(bearing) > 0.01 {
		t.Fatalf("bearing = %.3f degrees", bearing)
	}
}

func BenchmarkDistanceBearing(b *testing.B) {
	for range b.N {
		_, _ = DistanceBearing(40, -75, 41, -74)
	}
}
