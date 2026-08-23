// Command replay repeatedly serves sanitized readsb fixtures through the real
// HTTP source adapter and state engine. It never contacts the live receiver.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/source"
	"github.com/j4v3l/SkyFeed/internal/source/readsb"
	"github.com/j4v3l/SkyFeed/internal/state"
)

func main() {
	fixtureDirectory := flag.String("fixtures", "test/fixtures/readsb", "sanitized fixture directory")
	iterations := flag.Int("iterations", 100, "aircraft snapshots to publish")
	cadence := flag.Duration("cadence", 100*time.Millisecond, "replay cadence")
	flag.Parse()
	if *iterations < 1 || *cadence <= 0 {
		fmt.Fprintln(os.Stderr, "iterations and cadence must be positive")
		os.Exit(2)
	}

	payloads := make(map[string][]byte, 3)
	for _, name := range []string{"aircraft", "receiver", "stats"} {
		data, err := os.ReadFile(filepath.Join(*fixtureDirectory, name+".json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s fixture: %v\n", name, err)
			os.Exit(1)
		}
		payloads["/data/"+name+".json"] = data
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data, ok := payloads[request.URL.Path]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(data)
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL + "/data")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var published atomic.Int64
	var lastAircraft atomic.Int64
	var lastFetch atomic.Int64
	engine := state.NewEngine(func(snapshot *domain.Snapshot) {
		fetchedAt := snapshot.FetchedAt.UnixNano()
		if fetchedAt != 0 && lastFetch.Swap(fetchedAt) != fetchedAt {
			lastAircraft.Store(int64(len(snapshot.Aircraft)))
			if published.Add(1) >= int64(*iterations) {
				cancel()
			}
		}
	})
	start := time.Now()
	if err := engine.Run(ctx, source.NewSet(readsb.NewClient(baseURL, time.Second)), *cadence, *cadence); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("snapshots=%d aircraft=%d elapsed=%s\n", published.Load(), lastAircraft.Load(), time.Since(start))
}
