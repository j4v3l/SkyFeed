package discord

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestOutboundDashboardLatestWins(t *testing.T) {
	scheduler := NewOutboundScheduler(1, 1, 1, 1)
	var mu sync.Mutex
	run := ""
	for _, value := range []string{"first", "second", "latest"} {
		value := value
		if err := scheduler.Enqueue(context.Background(), OutboundJob{Priority: PriorityDashboard, Run: func(context.Context) error {
			mu.Lock()
			run = value
			mu.Unlock()
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = scheduler.Run(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for scheduler.Stats().Succeeded == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if run != "latest" {
		t.Fatalf("ran %q", run)
	}
	if scheduler.Stats().Coalesced != 2 {
		t.Fatalf("coalesced = %d", scheduler.Stats().Coalesced)
	}
}

func TestOutboundEmergencyHasReservedCapacity(t *testing.T) {
	scheduler := NewOutboundScheduler(1, 1, 1, 1)
	noop := func(context.Context) error { return nil }
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Key: "normal", Priority: PriorityAlert, Run: noop}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Key: "overflow", Priority: PriorityAlert, Run: noop}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected full alert queue, got %v", err)
	}
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Priority: PriorityEmergency, Run: noop}); err != nil {
		t.Fatalf("emergency capacity was not reserved: %v", err)
	}
}

func TestOutboundCoalescesAndRetries(t *testing.T) {
	scheduler := NewOutboundScheduler(1, 1, 2, 1)
	scheduler.retryBase = time.Millisecond
	attempts := 0
	job := OutboundJob{Key: "alert:1", Priority: PriorityAlert, Retryable: true, Run: func(context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("transient")
		}
		return nil
	}}
	if err := scheduler.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = scheduler.Run(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for scheduler.Stats().Succeeded == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	stats := scheduler.Stats()
	if attempts != 2 || stats.Retried != 1 || stats.Coalesced != 1 {
		t.Fatalf("attempts=%d stats=%+v", attempts, stats)
	}
}

func TestOutboundCriticalLaneBypassesSlowRetryingBackground(t *testing.T) {
	scheduler := NewOutboundScheduler(1, 1, 1, 1)
	scheduler.retryBase = 50 * time.Millisecond
	backgroundStarted := make(chan struct{})
	releaseBackground := make(chan struct{})
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Key: "report", Priority: PriorityReport, Retryable: true, Run: func(context.Context) error {
		select {
		case <-backgroundStarted:
		default:
			close(backgroundStarted)
		}
		<-releaseBackground
		return errors.New("slow failure")
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	<-backgroundStarted
	criticalRan := make(chan struct{})
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Priority: PriorityEmergency, Run: func(context.Context) error {
		close(criticalRan)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-criticalRan:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("critical job waited behind background retry")
	}
	close(releaseBackground)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOutboundInteractionDoesNotWaitForRetryingEmergency(t *testing.T) {
	scheduler := NewOutboundScheduler(1, 1, 1, 1)
	emergencyStarted := make(chan struct{})
	releaseEmergency := make(chan struct{})
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Priority: PriorityEmergency, Retryable: true, Run: func(context.Context) error {
		select {
		case <-emergencyStarted:
		default:
			close(emergencyStarted)
		}
		<-releaseEmergency
		return errors.New("retry emergency")
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	<-emergencyStarted
	interactionRan := make(chan struct{})
	if err := scheduler.Enqueue(context.Background(), OutboundJob{Priority: PriorityInteraction, Run: func(context.Context) error {
		close(interactionRan)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interactionRan:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("interaction waited behind emergency retry")
	}
	close(releaseEmergency)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestModerationLogBackoffIsBounded(t *testing.T) {
	if got := moderationLogBackoff(0); got != 5*time.Second {
		t.Fatalf("first retry=%s", got)
	}
	if got := moderationLogBackoff(100); got != 30*time.Minute {
		t.Fatalf("bounded retry=%s", got)
	}
}
