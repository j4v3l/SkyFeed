package planealert

import "testing"

func TestHighInterestTags(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   bool
	}{
		{name: "GTMO detention", record: Record{Tag1: "Guantanamo", Tag2: "GTMO", Tag3: "Offshore Detention"}, want: true},
		{name: "ICE deportation", record: Record{Tag1: "Guantanamo", Tag2: "ICE", Tag3: "Deportation Flight"}, want: true},
		{name: "rendition", record: Record{Category: "Extraordinary rendition"}, want: true},
		{name: "detainee transport", record: Record{Type: "Detainee Transport"}, want: true},
		{name: "police is not ICE", record: Record{Group: "Police", Tag1: "Surveillance"}, want: false},
		{name: "police aircraft is not ICE Air", record: Record{Type: "Police Aircraft"}, want: false},
		{name: "ordinary military", record: Record{Group: "Mil", Operator: "USAF", Tag1: "Heavy"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.record.HighInterest(); got != test.want {
				t.Fatalf("HighInterest() = %t, want %t", got, test.want)
			}
		})
	}
}
