package rules

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

var ErrAlertQueueFull = errors.New("alert queue full")

type Queue struct {
	emergency chan domain.Alert
	normal    chan domain.Alert
	mu        sync.Mutex
	pending   map[string]struct{}
	dropped   atomic.Uint64
	coalesced atomic.Uint64
}

func NewQueue(emergencyCapacity, normalCapacity int) *Queue {
	return &Queue{emergency: make(chan domain.Alert, emergencyCapacity), normal: make(chan domain.Alert, normalCapacity), pending: make(map[string]struct{})}
}

func (queue *Queue) Enqueue(ctx context.Context, alert domain.Alert) error {
	if alert.Priority == domain.AlertEmergency {
		select {
		case queue.emergency <- alert:
			return nil
		case <-ctx.Done():
			queue.dropped.Add(1)
			return errors.Join(ErrAlertQueueFull, ctx.Err())
		}
	}
	key := alert.ConditionFingerprint + ":" + alert.AircraftICAO
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if _, exists := queue.pending[key]; exists {
		queue.coalesced.Add(1)
		return nil
	}
	queue.pending[key] = struct{}{}
	select {
	case queue.normal <- alert:
		return nil
	default:
		select {
		case oldest := <-queue.normal:
			delete(queue.pending, oldest.ConditionFingerprint+":"+oldest.AircraftICAO)
			queue.dropped.Add(1)
		default:
		}
		select {
		case queue.normal <- alert:
			return nil
		default:
			delete(queue.pending, key)
			queue.dropped.Add(1)
			return ErrAlertQueueFull
		}
	}
}

func (queue *Queue) Pop(ctx context.Context) (domain.Alert, bool) {
	select {
	case alert := <-queue.emergency:
		return alert, true
	default:
	}
	select {
	case <-ctx.Done():
		return domain.Alert{}, false
	case alert := <-queue.emergency:
		return alert, true
	case alert := <-queue.normal:
		queue.mu.Lock()
		delete(queue.pending, alert.ConditionFingerprint+":"+alert.AircraftICAO)
		queue.mu.Unlock()
		return alert, true
	}
}

func (queue *Queue) Depth() (emergency, normal int) { return len(queue.emergency), len(queue.normal) }
func (queue *Queue) Dropped() uint64                { return queue.dropped.Load() }
func (queue *Queue) Coalesced() uint64              { return queue.coalesced.Load() }
