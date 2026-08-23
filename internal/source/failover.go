package source

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type AircraftFailoverConfig struct {
	// RecoverySuccesses is the number of consecutive successful probes
	// required before returning to a higher-priority provider.
	RecoverySuccesses int
}

func DefaultAircraftFailoverConfig() AircraftFailoverConfig {
	return AircraftFailoverConfig{RecoverySuccesses: 2}
}

// AircraftFailover returns exactly one provider snapshot. Providers are
// ordered from highest to lowest priority and are never merged.
type AircraftFailover struct {
	mu                sync.Mutex
	providers         []AircraftSource
	active            int
	recoverySuccesses int
	recoveries        []int
}

func NewAircraftFailover(providers []AircraftSource, config AircraftFailoverConfig) (*AircraftFailover, error) {
	if len(providers) == 0 {
		return nil, errors.New("aircraft failover requires at least one provider")
	}
	if config.RecoverySuccesses < 0 {
		return nil, errors.New("aircraft failover recovery successes cannot be negative")
	}
	if config.RecoverySuccesses == 0 {
		config = DefaultAircraftFailoverConfig()
	}

	ordered := append([]AircraftSource(nil), providers...)
	seen := make(map[domain.ProviderID]struct{}, len(ordered))
	for _, provider := range ordered {
		if provider == nil {
			return nil, errors.New("aircraft failover provider cannot be nil")
		}
		id := provider.ProviderID()
		if !id.Known() {
			return nil, fmt.Errorf("aircraft failover provider %q is not a fixed provider ID", id)
		}
		if !provider.Capabilities().Supports(domain.CapabilityAircraft) {
			return nil, fmt.Errorf("provider %q does not support aircraft", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("aircraft failover provider %q is duplicated", id)
		}
		seen[id] = struct{}{}
	}
	return &AircraftFailover{
		providers:         ordered,
		active:            -1,
		recoverySuccesses: config.RecoverySuccesses,
		recoveries:        make([]int, len(ordered)),
	}, nil
}

func (failover *AircraftFailover) ProviderID() domain.ProviderID {
	failover.mu.Lock()
	defer failover.mu.Unlock()
	if failover.active >= 0 {
		return failover.providers[failover.active].ProviderID()
	}
	return failover.providers[0].ProviderID()
}

func (*AircraftFailover) Capabilities() domain.Capabilities {
	return domain.CapabilitiesOf(domain.CapabilityAircraft)
}

func (failover *AircraftFailover) FetchAircraft(ctx context.Context) (Frame[domain.AircraftBatch], error) {
	failover.mu.Lock()
	defer failover.mu.Unlock()

	if failover.active < 0 {
		return failover.fetchInitial(ctx)
	}
	if failover.active == 0 {
		frame, err := failover.fetch(ctx, 0)
		if err == nil {
			return frame, nil
		}
		if ctx.Err() != nil {
			return Frame[domain.AircraftBatch]{}, ctx.Err()
		}
		return failover.fetchLower(ctx, 1, []error{failover.providerError(0, err)})
	}

	active := failover.active
	var attemptErrors []error
	recoveredIndex := -1
	var recoveredFrame Frame[domain.AircraftBatch]
	for index := 0; index < active; index++ {
		frame, err := failover.fetch(ctx, index)
		if err != nil {
			failover.recoveries[index] = 0
			attemptErrors = append(attemptErrors, failover.providerError(index, err))
			if ctx.Err() != nil {
				return Frame[domain.AircraftBatch]{}, ctx.Err()
			}
			continue
		}
		failover.recoveries[index]++
		if failover.recoveries[index] >= failover.recoverySuccesses {
			failover.activate(index)
			return frame, nil
		}
		if recoveredIndex < 0 {
			recoveredIndex, recoveredFrame = index, frame
		}
	}

	frame, err := failover.fetch(ctx, active)
	if err == nil {
		return frame, nil
	}
	attemptErrors = append(attemptErrors, failover.providerError(active, err))
	if ctx.Err() != nil {
		return Frame[domain.AircraftBatch]{}, ctx.Err()
	}
	if recoveredIndex >= 0 {
		failover.activate(recoveredIndex)
		return recoveredFrame, nil
	}
	return failover.fetchLower(ctx, active+1, attemptErrors)
}

func (failover *AircraftFailover) fetchInitial(ctx context.Context) (Frame[domain.AircraftBatch], error) {
	return failover.fetchLower(ctx, 0, nil)
}

func (failover *AircraftFailover) fetchLower(ctx context.Context, start int, attemptErrors []error) (Frame[domain.AircraftBatch], error) {
	for index := start; index < len(failover.providers); index++ {
		frame, err := failover.fetch(ctx, index)
		if err == nil {
			failover.activate(index)
			return frame, nil
		}
		attemptErrors = append(attemptErrors, failover.providerError(index, err))
		if ctx.Err() != nil {
			return Frame[domain.AircraftBatch]{}, ctx.Err()
		}
	}
	return Frame[domain.AircraftBatch]{}, fmt.Errorf("all aircraft providers failed: %w", errors.Join(attemptErrors...))
}

func (failover *AircraftFailover) fetch(ctx context.Context, index int) (Frame[domain.AircraftBatch], error) {
	if err := ctx.Err(); err != nil {
		return Frame[domain.AircraftBatch]{}, err
	}
	frame, err := failover.providers[index].FetchAircraft(ctx)
	if err != nil {
		return Frame[domain.AircraftBatch]{}, err
	}
	if err := ctx.Err(); err != nil {
		return Frame[domain.AircraftBatch]{}, err
	}
	provider := failover.providers[index].ProviderID()
	frame.Provider = provider
	frame.Value.Provider = provider
	for aircraftIndex := range frame.Value.Aircraft {
		frame.Value.Aircraft[aircraftIndex].Provider = provider
	}
	return frame, nil
}

func (failover *AircraftFailover) providerError(index int, err error) error {
	return fmt.Errorf("%s aircraft fetch: %w", failover.providers[index].ProviderID(), err)
}

func (failover *AircraftFailover) activate(index int) {
	failover.active = index
	clear(failover.recoveries)
}
