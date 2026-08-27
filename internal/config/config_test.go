package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
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
	if len(cfg.ADSB.ProviderOrder) != 1 || cfg.ADSB.ProviderOrder[0] != domain.ProviderReadsb {
		t.Fatalf("provider order = %v", cfg.ADSB.ProviderOrder)
	}
	if cfg.AirplanesLive.PublicAirportCode != "" || cfg.AirplanesLive.Latitude != nil || cfg.AirplanesLive.Longitude != nil {
		t.Fatalf("airplanes.live center must default unset: %+v", cfg.AirplanesLive)
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

func TestLoadWithExplicitAirplanesLiveFallback(t *testing.T) {
	environment := validEnvironment()
	environment["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "readsb, airplanes-live"
	environment["SKYFEED_PUBLIC_CENTER_AIRPORT_CODE"] = "kxyz"
	environment["SKYFEED_PUBLIC_CENTER_LATITUDE"] = "1.25"
	environment["SKYFEED_PUBLIC_CENTER_LONGITUDE"] = "-2.5"
	environment["SKYFEED_AIRPLANES_LIVE_RADIUS_NM"] = "75"
	environment["SKYFEED_AIRPLANES_LIVE_TIMEOUT"] = "1500ms"
	environment["SKYFEED_AIRPLANES_LIVE_POLL"] = "2s"

	cfg, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ADSB.ProviderOrder) != 2 || cfg.ADSB.ProviderOrder[1] != domain.ProviderAirplanesLive {
		t.Fatalf("provider order = %v", cfg.ADSB.ProviderOrder)
	}
	if cfg.AirplanesLive.PublicAirportCode != "KXYZ" ||
		cfg.AirplanesLive.Latitude == nil || *cfg.AirplanesLive.Latitude != 1.25 ||
		cfg.AirplanesLive.Longitude == nil || *cfg.AirplanesLive.Longitude != -2.5 {
		t.Fatalf("public center = %+v", cfg.AirplanesLive)
	}
	if cfg.AirplanesLive.RadiusNM != 75 || cfg.AirplanesLive.Timeout != 1500*time.Millisecond || cfg.AirplanesLive.Poll != 2*time.Second {
		t.Fatalf("airplanes.live settings = %+v", cfg.AirplanesLive)
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

func TestLoadWithAgentIngressIsExplicitAndBounded(t *testing.T) {
	environment := validEnvironment()
	environment["SKYFEED_AGENT_ENABLED"] = "true"
	environment["SKYFEED_AGENT_PUBLIC_URL"] = "https://mesh.example.test/skyfeed"
	environment["SKYFEED_AGENT_ADDR"] = "127.0.0.1:19091"
	environment["SKYFEED_AGENT_MAX_FEEDERS"] = "100"
	cfg, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AgentIngress.Enabled || cfg.AgentIngress.PublicURL == nil || cfg.AgentIngress.PublicURL.Scheme != "https" || cfg.AgentIngress.Addr != "127.0.0.1:19091" || cfg.AgentIngress.MaxFeeders != 100 {
		t.Fatalf("agent ingress = %+v", cfg.AgentIngress)
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
		{name: "unknown aircraft provider", mutate: func(values map[string]string) { values["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "readsb,other" }, wantError: "airplanes-live"},
		{name: "reversed aircraft providers", mutate: func(values map[string]string) { values["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "airplanes-live,readsb" }, wantError: "start with readsb"},
		{name: "fallback without public center", mutate: func(values map[string]string) { values["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "readsb,airplanes-live" }, wantError: "public center"},
		{name: "incomplete activity center", mutate: func(values map[string]string) { values["SKYFEED_PUBLIC_CENTER_AIRPORT_CODE"] = "KXYZ" }, wantError: "both public center coordinates"},
		{name: "fast airplanes live polling", mutate: func(values map[string]string) { values["SKYFEED_AIRPLANES_LIVE_POLL"] = "500ms" }, wantError: "AIRPLANES_LIVE_POLL"},
		{name: "invalid airplanes live radius", mutate: func(values map[string]string) {
			values["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "readsb,airplanes-live"
			values["SKYFEED_PUBLIC_CENTER_AIRPORT_CODE"] = "KXYZ"
			values["SKYFEED_PUBLIC_CENTER_LATITUDE"] = "1"
			values["SKYFEED_PUBLIC_CENTER_LONGITUDE"] = "2"
			values["SKYFEED_AIRPLANES_LIVE_RADIUS_NM"] = "251"
		}, wantError: "RADIUS_NM"},
		{name: "route without provider", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_ROUTE_ENABLED"] = "true" }, wantError: "requires"},
		{name: "unsafe aircraft ttl", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_AIRCRAFT_TTL"] = "1h" }, wantError: "AIRCRAFT_TTL"},
		{name: "insecure ADSBDB", mutate: func(values map[string]string) { values["SKYFEED_ADSBDB_BASE_URL"] = "http://api.adsbdb.com/v0" }, wantError: "HTTPS"},
		{name: "agent without public URL", mutate: func(values map[string]string) { values["SKYFEED_AGENT_ENABLED"] = "true" }, wantError: "AGENT_PUBLIC_URL"},
		{name: "agent insecure public URL", mutate: func(values map[string]string) {
			values["SKYFEED_AGENT_ENABLED"] = "true"
			values["SKYFEED_AGENT_PUBLIC_URL"] = "http://example.test"
		}, wantError: "absolute HTTPS"},
		{name: "agent excessive feeder count", mutate: func(values map[string]string) { values["SKYFEED_AGENT_MAX_FEEDERS"] = "251" }, wantError: "MAX_FEEDERS"},
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

func TestLoadWithLocalAirportActivityWithoutExternalFallback(t *testing.T) {
	environment := validEnvironment()
	environment["SKYFEED_PUBLIC_CENTER_AIRPORT_CODE"] = "KXYZ"
	environment["SKYFEED_PUBLIC_CENTER_LATITUDE"] = "1.25"
	environment["SKYFEED_PUBLIC_CENTER_LONGITUDE"] = "-2.5"
	cfg, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ADSB.ProviderOrder) != 1 || cfg.AirplanesLive.PublicAirportCode != "KXYZ" {
		t.Fatalf("local activity config = %+v", cfg)
	}
}

func TestLoadWithCoordinateParseErrorDoesNotEchoInput(t *testing.T) {
	environment := validEnvironment()
	environment["SKYFEED_AIRCRAFT_PROVIDER_ORDER"] = "readsb,airplanes-live"
	environment["SKYFEED_PUBLIC_CENTER_AIRPORT_CODE"] = "KXYZ"
	environment["SKYFEED_PUBLIC_CENTER_LATITUDE"] = "private-coordinate-value"
	environment["SKYFEED_PUBLIC_CENTER_LONGITUDE"] = "2"
	_, err := LoadWith(mapLookup(environment), func(string) ([]byte, error) { return []byte("token"), nil })
	if err == nil {
		t.Fatal("invalid coordinate was accepted")
	}
	if strings.Contains(err.Error(), "private-coordinate-value") {
		t.Fatalf("configuration error exposed coordinate input: %v", err)
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
