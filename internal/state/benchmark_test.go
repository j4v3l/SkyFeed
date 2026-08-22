package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/rules"
	"github.com/j4v3l/SkyFeed/internal/source"
)

func BenchmarkReplayPipeline(b *testing.B) {
	now := time.Unix(1_700_000_000, 0)
	batch := domain.AircraftBatch{GeneratedAt: now, Aircraft: make([]domain.Aircraft, 1_000)}
	watchRules := make([]domain.WatchRule, 5_000)
	for index := range batch.Aircraft {
		batch.Aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index), Callsign: fmt.Sprintf("SKY%04d", index), Latitude: 40 + float64(index%100)/100, Longitude: -75 + float64(index%100)/100, HasPosition: true}
	}
	for index := range watchRules {
		watchRules[index] = domain.WatchRule{ID: int64(index + 1), Type: domain.RuleICAO, Value: fmt.Sprintf("%06X", index), Enabled: true, MinimumObservations: 2, Cooldown: time.Minute}
	}
	ruleEngine := rules.NewEngine(watchRules, nil)
	engine := NewEngine(nil)
	engine.applyReceiver(source.Frame[domain.Receiver]{FetchedAt: now, Value: domain.Receiver{Latitude: 40, Longitude: -75, HasPosition: true}}, 30*time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		engine.applyAircraft(source.Frame[domain.AircraftBatch]{FetchedAt: now, Value: batch}, time.Second)
		ruleEngine.Evaluate(engine.Current())
	}
}
