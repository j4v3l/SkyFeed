package app

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
)

func TestRunStopsAfterCancellation(t *testing.T) {
	sourceURL, err := url.Parse("http://receiver.invalid/data")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ADSB:       config.ADSB{BaseURL: sourceURL},
		HealthAddr: "127.0.0.1:0",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("application did not stop after cancellation")
	}
}
