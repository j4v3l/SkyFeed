package config

import "testing"

func TestInferDomesticCountryISO(t *testing.T) {
	if got := InferDomesticCountryISO("KPBI"); got != "US" {
		t.Fatalf("got %q", got)
	}
	if got := InferDomesticCountryISO("EGLL"); got != "GB" {
		t.Fatalf("got %q", got)
	}
	if got := InferDomesticCountryISO(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
