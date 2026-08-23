package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
	"github.com/j4v3l/SkyFeed/internal/source/airplaneslive"
	"github.com/j4v3l/SkyFeed/internal/source/readsb"
)

const readsbTimeout = 2 * time.Second

type configuredSources struct {
	Set            source.Set
	Readsb         *readsb.Client
	AirplanesLive  *airplaneslive.Client
	AircraftChecks []source.AircraftSource
}

func configureSources(cfg config.Config) (configuredSources, error) {
	readsbClient := readsb.NewClient(cfg.ADSB.BaseURL, readsbTimeout)
	configured := configuredSources{
		Readsb:         readsbClient,
		AircraftChecks: []source.AircraftSource{readsbClient},
	}
	providers := cfg.ADSB.ProviderOrder
	if len(providers) == 0 {
		providers = []domain.ProviderID{domain.ProviderReadsb}
	}
	if providers[0] != domain.ProviderReadsb {
		return configuredSources{}, errors.New("aircraft provider order must start with readsb")
	}

	aircraftSources := []source.AircraftSource{readsbClient}
	for _, provider := range providers[1:] {
		if provider != domain.ProviderAirplanesLive {
			return configuredSources{}, fmt.Errorf("unsupported aircraft provider %q", provider)
		}
		if cfg.AirplanesLive.Latitude == nil || cfg.AirplanesLive.Longitude == nil {
			return configuredSources{}, errors.New("airplanes.live public center is not configured")
		}
		client, err := airplaneslive.NewClient(airplaneslive.Config{
			Latitude:        *cfg.AirplanesLive.Latitude,
			Longitude:       *cfg.AirplanesLive.Longitude,
			RadiusNM:        cfg.AirplanesLive.RadiusNM,
			Timeout:         cfg.AirplanesLive.Timeout,
			MinimumInterval: cfg.AirplanesLive.Poll,
		})
		if err != nil {
			return configuredSources{}, fmt.Errorf("configure airplanes.live source: %w", err)
		}
		configured.AirplanesLive = client
		configured.AircraftChecks = append(configured.AircraftChecks, client)
		aircraftSources = append(aircraftSources, client)
	}

	aircraftSource := source.AircraftSource(readsbClient)
	if len(aircraftSources) > 1 {
		failover, err := source.NewAircraftFailover(aircraftSources, source.DefaultAircraftFailoverConfig())
		if err != nil {
			return configuredSources{}, fmt.Errorf("configure aircraft failover: %w", err)
		}
		aircraftSource = failover
	}
	configured.Set = source.Set{
		Aircraft: aircraftSource,
		Receiver: readsbClient,
		Stats:    readsbClient,
	}
	return configured, nil
}
