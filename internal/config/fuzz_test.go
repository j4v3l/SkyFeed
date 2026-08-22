package config

import (
	"testing"
	"time"
)

func FuzzDurationParsing(f *testing.F) {
	for _, seed := range []string{"1s", "250ms", "336h", "", "-1s", "invalid"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 128 {
			t.Skip()
		}
		lookup := func(key string) (string, bool) {
			if key == "DURATION" {
				return value, true
			}
			return "", false
		}
		duration, err := parseDuration(lookup, "DURATION", time.Second)
		if err == nil {
			_ = validateDuration("DURATION", duration, 0, 30*24*time.Hour)
		}
	})
}
