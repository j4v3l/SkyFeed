package domain

import (
	"strings"
	"testing"
)

func TestNormalizeFeederDisplayName(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trimmed", input: "  Palm Beach Radar  ", want: "Palm Beach Radar"},
		{name: "unicode", input: "📡 Coastal Skywatch", want: "📡 Coastal Skywatch"},
		{name: "empty", input: "  ", wantErr: true},
		{name: "control", input: "North\nRadar", wantErr: true},
		{name: "too long", input: strings.Repeat("🛰️", 81), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeFeederDisplayName(test.input)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("NormalizeFeederDisplayName(%q) = %q, %v", test.input, got, err)
			}
		})
	}
}
