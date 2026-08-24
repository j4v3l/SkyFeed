package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

func (cfg *Config) validateStatic() error {
	var errs []error
	if cfg.Discord.TokenFile == "" {
		errs = append(errs, errors.New("SKYFEED_DISCORD_TOKEN_FILE is required"))
	} else if !filepath.IsAbs(cfg.Discord.TokenFile) {
		errs = append(errs, errors.New("SKYFEED_DISCORD_TOKEN_FILE must be an absolute path"))
	}
	if cfg.Discord.ApplicationID == 0 {
		errs = append(errs, errors.New("SKYFEED_DISCORD_APPLICATION_ID is required and must be non-zero"))
	}
	if cfg.Discord.GuildID == 0 {
		errs = append(errs, errors.New("SKYFEED_DISCORD_GUILD_ID is required for local development"))
	}
	if err := validateReadsbURL(cfg.ADSB.BaseURL); err != nil {
		errs = append(errs, err)
	}
	airplanesLiveEnabled, providerOrderErr := validateProviderOrder(cfg.ADSB.ProviderOrder)
	if providerOrderErr != nil {
		errs = append(errs, providerOrderErr)
	}
	if err := validateAirplanesLive(cfg.AirplanesLive, airplanesLiveEnabled); err != nil {
		errs = append(errs, err)
	}
	if err := validateADSBDBURL(cfg.ADSBDB.BaseURL); err != nil {
		errs = append(errs, err)
	}
	if err := validateAdsbLolURL(cfg.AdsbLol.BaseURL); err != nil {
		errs = append(errs, err)
	}
	if cfg.AdsbLol.Enabled && cfg.AdsbLol.BaseURL == nil {
		errs = append(errs, errors.New("SKYFEED_ADSBLOL_BASE_URL is required when adsb.lol enrichment is enabled"))
	}
	if cfg.ADSBDB.RouteEnabled && !cfg.ADSBDB.Enabled {
		errs = append(errs, errors.New("SKYFEED_ADSBDB_ROUTE_ENABLED requires SKYFEED_ADSBDB_ENABLED"))
	}
	if !filepath.IsAbs(cfg.DatabasePath) {
		errs = append(errs, errors.New("SKYFEED_DATABASE_PATH must be an absolute path"))
	}

	errs = append(errs,
		validateDuration("SKYFEED_AIRCRAFT_POLL", cfg.ADSB.AircraftPoll, 250*time.Millisecond, time.Minute),
		validateDuration("SKYFEED_METADATA_POLL", cfg.ADSB.MetadataPoll, 5*time.Second, 15*time.Minute),
		validateDuration("SKYFEED_AIRPLANES_LIVE_TIMEOUT", cfg.AirplanesLive.Timeout, 250*time.Millisecond, 10*time.Second),
		validateDuration("SKYFEED_AIRPLANES_LIVE_POLL", cfg.AirplanesLive.Poll, time.Second, time.Minute),
		validateDuration("SKYFEED_ADSBDB_TIMEOUT", cfg.ADSBDB.Timeout, 250*time.Millisecond, 10*time.Second),
		validateDuration("SKYFEED_ADSBLOL_TIMEOUT", cfg.AdsbLol.Timeout, 250*time.Millisecond, 10*time.Second),
		validateDuration("SKYFEED_ADSBDB_AIRCRAFT_TTL", cfg.ADSBDB.AircraftTTL, 24*time.Hour, 30*24*time.Hour),
		validateDuration("SKYFEED_ADSBDB_ROUTE_TTL", cfg.ADSBDB.RouteTTL, time.Hour, 12*time.Hour),
		validateDuration("SKYFEED_ADSBDB_NOT_FOUND_TTL", cfg.ADSBDB.NotFoundTTL, 15*time.Minute, 6*time.Hour),
		validateDuration("SKYFEED_ADSBDB_ERROR_TTL", cfg.ADSBDB.ErrorTTL, 15*time.Second, 5*time.Minute),
		validateDuration("SKYFEED_ADSBDB_STALE_TTL", cfg.ADSBDB.StaleTTL, time.Hour, 7*24*time.Hour),
		validateDuration("SKYFEED_DASHBOARD_INTERVAL", cfg.DashboardInterval, 10*time.Second, 15*time.Minute),
	)
	if cfg.AdminDigestInterval < 0 {
		errs = append(errs, errors.New("SKYFEED_ADMIN_DIGEST_INTERVAL must be zero (disabled) or a positive duration"))
	} else if cfg.AdminDigestInterval > 0 && (cfg.AdminDigestInterval < time.Hour || cfg.AdminDigestInterval > 24*time.Hour) {
		errs = append(errs, errors.New("SKYFEED_ADMIN_DIGEST_INTERVAL must be between 1h and 24h when enabled"))
	}
	if cfg.ADSBDB.Workers < 1 || cfg.ADSBDB.Workers > 16 {
		errs = append(errs, errors.New("SKYFEED_ADSBDB_WORKERS must be between 1 and 16"))
	}
	if err := validateAddress("SKYFEED_HEALTH_ADDR", cfg.HealthAddr); err != nil {
		errs = append(errs, err)
	}
	if cfg.PprofAddr != "" {
		if err := validateAddress("SKYFEED_PPROF_ADDR", cfg.PprofAddr); err != nil {
			errs = append(errs, err)
		} else if host, _, _ := net.SplitHostPort(cfg.PprofAddr); host != "127.0.0.1" && host != "::1" && host != "localhost" {
			errs = append(errs, errors.New("SKYFEED_PPROF_ADDR must bind to a loopback host"))
		}
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, cfg.LogLevel) {
		errs = append(errs, errors.New("SKYFEED_LOG_LEVEL must be debug, info, warn, or error"))
	}
	if !slices.Contains([]string{"json", "text"}, cfg.LogFormat) {
		errs = append(errs, errors.New("SKYFEED_LOG_FORMAT must be json or text"))
	}

	return errors.Join(compact(errs)...)
}

func validateProviderOrder(providers []domain.ProviderID) (bool, error) {
	if len(providers) == 0 {
		return false, errors.New("SKYFEED_AIRCRAFT_PROVIDER_ORDER must contain readsb")
	}
	if len(providers) > 2 {
		return false, errors.New("SKYFEED_AIRCRAFT_PROVIDER_ORDER supports only readsb,airplanes-live")
	}
	if providers[0] != domain.ProviderReadsb {
		return false, errors.New("SKYFEED_AIRCRAFT_PROVIDER_ORDER must start with readsb")
	}
	if len(providers) == 1 {
		return false, nil
	}
	if providers[1] != domain.ProviderAirplanesLive {
		return false, errors.New("SKYFEED_AIRCRAFT_PROVIDER_ORDER may only append airplanes-live after readsb")
	}
	return true, nil
}

func validateAirplanesLive(value AirplanesLive, enabled bool) error {
	centerConfigured := value.PublicAirportCode != "" || value.Latitude != nil || value.Longitude != nil
	if !enabled {
		if centerConfigured {
			return errors.New("airplanes-live public center settings require airplanes-live in SKYFEED_AIRCRAFT_PROVIDER_ORDER")
		}
		return nil
	}

	var errs []error
	if len(value.PublicAirportCode) != 4 || !asciiLetters(value.PublicAirportCode) {
		errs = append(errs, errors.New("SKYFEED_PUBLIC_CENTER_AIRPORT_CODE must be a four-letter airport code"))
	}
	if value.Latitude == nil || value.Longitude == nil {
		errs = append(errs, errors.New("airplanes-live requires both public center coordinates"))
	} else {
		if *value.Latitude < -90 || *value.Latitude > 90 {
			errs = append(errs, errors.New("SKYFEED_PUBLIC_CENTER_LATITUDE must be between -90 and 90"))
		}
		if *value.Longitude < -180 || *value.Longitude > 180 {
			errs = append(errs, errors.New("SKYFEED_PUBLIC_CENTER_LONGITUDE must be between -180 and 180"))
		}
	}
	if value.RadiusNM < 1 || value.RadiusNM > 250 {
		errs = append(errs, errors.New("SKYFEED_AIRPLANES_LIVE_RADIUS_NM must be between 1 and 250"))
	}
	return errors.Join(errs...)
}

func asciiLetters(value string) bool {
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validateReadsbURL(value *url.URL) error {
	if value == nil {
		return errors.New("SKYFEED_ADSB_BASE_URL is required")
	}
	if value.Scheme != "http" && value.Scheme != "https" {
		return errors.New("SKYFEED_ADSB_BASE_URL scheme must be http or https")
	}
	if value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return errors.New("SKYFEED_ADSB_BASE_URL must contain only an absolute scheme, host, and path")
	}
	cleaned := strings.TrimSuffix(path.Clean(value.Path), "/")
	if !strings.HasSuffix(cleaned, "/data") {
		return errors.New("SKYFEED_ADSB_BASE_URL path must end in /data")
	}
	value.Path = cleaned
	return nil
}

func validateADSBDBURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return errors.New("SKYFEED_ADSBDB_BASE_URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	value.Path = strings.TrimSuffix(value.Path, "/")
	return nil
}

func validateAdsbLolURL(value *url.URL) error {
	if value == nil {
		return nil
	}
	if value.Scheme != "https" || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return errors.New("SKYFEED_ADSBLOL_BASE_URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	value.Path = strings.TrimSuffix(value.Path, "/")
	return nil
}

func validateDuration(name string, value, minimum, maximum time.Duration) error {
	if value < minimum || value > maximum {
		return fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return nil
}

func validateAddress(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if port == "" {
		return fmt.Errorf("%s must include a port", name)
	}
	return nil
}

func compact(errs []error) []error {
	result := errs[:0]
	for _, err := range errs {
		if err != nil {
			result = append(result, err)
		}
	}
	return result
}
