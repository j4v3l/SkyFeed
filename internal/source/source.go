package source

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

type Frame[T any] struct {
	FeederID  domain.FeederID
	FetchedAt time.Time
	Provider  domain.ProviderID
	Value     T
}

type Provider interface {
	ProviderID() domain.ProviderID
	Capabilities() domain.Capabilities
}

type AircraftSource interface {
	Provider
	FetchAircraft(context.Context) (Frame[domain.AircraftBatch], error)
}

type ReceiverSource interface {
	Provider
	FetchReceiver(context.Context) (Frame[domain.Receiver], error)
}

type StatisticsSource interface {
	Provider
	FetchStats(context.Context) (Frame[domain.Statistics], error)
}

type Source interface {
	AircraftSource
	ReceiverSource
	StatisticsSource
}

// Set keeps independently capable providers separate. In particular, an
// aircraft failover may be paired with readsb-only receiver and statistics
// sources without asking fallback providers for unsupported metadata.
type Set struct {
	Aircraft AircraftSource
	Receiver ReceiverSource
	Stats    StatisticsSource
}

func NewSet(upstream Source) Set {
	return Set{Aircraft: upstream, Receiver: upstream, Stats: upstream}
}

func (set Set) Capabilities() domain.Capabilities {
	var capabilities domain.Capabilities
	if Supports(set.Aircraft, domain.CapabilityAircraft) {
		capabilities |= domain.Capabilities(domain.CapabilityAircraft)
	}
	if Supports(set.Receiver, domain.CapabilityReceiver) {
		capabilities |= domain.Capabilities(domain.CapabilityReceiver)
	}
	if Supports(set.Stats, domain.CapabilityStatistics) {
		capabilities |= domain.Capabilities(domain.CapabilityStatistics)
	}
	return capabilities
}

func Supports(provider Provider, capability domain.Capability) bool {
	return provider != nil && provider.Capabilities().Supports(capability)
}

type ErrorClass string

const (
	ErrorTimeout ErrorClass = "timeout"
	ErrorNetwork ErrorClass = "network"
	ErrorStatus  ErrorClass = "http_status"
	ErrorPayload ErrorClass = "invalid_payload"
	ErrorUnknown ErrorClass = "unknown"
)

type FetchError struct {
	Endpoint string
	Class    ErrorClass
	Err      error
}

func (err *FetchError) Error() string {
	return fmt.Sprintf("fetch %s: %v", err.Endpoint, err.Err)
}

func (err *FetchError) Unwrap() error {
	return err.Err
}

func ClassifyError(err error) ErrorClass {
	var fetchError *FetchError
	if errors.As(err, &fetchError) {
		return fetchError.Class
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return ErrorTimeout
		}
		return ErrorNetwork
	}
	return ErrorUnknown
}
