package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestAdminDigestStateAndRouteTrafficCounts(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "skyfeed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	last, err := store.AdminDigestLastRun(ctx, 42)
	if err != nil || !last.IsZero() {
		t.Fatalf("last=%v err=%v", last, err)
	}
	now := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	if err := store.MarkAdminDigestRun(ctx, 42, now); err != nil {
		t.Fatal(err)
	}
	last, err = store.AdminDigestLastRun(ctx, 42)
	if err != nil || !last.Equal(now) {
		t.Fatalf("last=%v err=%v", last, err)
	}
	counts, err := store.RouteTrafficCounts(ctx, 42, now.Add(-24*time.Hour))
	if err != nil || counts.CatalogEntries != 0 || counts.Sightings != 0 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	_ = storage.RouteTrafficCounts{}
}
