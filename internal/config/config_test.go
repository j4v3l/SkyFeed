package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadWithDefaultsAndRedaction(t *testing.T) {
	environment := validEnvironment()
	cfg, err := LoadWith(mapLookup(environment), func(path string) ([]byte, error) {
		if path != "/run/secrets/discord_token" {
			t.Fatalf("read path = %q", path)
		}
		return []byte("synthetic.token.value\n"), nil
	})
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}

	if cfg.ADSB.AircraftPoll != time.Second || cfg.ADSB.MetadataPoll != 30*time.Second {
		t.Fatalf("unexpected poll defaults: %#v", cfg.ADSB)
	}
	if cfg.ADSBDB.Enabled || cfg.ADSBDB.RouteEnabled {
		t.Fatal("ADSBDB and route enrichment must default off")
	}
	if cfg.Discord.GlobalCommands {
		t.Fatal("Discord commands must default to guild scope")
	}
	if cfg.Discord.Token.Reveal() != "synthetic.token.value" {
		t.Fatal("token was not loaded or trimmed")
	}
	if got := cfg.Discord.Token.String(); got != redacted {
		t.Fatalf("formatted token = %q", got)
	}
	encoded, err := json.Marshal(cfg.Discord.Token)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	if strings.Contains(string(encoded), "synthetic") {
		t.Fatal("JSON marshaling exposed the token")
	}
}

func TestLoadWithGlobalCommandScope(t *testing.T) {
	environment := validEnvironment()
	environment["SKYFEED_DISCORD_GLOBAL_COMMANDS"] = "true"
	cfg, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discord.GlobalCommands {
		t.Fatal("global command scope was not loaded")
	}
}

func TestLoadWithRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(map[string]string)
		wantError string
	}{
		{name: "missing application", mutate: func(values map[string]string) { delete(values, "SKYFEED_DISCORD_APPLICATION_ID") }, wantError: "APPLICATION_ID"},
		{name: "missing guild", mutate: func(values map[string]string) { delete(values, "SKYFEED_DISCORD_GUILD_ID") }, wantError: "GUILD_ID"},
		{name: "bad source path", mutate: func(values map[string]string) { values["SKYFEED_ADSB_BASE_URL"] = "http://receiver.invalid/not-data" }, wantError: "end in /data"},
		{name: "fast polling", mutate: func(values map[string]string) { values["SKYFEED_AIRCRAFT_POLL"] = "1ms" }, wantError: "AIRCRAFT_POLL"},
		{name: "route without provider", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_ROUTE_ENABLED"] = "true" }, wantError: "requires"},
		{name: "unsafe aircraft ttl", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_AIRCRAFT_TTL"] = "1h" }, wantError: "AIRCRAFT_TTL"},
		{name: "insecure ADSBDB", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_BASE_URL"] = "http://api.adsbdb.com/v0" }, wantError: "HTTPS"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := validEnvironment()
			test.mutate(environment)
			_, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestLoadWithRejectsTokenReadFailureAndWhitespace(t *testing.T) {
	environment := validEnvironment()
	_, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return nil, errors.New("denied") })
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("read error = %v", err)
	}

	_, err = LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("bad token"), nil })
	if err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("whitespace error = %v", err)
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		"SKYFEED_DISCORD_TOKEN_FILE":     "/run/secrets/discord_token",
		"SKYFEED_DISCORD_APPLICATION_ID": "1",
		"SKYFEED_DISCORD_GUILD_ID":       "2",
		"SKYFEED_ADSB_BASE_URL":          "http://receiver.invalid/data/",
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
