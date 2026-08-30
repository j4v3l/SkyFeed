package domain

import "testing"

func TestUnitSystemParsingAndDefault(t *testing.T) {
	for input, want := range map[string]UnitSystem{
		" imperial ": UnitsImperial,
		"AVIATION":   UnitsAviation,
		"metric":     UnitsMetric,
	} {
		got, ok := ParseUnitSystem(input)
		if !ok || got != want {
			t.Fatalf("ParseUnitSystem(%q)=(%q,%t), want %q", input, got, ok, want)
		}
	}
	if _, ok := ParseUnitSystem("nautical-ish"); ok {
		t.Fatal("invalid unit system accepted")
	}
	if got := NormalizeUnitSystem(""); got != UnitsImperial {
		t.Fatalf("empty unit default=%q", got)
	}
	if got := NormalizeUnitSystem("invalid"); got != UnitsImperial {
		t.Fatalf("invalid unit default=%q", got)
	}
}
