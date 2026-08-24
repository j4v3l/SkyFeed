package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (store *Store) AdminDigestLastRun(ctx context.Context, guildID uint64) (time.Time, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `SELECT last_run_at FROM admin_digest_state WHERE guild_id=?`, guildID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("admin digest last run: %w", err)
	}
	return parseTime(raw)
}

func (store *Store) MarkAdminDigestRun(ctx context.Context, guildID uint64, at time.Time) error {
	at = at.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_digest_state(guild_id, last_run_at) VALUES (?, ?)
ON CONFLICT(guild_id) DO UPDATE SET last_run_at=excluded.last_run_at`, guildID, formatTime(at))
	return wrap("mark admin digest run", err)
}
