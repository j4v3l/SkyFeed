package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var forbiddenFixturePatterns = map[string]*regexp.Regexp{
	"private IPv4 address": regexp.MustCompile(`\b(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})\b`),
	"Discord token":        regexp.MustCompile(`[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{20,}`),
}

func TestReadsbFixturesAreSyntheticJSON(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "fixtures", "readsb", "*.json"))
	if err != nil {
		t.Fatalf("list fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("receiver gate is open only after sanitized JSON fixtures exist")
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if len(data) == 0 || !json.Valid(data) {
				t.Fatal("fixture must contain valid, non-empty JSON")
			}
			if strings.Contains(strings.ToLower(string(data)), ".local") {
				t.Fatal("fixture contains a .local hostname")
			}
			for name, pattern := range forbiddenFixturePatterns {
				if pattern.Match(data) {
					t.Fatalf("fixture contains %s", name)
				}
			}
		})
	}
}
