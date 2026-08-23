package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
)

func TestCLIVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := CLI{Stdout: &stdout, Stderr: &stderr}
	if exit := cli.Execute(context.Background(), []string{"version"}); exit != ExitSuccess {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout.String(), "skyfeed version=") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCLIConfigCheck(t *testing.T) {
	values := map[string]string{
		"SKYFEED_DISCORD_TOKEN_FILE":     "/run/secrets/discord_token",
		"SKYFEED_DISCORD_APPLICATION_ID": "1",
		"SKYFEED_DISCORD_GUILD_ID":       "2",
		"SKYFEED_ADSB_BASE_URL":          "http://receiver.invalid/data",
	}
	lookup := func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
	var stdout, stderr bytes.Buffer
	cli := CLI{
		Stdout:    &stdout,
		Stderr:    &stderr,
		LookupEnv: config.LookupEnv(lookup),
		ReadFile:  func(string) ([]byte, error) { return []byte("synthetic.token"), nil },
	}
	if exit := cli.Execute(context.Background(), []string{"config", "check"}); exit != ExitSuccess {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	if stdout.String() != "configuration valid\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCLIUsage(t *testing.T) {
	var stderr bytes.Buffer
	cli := CLI{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if exit := cli.Execute(context.Background(), nil); exit != ExitUsage {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCheckConfiguredSourcesProbesEveryAircraftProviderAndReadsbMetadata(t *testing.T) {
	now := time.Unix(1_787_414_400, 0)
	readsbSource := &sourceCheckStub{
		id: domain.ProviderReadsb,
		aircraft: source.Frame[domain.AircraftBatch]{
			FetchedAt: now,
			Value: domain.AircraftBatch{
				Aircraft: []domain.Aircraft{{ICAO: "ABC123"}, {ICAO: "DEF456"}},
			},
		},
		receiver: source.Frame[domain.Receiver]{
			FetchedAt: now,
			Value:     domain.Receiver{Version: "synthetic"},
		},
		stats: source.Frame[domain.Statistics]{
			FetchedAt: now,
			Value:     domain.Statistics{Messages: 42},
		},
	}
	fallback := &sourceCheckStub{
		id: domain.ProviderAirplanesLive,
		aircraft: source.Frame[domain.AircraftBatch]{
			FetchedAt: now,
			Value: domain.AircraftBatch{
				Aircraft: []domain.Aircraft{{ICAO: "123ABC"}},
			},
		},
	}

	result, err := checkConfiguredSources(
		context.Background(),
		[]source.AircraftSource{readsbSource, fallback},
		readsbSource,
		readsbSource,
		domain.ProviderReadsb,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReadsbAircraft != 2 || result.ReceiverVersion != "synthetic" || result.Messages != 42 {
		t.Fatalf("result = %+v", result)
	}
	if strings.Join(result.AircraftCounts, ",") != "readsb:2,airplanes-live:1" {
		t.Fatalf("provider counts = %v", result.AircraftCounts)
	}
	if readsbSource.aircraftCalls != 1 || fallback.aircraftCalls != 1 ||
		readsbSource.receiverCalls != 1 || readsbSource.statsCalls != 1 {
		t.Fatalf(
			"calls readsb_aircraft=%d fallback=%d receiver=%d stats=%d",
			readsbSource.aircraftCalls,
			fallback.aircraftCalls,
			readsbSource.receiverCalls,
			readsbSource.statsCalls,
		)
	}
}

type sourceCheckStub struct {
	id            domain.ProviderID
	aircraft      source.Frame[domain.AircraftBatch]
	receiver      source.Frame[domain.Receiver]
	stats         source.Frame[domain.Statistics]
	aircraftCalls int
	receiverCalls int
	statsCalls    int
}

func (stub *sourceCheckStub) ProviderID() domain.ProviderID {
	return stub.id
}

func (stub *sourceCheckStub) Capabilities() domain.Capabilities {
	if stub.id == domain.ProviderAirplanesLive {
		return domain.CapabilitiesOf(domain.CapabilityAircraft)
	}
	return domain.CapabilitiesOf(
		domain.CapabilityAircraft,
		domain.CapabilityReceiver,
		domain.CapabilityStatistics,
	)
}

func (stub *sourceCheckStub) FetchAircraft(context.Context) (source.Frame[domain.AircraftBatch], error) {
	stub.aircraftCalls++
	return stub.aircraft, nil
}

func (stub *sourceCheckStub) FetchReceiver(context.Context) (source.Frame[domain.Receiver], error) {
	stub.receiverCalls++
	return stub.receiver, nil
}

func (stub *sourceCheckStub) FetchStats(context.Context) (source.Frame[domain.Statistics], error) {
	stub.statsCalls++
	return stub.stats, nil
}
