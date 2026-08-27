package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/j4v3l/SkyFeed/internal/buildinfo"
	"github.com/j4v3l/SkyFeed/internal/source/agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "skyfeed-agent:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "run" && args[0] != "enroll" && args[0] != "config-check" && args[0] != "version") {
		return fmt.Errorf("usage: skyfeed-agent run|enroll|config-check|version")
	}
	if args[0] == "version" {
		_, _ = fmt.Fprintln(os.Stdout, buildinfo.Current().String("skyfeed-agent"))
		return nil
	}
	serverURL := strings.TrimSpace(os.Getenv("SKYFEED_AGENT_SERVER_URL"))
	stateDir := strings.TrimSpace(os.Getenv("SKYFEED_AGENT_STATE_DIR"))
	if stateDir == "" {
		stateDir = "/var/lib/skyfeed-agent"
	}
	allowHTTP, err := strconv.ParseBool(valueOr(os.Getenv("SKYFEED_AGENT_ALLOW_PRIVATE_HTTP"), "false"))
	if err != nil {
		return fmt.Errorf("SKYFEED_AGENT_ALLOW_PRIVATE_HTTP: %w", err)
	}
	client, err := agent.NewClient(serverURL, stateDir, allowHTTP)
	if err != nil {
		return err
	}
	receiverURL, aircraftPoll, metadataPoll, err := agentSourceConfig()
	if err != nil {
		return err
	}
	if args[0] == "config-check" {
		_, _ = fmt.Fprintln(os.Stdout, "agent configuration valid")
		return nil
	}
	token, err := readEnrollmentToken()
	if err != nil && args[0] == "enroll" {
		return err
	}
	if args[0] == "enroll" {
		credential, err := client.Enroll(ctx, token)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "agent enrolled as %s\n", credential.FeederID)
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return agent.Run(ctx, agent.RuntimeConfig{
		ReceiverURL: receiverURL, AircraftPoll: aircraftPoll, MetadataPoll: metadataPoll, EnrollmentToken: token,
	}, client, logger)
}

func agentSourceConfig() (*url.URL, time.Duration, time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("SKYFEED_ADSB_BASE_URL"))
	receiverURL, err := url.Parse(raw)
	if err != nil || receiverURL.Host == "" || (receiverURL.Scheme != "http" && receiverURL.Scheme != "https") || receiverURL.User != nil || receiverURL.RawQuery != "" || receiverURL.Fragment != "" || !strings.HasSuffix(strings.TrimSuffix(path.Clean(receiverURL.Path), "/"), "/data") {
		return nil, 0, 0, fmt.Errorf("SKYFEED_ADSB_BASE_URL must be an absolute HTTP(S) URL ending in /data")
	}
	aircraftPoll, err := time.ParseDuration(valueOr(os.Getenv("SKYFEED_AIRCRAFT_POLL"), "1s"))
	if err != nil || aircraftPoll < 500*time.Millisecond || aircraftPoll > time.Minute {
		return nil, 0, 0, fmt.Errorf("SKYFEED_AIRCRAFT_POLL must be between 500ms and 1m")
	}
	metadataPoll, err := time.ParseDuration(valueOr(os.Getenv("SKYFEED_METADATA_POLL"), "30s"))
	if err != nil || metadataPoll < 5*time.Second || metadataPoll > 15*time.Minute {
		return nil, 0, 0, fmt.Errorf("SKYFEED_METADATA_POLL must be between 5s and 15m")
	}
	return receiverURL, aircraftPoll, metadataPoll, nil
}

func readEnrollmentToken() (string, error) {
	file := strings.TrimSpace(os.Getenv("SKYFEED_AGENT_ENROLLMENT_FILE"))
	if file == "" {
		return "", nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read SKYFEED_AGENT_ENROLLMENT_FILE: %w", err)
	}
	if len(data) > 512 {
		return "", fmt.Errorf("enrollment file is oversized")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("enrollment file is invalid")
	}
	return token, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
