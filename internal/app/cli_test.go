package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/j4v3l/SkyFeed/internal/config"
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
