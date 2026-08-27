package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestVersionDoesNotRequireRuntimeConfiguration(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })

	if err := execute(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output := make([]byte, 256)
	count, err := read.Read(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(output[:count]); !strings.Contains(got, "skyfeed-agent version=") {
		t.Fatalf("version output = %q", got)
	}
}
