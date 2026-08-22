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
	FetchedAt time.Time
	Value     T
}

type Source interface {
	FetchAircraft(context.Context) (Frame[domain.AircraftBatch], error)
	FetchReceiver(context.Context) (Frame[domain.Receiver], error)
	FetchStats(context.Context) (Frame[domain.Statistics], error)
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
