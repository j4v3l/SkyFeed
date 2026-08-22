package adsbdb

import (
	"encoding/json"
	"testing"
)

func FuzzADSBDBDecoder(f *testing.F) {
	f.Add([]byte(`{"response":{"aircraft":{"mode_s":"ABC123","registration":"N123SF"}}}`))
	f.Add([]byte(`{"response":{}}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 1<<20 {
			t.Skip()
		}
		var response responseDTO
		if json.Unmarshal(payload, &response) == nil {
			_ = mapResponse("ABC123", "SKY1", response.Response)
		}
	})
}
