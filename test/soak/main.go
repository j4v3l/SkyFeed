package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/rules"
)

func main() {
	duration := flag.Duration("duration", 24*time.Hour, "soak duration")
	cadence := flag.Duration("cadence", time.Second, "synthetic snapshot cadence")
	aircraftCount := flag.Int("aircraft", 1_000, "aircraft per snapshot")
	flag.Parse()

	aircraft := make([]domain.Aircraft, *aircraftCount)
	watchRules := make([]domain.WatchRule, *aircraftCount)
	for index := range aircraft {
		icao := fmt.Sprintf("%06X", index)
		aircraft[index] = domain.Aircraft{ICAO: icao, Callsign: fmt.Sprintf("SKY%04d", index)}
		watchRules[index] = domain.WatchRule{ID: int64(index + 1), Type: domain.RuleICAO, Value: icao, Enabled: true, MinimumObservations: 2, Cooldown: time.Hour}
	}
	engine := rules.NewEngine(watchRules, nil)
	byICAO := make(map[string]int, len(aircraft))
	for index := range aircraft {
		byICAO[aircraft[index].ICAO] = index
	}
	// Warm one full evaluation so durable rule state is not mistaken for a
	// leak during the measured window.
	engine.Evaluate(&domain.Snapshot{PublishedAt: time.Now(), Aircraft: aircraft, ByICAO: byICAO})
	started := time.Now()
	deadline := started.Add(*duration)
	startGoroutines := runtime.NumGoroutine()
	startFDs := openFileDescriptors()
	var startMemory runtime.MemStats
	runtime.ReadMemStats(&startMemory)
	iterations := 0
	ticker := time.NewTicker(*cadence)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		now := time.Now()
		engine.Evaluate(&domain.Snapshot{PublishedAt: now, Aircraft: aircraft, ByICAO: byICAO})
		iterations++
		<-ticker.C
	}
	runtime.GC()
	var endMemory runtime.MemStats
	runtime.ReadMemStats(&endMemory)
	endFDs := openFileDescriptors()
	fmt.Printf("duration=%s iterations=%d goroutines_start=%d goroutines_end=%d fds_start=%d fds_end=%d heap_start=%d heap_end=%d\n", time.Since(started), iterations, startGoroutines, runtime.NumGoroutine(), startFDs, endFDs, startMemory.HeapAlloc, endMemory.HeapAlloc)
	if runtime.NumGoroutine() > startGoroutines+2 {
		panic("goroutine growth exceeded tolerance")
	}
	if startFDs >= 0 && endFDs > startFDs+2 {
		panic("file-descriptor growth exceeded tolerance")
	}
	if endMemory.HeapAlloc > startMemory.HeapAlloc+16<<20 {
		panic("heap growth exceeded tolerance")
	}
}

func openFileDescriptors() int {
	for _, directory := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(directory)
		if err == nil {
			return len(entries)
		}
	}
	return -1
}
