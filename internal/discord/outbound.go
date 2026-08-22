package discord

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrQueueFull = errors.New("outbound queue full")
	ErrNoRun     = errors.New("outbound job has no run function")
)

type Priority uint8

const (
	PriorityEmergency Priority = iota
	PriorityInteraction
	PriorityAlert
	PriorityDashboard
	PriorityReport
)

type OutboundJob struct {
	Key       string
	Priority  Priority
	Retryable bool
	Run       func(context.Context) error
}

type QueueStats struct {
	Accepted  uint64
	Succeeded uint64
	Failed    uint64
	Retried   uint64
	Dropped   uint64
	Coalesced uint64
}

type OutboundScheduler struct {
	emergency   chan OutboundJob
	interaction chan OutboundJob
	alert       chan OutboundJob
	dashboard   chan OutboundJob
	report      chan OutboundJob

	mu      sync.Mutex
	pending map[Priority]map[string]struct{}

	accepted  atomic.Uint64
	succeeded atomic.Uint64
	failed    atomic.Uint64
	retried   atomic.Uint64
	dropped   atomic.Uint64
	coalesced atomic.Uint64

	retryBase time.Duration
}

func NewOutboundScheduler(emergencyCapacity, interactionCapacity, alertCapacity, reportCapacity int) *OutboundScheduler {
	return &OutboundScheduler{
		emergency:   make(chan OutboundJob, emergencyCapacity),
		interaction: make(chan OutboundJob, interactionCapacity),
		alert:       make(chan OutboundJob, alertCapacity),
		dashboard:   make(chan OutboundJob, 1),
		report:      make(chan OutboundJob, reportCapacity),
		pending: map[Priority]map[string]struct{}{
			PriorityAlert:  {},
			PriorityReport: {},
		},
		retryBase: 100 * time.Millisecond,
	}
}

func (scheduler *OutboundScheduler) Enqueue(ctx context.Context, job OutboundJob) error {
	if job.Run == nil {
		return ErrNoRun
	}
	switch job.Priority {
	case PriorityEmergency:
		select {
		case scheduler.emergency <- job:
			scheduler.accepted.Add(1)
			return nil
		case <-ctx.Done():
			scheduler.dropped.Add(1)
			return errors.Join(ErrQueueFull, ctx.Err())
		}
	case PriorityInteraction:
		select {
		case scheduler.interaction <- job:
			scheduler.accepted.Add(1)
			return nil
		case <-ctx.Done():
			scheduler.dropped.Add(1)
			return errors.Join(ErrQueueFull, ctx.Err())
		}
	case PriorityAlert:
		return scheduler.enqueueDeduplicated(scheduler.alert, job)
	case PriorityDashboard:
		select {
		case scheduler.dashboard <- job:
			scheduler.accepted.Add(1)
			return nil
		default:
		}
		select {
		case <-scheduler.dashboard:
			scheduler.coalesced.Add(1)
		default:
		}
		select {
		case scheduler.dashboard <- job:
			scheduler.accepted.Add(1)
			return nil
		default:
			scheduler.dropped.Add(1)
			return ErrQueueFull
		}
	case PriorityReport:
		return scheduler.enqueueDeduplicated(scheduler.report, job)
	default:
		return ErrQueueFull
	}
}

func (scheduler *OutboundScheduler) Run(ctx context.Context) error {
	for {
		job, ok := scheduler.next(ctx)
		if !ok {
			return nil
		}
		scheduler.releasePending(job)
		scheduler.execute(ctx, job)
	}
}

func (scheduler *OutboundScheduler) Stats() QueueStats {
	return QueueStats{
		Accepted:  scheduler.accepted.Load(),
		Succeeded: scheduler.succeeded.Load(),
		Failed:    scheduler.failed.Load(),
		Retried:   scheduler.retried.Load(),
		Dropped:   scheduler.dropped.Load(),
		Coalesced: scheduler.coalesced.Load(),
	}
}

func (scheduler *OutboundScheduler) Depth(priority Priority) int {
	switch priority {
	case PriorityEmergency:
		return len(scheduler.emergency)
	case PriorityInteraction:
		return len(scheduler.interaction)
	case PriorityAlert:
		return len(scheduler.alert)
	case PriorityDashboard:
		return len(scheduler.dashboard)
	case PriorityReport:
		return len(scheduler.report)
	default:
		return 0
	}
}

func (scheduler *OutboundScheduler) enqueueDeduplicated(queue chan OutboundJob, job OutboundJob) error {
	scheduler.mu.Lock()
	if job.Key != "" {
		if _, exists := scheduler.pending[job.Priority][job.Key]; exists {
			scheduler.mu.Unlock()
			scheduler.coalesced.Add(1)
			return nil
		}
		scheduler.pending[job.Priority][job.Key] = struct{}{}
	}
	select {
	case queue <- job:
		scheduler.mu.Unlock()
		scheduler.accepted.Add(1)
		return nil
	default:
		if job.Key != "" {
			delete(scheduler.pending[job.Priority], job.Key)
		}
		scheduler.mu.Unlock()
		scheduler.dropped.Add(1)
		return ErrQueueFull
	}
}

func (scheduler *OutboundScheduler) releasePending(job OutboundJob) {
	if job.Key == "" || (job.Priority != PriorityAlert && job.Priority != PriorityReport) {
		return
	}
	scheduler.mu.Lock()
	delete(scheduler.pending[job.Priority], job.Key)
	scheduler.mu.Unlock()
}

func (scheduler *OutboundScheduler) next(ctx context.Context) (OutboundJob, bool) {
	// The non-blocking cascade makes queued high-priority work deterministic.
	select {
	case job := <-scheduler.emergency:
		return job, true
	default:
	}
	select {
	case job := <-scheduler.interaction:
		return job, true
	default:
	}
	select {
	case job := <-scheduler.alert:
		return job, true
	default:
	}
	select {
	case job := <-scheduler.dashboard:
		return job, true
	default:
	}
	select {
	case job := <-scheduler.report:
		return job, true
	default:
	}
	select {
	case <-ctx.Done():
		return OutboundJob{}, false
	case job := <-scheduler.emergency:
		return job, true
	case job := <-scheduler.interaction:
		return job, true
	case job := <-scheduler.alert:
		return job, true
	case job := <-scheduler.dashboard:
		return job, true
	case job := <-scheduler.report:
		return job, true
	}
}

func (scheduler *OutboundScheduler) execute(ctx context.Context, job OutboundJob) {
	attempts := 1
	if job.Retryable {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := job.Run(ctx); err == nil {
			scheduler.succeeded.Add(1)
			return
		}
		if attempt+1 >= attempts || ctx.Err() != nil {
			scheduler.failed.Add(1)
			return
		}
		scheduler.retried.Add(1)
		jitter := time.Duration(rand.Int64N(int64(scheduler.retryBase/2) + 1))
		delay := scheduler.retryBase*(1<<attempt) + jitter
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			scheduler.failed.Add(1)
			return
		case <-timer.C:
		}
	}
}
