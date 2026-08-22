package discord

import "testing"

func FuzzCommandFilters(f *testing.F) {
	f.Add("distance", 10)
	f.Add("altitude", -100)
	f.Add("unknown", 1_000_000)
	f.Fuzz(func(t *testing.T, ordering string, limit int) {
		if len(ordering) > 256 {
			t.Skip()
		}
		normalized := normalizedSort(ordering)
		if !validSort(normalized) {
			t.Fatalf("invalid normalized sort %q", normalized)
		}
		bounded := boundedInt(limit, 1, 25, 10)
		if bounded < 1 || bounded > 25 {
			t.Fatalf("limit escaped bounds: %d", bounded)
		}
	})
}
