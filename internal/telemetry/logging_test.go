package telemetry

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerHonorsLevelAndFormat(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(&output, "json", "info")
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	logger.Debug("hidden")
	logger.Info("visible", slog.String("event", "test"))
	if strings.Contains(output.String(), "hidden") || !strings.Contains(output.String(), `"event":"test"`) {
		t.Fatalf("unexpected log output: %s", output.String())
	}
}

func TestNewLoggerRejectsUnknownFormat(t *testing.T) {
	if _, err := NewLogger(&bytes.Buffer{}, "xml", "info"); err == nil {
		t.Fatal("expected unknown format error")
	}
}
