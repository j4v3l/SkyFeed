package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
	"github.com/j4v3l/SkyFeed/internal/source/readsb"
	"github.com/j4v3l/SkyFeed/internal/state"
)

type RuntimeConfig struct {
	ReceiverURL     *url.URL
	AircraftPoll    time.Duration
	MetadataPoll    time.Duration
	EnrollmentToken string
}

// Run polls and normalizes readsb locally, then forwards only signed snapshots.
// The queue is bounded and latest-value-wins during an outage.
func Run(ctx context.Context, config RuntimeConfig, client *Client, logger *slog.Logger) error {
	if config.ReceiverURL == nil || client == nil {
		return errors.New("agent receiver and central client are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if config.AircraftPoll <= 0 {
		config.AircraftPoll = time.Second
	}
	if config.MetadataPoll <= 0 {
		config.MetadataPoll = 30 * time.Second
	}
	if _, err := client.LoadCredentials(); err != nil {
		if config.EnrollmentToken == "" {
			return err
		}
		if _, enrollErr := client.Enroll(ctx, config.EnrollmentToken); enrollErr != nil {
			return fmt.Errorf("enroll agent: %w", enrollErr)
		}
	}

	latest := make(chan *domain.Snapshot, 5)
	engine := state.NewEngineForFeeder(domain.FeederLocal, func(snapshot *domain.Snapshot) {
		if snapshot == nil || snapshot.FetchedAt.IsZero() {
			return
		}
		copyValue := *snapshot
		select {
		case latest <- &copyValue:
		default:
			select {
			case <-latest:
			default:
			}
			select {
			case latest <- &copyValue:
			default:
			}
		}
	})
	receiver := readsb.NewClient(config.ReceiverURL, 2*time.Second)
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error {
		return engine.Run(groupContext, source.NewSet(receiver), config.AircraftPoll, config.MetadataPoll)
	})
	group.Go(func() error {
		backoff := time.Second
		var pending *domain.Snapshot
		for {
			if pending == nil {
				select {
				case <-groupContext.Done():
					return nil
				case pending = <-latest:
				}
				// Collapse an outage burst to the newest observation. The current
				// signed delivery still retries to completion before this drain.
				for draining := true; draining; {
					select {
					case pending = <-latest:
					default:
						draining = false
					}
				}
			}
			sendContext, cancel := context.WithTimeout(groupContext, 12*time.Second)
			err := client.Send(sendContext, pending)
			cancel()
			if err == nil {
				pending = nil
				backoff = time.Second
				continue
			}
			logger.Warn("central snapshot delivery failed", "component", "agent", "event", "delivery_retry", "error", err, "retry_in", backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-groupContext.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	})
	return group.Wait()
}
