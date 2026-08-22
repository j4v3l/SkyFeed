package readsb

import (
	"encoding/json"
	"testing"
)

func FuzzAircraftJSONDecoder(f *testing.F) {
	f.Add([]byte(`{"now":1700000000,"messages":1,"aircraft":[{"hex":"abc123","flight":"sky1"}]}`))
	f.Add([]byte(`{"aircraft":[]}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 1<<20 {
			t.Skip()
		}
		var response aircraftResponse
		if json.Unmarshal(payload, &response) == nil {
			_ = normalizeAircraft(response)
		}
	})
}
