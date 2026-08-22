package telemetry

import (
	"fmt"
	"io"
	"log/slog"
)

func NewLogger(output io.Writer, format, level string) (*slog.Logger, error) {
	var minimum slog.Level
	if err := minimum.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("parse log level: %w", err)
	}

	options := &slog.HandlerOptions{Level: minimum}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(output, options)
	case "text":
		handler = slog.NewTextHandler(output, options)
	default:
		return nil, fmt.Errorf("unsupported log format %q", format)
	}
	return slog.New(handler), nil
}
