package enrichment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var ErrNotFound = errors.New("enrichment not found")

type Enricher interface {
	Lookup(ctx context.Context, icao, callsign string) (domain.Enrichment, error)
}

type RetryableError interface {
	error
	Retryable() bool
	RetryDelay() time.Duration
}

func NormalizeKey(icao, callsign string) (normalizedICAO, normalizedCallsign, key string) {
	normalizedICAO = strings.ToUpper(strings.TrimSpace(icao))
	normalizedCallsign = strings.ToUpper(strings.TrimSpace(callsign))
	return normalizedICAO, normalizedCallsign, normalizedICAO + "|" + normalizedCallsign
}
