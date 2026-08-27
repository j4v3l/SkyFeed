package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const maxTokenBytes = 4096

type Discord struct {
	TokenFile       string
	Token           Secret
	ApplicationID   uint64
	GuildID         uint64
	GlobalCommands  bool
	AdminRoleID     uint64
	OperatorRoleID  uint64
	ModeratorRoleID uint64
}

type ADSB struct {
	BaseURL       *url.URL
	ProviderOrder []domain.ProviderID
	AircraftPoll  time.Duration
	MetadataPoll  time.Duration
}

type AirplanesLive struct {
	PublicAirportCode  string
	DomesticCountryISO string
	Latitude           *float64
	Longitude          *float64
	RadiusNM           int
	Timeout            time.Duration
	Poll               time.Duration
}

type AdsbLol struct {
	Enabled bool
	BaseURL *url.URL
	Timeout time.Duration
}

type PlaneAlert struct {
	Enabled bool
	URL     string
	Timeout time.Duration
	Refresh time.Duration
}

type ADSBDB struct {
	Enabled      bool
	RouteEnabled bool
	BaseURL      *url.URL
	Timeout      time.Duration
	Workers      int
	AircraftTTL  time.Duration
	RouteTTL     time.Duration
	NotFoundTTL  time.Duration
	ErrorTTL     time.Duration
	StaleTTL     time.Duration
}

type AgentIngress struct {
	Enabled      bool
	Addr         string
	PublicURL    *url.URL
	MaxFeeders   int
	Workers      int
	Queue        int
	MaxBodyBytes int
}

type Config struct {
	Discord             Discord
	ADSB                ADSB
	AirplanesLive       AirplanesLive
	AdsbLol             AdsbLol
	PlaneAlert          PlaneAlert
	ADSBDB              ADSBDB
	AgentIngress        AgentIngress
	DatabasePath        string
	DashboardInterval   time.Duration
	AdminDigestInterval time.Duration
	HealthAddr          string
	PprofAddr           string
	LogLevel            string
	LogFormat           string
	Timezone            *time.Location
}

type LookupEnv func(string) (string, bool)
type ReadFile func(string) ([]byte, error)

func Load() (Config, error) {
	return LoadWith(os.LookupEnv, os.ReadFile)
}

func LoadWith(lookup LookupEnv, readFile ReadFile) (Config, error) {
	cfg := Config{
		ADSB: ADSB{
			ProviderOrder: []domain.ProviderID{domain.ProviderReadsb},
			AircraftPoll:  time.Second,
			MetadataPoll:  30 * time.Second,
		},
		AirplanesLive: AirplanesLive{
			RadiusNM: 50,
			Timeout:  2 * time.Second,
			Poll:     time.Second,
		},
		ADSBDB: ADSBDB{
			Enabled:     false,
			Timeout:     2 * time.Second,
			Workers:     2,
			AircraftTTL: 14 * 24 * time.Hour,
			RouteTTL:    8 * time.Hour,
			NotFoundTTL: 3 * time.Hour,
			ErrorTTL:    30 * time.Second,
			StaleTTL:    24 * time.Hour,
		},
		AgentIngress: AgentIngress{
			Addr: "127.0.0.1:9091", MaxFeeders: 100, Workers: 4, Queue: 256, MaxBodyBytes: 3 << 20,
		},
		AdsbLol: AdsbLol{
			Enabled: true,
			Timeout: 4 * time.Second,
		},
		PlaneAlert: PlaneAlert{
			Enabled: true,
			Timeout: 30 * time.Second,
			Refresh: 24 * time.Hour,
		},
		DatabasePath:        "/var/lib/skyfeed/skyfeed.db",
		DashboardInterval:   15 * time.Second,
		AdminDigestInterval: 6 * time.Hour,
		HealthAddr:          "0.0.0.0:9090",
		LogLevel:            "info",
		LogFormat:           "json",
		Timezone:            time.UTC,
	}

	cfg.Discord.TokenFile = env(lookup, "SKYFEED_DISCORD_TOKEN_FILE", "")
	cfg.DatabasePath = env(lookup, "SKYFEED_DATABASE_PATH", cfg.DatabasePath)
	cfg.HealthAddr = env(lookup, "SKYFEED_HEALTH_ADDR", cfg.HealthAddr)
	cfg.PprofAddr = env(lookup, "SKYFEED_PPROF_ADDR", "")
	cfg.AgentIngress.Addr = env(lookup, "SKYFEED_AGENT_ADDR", cfg.AgentIngress.Addr)
	cfg.LogLevel = strings.ToLower(env(lookup, "SKYFEED_LOG_LEVEL", cfg.LogLevel))
	cfg.LogFormat = strings.ToLower(env(lookup, "SKYFEED_LOG_FORMAT", cfg.LogFormat))

	var err error
	if cfg.Discord.ApplicationID, err = parseUint(lookup, "SKYFEED_DISCORD_APPLICATION_ID", 0); err != nil {
		return Config{}, err
	}
	if cfg.Discord.GuildID, err = parseUint(lookup, "SKYFEED_DISCORD_GUILD_ID", 0); err != nil {
		return Config{}, err
	}
	if cfg.Discord.GlobalCommands, err = parseBool(lookup, "SKYFEED_DISCORD_GLOBAL_COMMANDS", false); err != nil {
		return Config{}, err
	}
	if cfg.Discord.AdminRoleID, err = parseUint(lookup, "SKYFEED_DISCORD_ADMIN_ROLE_ID", 0); err != nil {
		return Config{}, err
	}
	if cfg.Discord.OperatorRoleID, err = parseUint(lookup, "SKYFEED_DISCORD_OPERATOR_ROLE_ID", 0); err != nil {
		return Config{}, err
	}
	if cfg.Discord.ModeratorRoleID, err = parseUint(lookup, "SKYFEED_DISCORD_MODERATOR_ROLE_ID", 0); err != nil {
		return Config{}, err
	}
	if cfg.ADSB.AircraftPoll, err = parseDuration(lookup, "SKYFEED_AIRCRAFT_POLL", cfg.ADSB.AircraftPoll); err != nil {
		return Config{}, err
	}
	if cfg.ADSB.MetadataPoll, err = parseDuration(lookup, "SKYFEED_METADATA_POLL", cfg.ADSB.MetadataPoll); err != nil {
		return Config{}, err
	}
	if cfg.ADSB.ProviderOrder, err = parseProviderOrder(lookup, "SKYFEED_AIRCRAFT_PROVIDER_ORDER", cfg.ADSB.ProviderOrder); err != nil {
		return Config{}, err
	}
	if cfg.AirplanesLive.Latitude, err = parseOptionalFloat(lookup, "SKYFEED_PUBLIC_CENTER_LATITUDE"); err != nil {
		return Config{}, err
	}
	if cfg.AirplanesLive.Longitude, err = parseOptionalFloat(lookup, "SKYFEED_PUBLIC_CENTER_LONGITUDE"); err != nil {
		return Config{}, err
	}
	if cfg.AirplanesLive.RadiusNM, err = parseInt(lookup, "SKYFEED_AIRPLANES_LIVE_RADIUS_NM", cfg.AirplanesLive.RadiusNM); err != nil {
		return Config{}, err
	}
	if cfg.AirplanesLive.Timeout, err = parseDuration(lookup, "SKYFEED_AIRPLANES_LIVE_TIMEOUT", cfg.AirplanesLive.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.AirplanesLive.Poll, err = parseDuration(lookup, "SKYFEED_AIRPLANES_LIVE_POLL", cfg.AirplanesLive.Poll); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.Timeout, err = parseDuration(lookup, "SKYFEED_ADSBDB_TIMEOUT", cfg.ADSBDB.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.AircraftTTL, err = parseDuration(lookup, "SKYFEED_ADSBDB_AIRCRAFT_TTL", cfg.ADSBDB.AircraftTTL); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.RouteTTL, err = parseDuration(lookup, "SKYFEED_ADSBDB_ROUTE_TTL", cfg.ADSBDB.RouteTTL); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.NotFoundTTL, err = parseDuration(lookup, "SKYFEED_ADSBDB_NOT_FOUND_TTL", cfg.ADSBDB.NotFoundTTL); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.ErrorTTL, err = parseDuration(lookup, "SKYFEED_ADSBDB_ERROR_TTL", cfg.ADSBDB.ErrorTTL); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.StaleTTL, err = parseDuration(lookup, "SKYFEED_ADSBDB_STALE_TTL", cfg.ADSBDB.StaleTTL); err != nil {
		return Config{}, err
	}
	if cfg.DashboardInterval, err = parseDuration(lookup, "SKYFEED_DASHBOARD_INTERVAL", cfg.DashboardInterval); err != nil {
		return Config{}, err
	}
	if cfg.AdminDigestInterval, err = parseDuration(lookup, "SKYFEED_ADMIN_DIGEST_INTERVAL", cfg.AdminDigestInterval); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.Workers, err = parseInt(lookup, "SKYFEED_ADSBDB_WORKERS", cfg.ADSBDB.Workers); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.Enabled, err = parseBool(lookup, "SKYFEED_ADSBDB_ENABLED", cfg.ADSBDB.Enabled); err != nil {
		return Config{}, err
	}
	if cfg.ADSBDB.RouteEnabled, err = parseBool(lookup, "SKYFEED_ADSBDB_ROUTE_ENABLED", cfg.ADSBDB.RouteEnabled); err != nil {
		return Config{}, err
	}
	if cfg.AgentIngress.Enabled, err = parseBool(lookup, "SKYFEED_AGENT_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.AgentIngress.MaxFeeders, err = parseInt(lookup, "SKYFEED_AGENT_MAX_FEEDERS", cfg.AgentIngress.MaxFeeders); err != nil {
		return Config{}, err
	}
	if cfg.AgentIngress.Workers, err = parseInt(lookup, "SKYFEED_AGENT_WORKERS", cfg.AgentIngress.Workers); err != nil {
		return Config{}, err
	}
	if cfg.AgentIngress.Queue, err = parseInt(lookup, "SKYFEED_AGENT_QUEUE", cfg.AgentIngress.Queue); err != nil {
		return Config{}, err
	}
	if cfg.AgentIngress.MaxBodyBytes, err = parseInt(lookup, "SKYFEED_AGENT_MAX_BODY_BYTES", cfg.AgentIngress.MaxBodyBytes); err != nil {
		return Config{}, err
	}
	if cfg.AdsbLol.Enabled, err = parseBool(lookup, "SKYFEED_ADSBLOL_ENABLED", cfg.AdsbLol.Enabled); err != nil {
		return Config{}, err
	}
	if cfg.AdsbLol.Timeout, err = parseDuration(lookup, "SKYFEED_ADSBLOL_TIMEOUT", cfg.AdsbLol.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.PlaneAlert.Enabled, err = parseBool(lookup, "SKYFEED_PLANE_ALERT_ENABLED", cfg.PlaneAlert.Enabled); err != nil {
		return Config{}, err
	}
	cfg.PlaneAlert.URL = env(lookup, "SKYFEED_PLANE_ALERT_URL", "")
	if cfg.PlaneAlert.Timeout, err = parseDuration(lookup, "SKYFEED_PLANE_ALERT_TIMEOUT", cfg.PlaneAlert.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.PlaneAlert.Refresh, err = parseDuration(lookup, "SKYFEED_PLANE_ALERT_REFRESH", cfg.PlaneAlert.Refresh); err != nil {
		return Config{}, err
	}

	if raw := env(lookup, "SKYFEED_ADSB_BASE_URL", ""); raw != "" {
		cfg.ADSB.BaseURL, err = url.Parse(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SKYFEED_ADSB_BASE_URL: %w", err)
		}
	}
	if raw := env(lookup, "SKYFEED_ADSBDB_BASE_URL", "https://api.adsbdb.com/v0"); raw != "" {
		cfg.ADSBDB.BaseURL, err = url.Parse(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SKYFEED_ADSBDB_BASE_URL: %w", err)
		}
	}
	if raw := env(lookup, "SKYFEED_ADSBLOL_BASE_URL", "https://api.adsb.lol"); raw != "" {
		cfg.AdsbLol.BaseURL, err = url.Parse(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SKYFEED_ADSBLOL_BASE_URL: %w", err)
		}
	}
	if raw := env(lookup, "SKYFEED_AGENT_PUBLIC_URL", ""); raw != "" {
		cfg.AgentIngress.PublicURL, err = url.Parse(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SKYFEED_AGENT_PUBLIC_URL: %w", err)
		}
	}
	cfg.AirplanesLive.PublicAirportCode = strings.ToUpper(env(lookup, "SKYFEED_PUBLIC_CENTER_AIRPORT_CODE", ""))
	cfg.AirplanesLive.DomesticCountryISO = strings.ToUpper(strings.TrimSpace(env(lookup, "SKYFEED_DOMESTIC_COUNTRY_ISO", "")))
	if cfg.AirplanesLive.DomesticCountryISO == "" {
		cfg.AirplanesLive.DomesticCountryISO = InferDomesticCountryISO(cfg.AirplanesLive.PublicAirportCode)
	}
	if zone := env(lookup, "SKYFEED_TIMEZONE", "UTC"); zone != "UTC" {
		cfg.Timezone, err = time.LoadLocation(zone)
		if err != nil {
			return Config{}, fmt.Errorf("SKYFEED_TIMEZONE: %w", err)
		}
	}

	if err := cfg.validateStatic(); err != nil {
		return Config{}, err
	}
	tokenBytes, err := readFile(cfg.Discord.TokenFile)
	if err != nil {
		return Config{}, fmt.Errorf("SKYFEED_DISCORD_TOKEN_FILE: read token: %w", err)
	}
	if len(tokenBytes) > maxTokenBytes {
		return Config{}, fmt.Errorf("SKYFEED_DISCORD_TOKEN_FILE: token exceeds %d bytes", maxTokenBytes)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return Config{}, fmt.Errorf("SKYFEED_DISCORD_TOKEN_FILE: token is empty")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return Config{}, fmt.Errorf("SKYFEED_DISCORD_TOKEN_FILE: token contains whitespace")
	}
	cfg.Discord.Token = newSecret(token)

	return cfg, nil
}

func HealthAddress(lookup LookupEnv) (string, error) {
	addr := env(lookup, "SKYFEED_HEALTH_ADDR", "0.0.0.0:9090")
	if err := validateAddress("SKYFEED_HEALTH_ADDR", addr); err != nil {
		return "", err
	}
	return addr, nil
}

func env(lookup LookupEnv, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func parseUint(lookup LookupEnv, key string, fallback uint64) (uint64, error) {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: parse unsigned integer: %w", key, err)
	}
	return value, nil
}

func parseInt(lookup LookupEnv, key string, fallback int) (int, error) {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: parse integer: %w", key, err)
	}
	return value, nil
}

func parseDuration(lookup LookupEnv, key string, fallback time.Duration) (time.Duration, error) {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: parse duration: %w", key, err)
	}
	return value, nil
}

func parseBool(lookup LookupEnv, key string, fallback bool) (bool, error) {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: parse boolean: %w", key, err)
	}
	return value, nil
}

func parseProviderOrder(lookup LookupEnv, key string, fallback []domain.ProviderID) ([]domain.ProviderID, error) {
	raw := env(lookup, key, "")
	if raw == "" {
		return append([]domain.ProviderID(nil), fallback...), nil
	}
	parts := strings.Split(raw, ",")
	providers := make([]domain.ProviderID, 0, len(parts))
	for _, part := range parts {
		provider := domain.ProviderID(strings.TrimSpace(part))
		if provider == "" {
			return nil, fmt.Errorf("%s must not contain empty providers", key)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func parseOptionalFloat(lookup LookupEnv, key string) (*float64, error) {
	raw, configured := lookup(key)
	if !configured || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("%s must be a finite number", key)
	}
	return &value, nil
}
