package privacy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDisclosureJSONHasOnlyPrivacySafeFields(t *testing.T) {
	disclosure := NewDisclosure(
		[]string{"readsb", "airplanes.live", "adsb.lol"},
		"KPBI",
		50,
		[]Retention{{Category: "aircraft snapshots", Period: "memory only"}},
		[]Attribution{{Provider: "adsb.lol", Notice: "ODbL"}},
	)
	encoded, err := json.Marshal(disclosure)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]struct{}, len(object))
	for key := range object {
		got[key] = struct{}{}
	}
	want := map[string]struct{}{
		"providers": {}, "public_airport_code": {}, "radius_nm": {}, "retention": {}, "attribution": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON fields = %v", got)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"latitude", "longitude", "coordinate", "base_url", "guild_id"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("disclosure exposed forbidden field %q: %s", forbidden, encoded)
		}
	}
}

func TestDisclosureOwnsItsSlices(t *testing.T) {
	providers := []string{"readsb"}
	retention := []Retention{{Category: "snapshots", Period: "memory only"}}
	disclosure := NewDisclosure(providers, "", 0, retention, nil)
	providers[0] = "changed"
	retention[0].Period = "changed"
	if disclosure.Providers[0] != "readsb" || disclosure.Retention[0].Period != "memory only" {
		t.Fatalf("constructor retained caller slices: %+v", disclosure)
	}

	clone := disclosure.Clone()
	clone.Providers[0] = "changed"
	if disclosure.Providers[0] != "readsb" {
		t.Fatal("clone shares provider storage")
	}
}
