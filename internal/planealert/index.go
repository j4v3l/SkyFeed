package planealert

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage"
)

type ReferenceStore interface {
	PlaneAlertReferenceCount(context.Context) (int, error)
	PlaneAlertCommitHash(context.Context) (string, error)
	ReplacePlaneAlertReference(context.Context, []storage.PlaneAlertReference, string) error
	PlaneAlertReferences(context.Context) ([]storage.PlaneAlertReference, error)
}

type Index struct {
	loader   *Loader
	store    ReferenceStore
	logger   *slog.Logger
	interval time.Duration
	custom   bool

	mu      sync.RWMutex
	records map[string]Record
	loaded  atomic.Bool
}

func NewIndex(loader *Loader, store ReferenceStore, interval time.Duration, logger *slog.Logger) *Index {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Index{
		loader:   loader,
		store:    store,
		logger:   logger,
		interval: interval,
		custom:   loader != nil && isCustomURL(loader.url),
		records:  make(map[string]Record),
	}
}

func (index *Index) Run(ctx context.Context) error {
	if err := index.refresh(ctx, true); err != nil {
		index.logger.Error("plane-alert-db initial load failed", "component", "planealert", "event", "load_failure", "error", err)
	}
	ticker := time.NewTicker(index.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := index.refresh(ctx, false); err != nil {
				index.logger.Warn("plane-alert-db refresh failed", "component", "planealert", "event", "refresh_failure", "error", err)
			}
		}
	}
}

func (index *Index) Lookup(icao string) (Record, bool) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	record, ok := index.records[normalizeICAO(icao)]
	return record, ok
}

func (index *Index) Len() int {
	index.mu.RLock()
	defer index.mu.RUnlock()
	return len(index.records)
}

func (index *Index) refresh(ctx context.Context, force bool) error {
	if index.store == nil || index.loader == nil {
		return nil
	}
	loadContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if !force {
		count, err := index.store.PlaneAlertReferenceCount(loadContext)
		if err != nil {
			return err
		}
		if count == 0 {
			force = true
		} else if index.custom {
			return index.loadFromStore(loadContext)
		}
		existingHash, err := index.store.PlaneAlertCommitHash(loadContext)
		if err != nil {
			return err
		}
		latestHash, err := index.loader.LatestCommitHash(loadContext)
		if err == nil && latestHash != "" && latestHash == existingHash {
			return index.loadFromStore(loadContext)
		}
	}
	records, commitHash, err := index.loader.FetchRecords(loadContext)
	if err != nil {
		if !force {
			return index.loadFromStore(loadContext)
		}
		return err
	}
	stored := make([]storage.PlaneAlertReference, 0, len(records))
	for _, record := range records {
		stored = append(stored, StorageReference(record, commitHash))
	}
	if err := index.store.ReplacePlaneAlertReference(loadContext, stored, commitHash); err != nil {
		return err
	}
	index.logger.Info("plane-alert-db reference updated", "component", "planealert", "event", "reference_refresh", "records", len(stored), "commit_hash", commitHash)
	return index.setRecords(records)
}

func (index *Index) loadFromStore(ctx context.Context) error {
	stored, err := index.store.PlaneAlertReferences(ctx)
	if err != nil {
		return err
	}
	records := make([]Record, 0, len(stored))
	for _, item := range stored {
		records = append(records, RecordFromStorage(item))
	}
	return index.setRecords(records)
}

func (index *Index) setRecords(records []Record) error {
	next := make(map[string]Record, len(records))
	for _, record := range records {
		next[normalizeICAO(record.ICAO)] = record
	}
	index.mu.Lock()
	index.records = next
	index.mu.Unlock()
	index.loaded.Store(true)
	return nil
}

func normalizeICAO(icao string) string {
	return strings.ToUpper(strings.TrimSpace(icao))
}
