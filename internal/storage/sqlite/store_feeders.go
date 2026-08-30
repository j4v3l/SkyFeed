package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const feederSelect = `SELECT id, guild_id, display_name, public_area, airport_icao, weather_station_icao, latitude, longitude, has_center, source_kind, enabled, owner_user_id, public_key, last_sequence, last_payload_hash, last_seen_at, created_at, updated_at FROM feeders`

func (store *Store) UpsertFeeder(ctx context.Context, value storage.Feeder) error {
	descriptor := value.Descriptor
	id, err := domain.NormalizeFeederID(string(descriptor.ID))
	if err != nil || id == domain.FeederAll {
		return errors.New("invalid feeder ID")
	}
	name, err := domain.NormalizeFeederDisplayName(descriptor.DisplayName)
	if err != nil {
		return err
	}
	if descriptor.SourceKind != domain.FeederSourceLocal && descriptor.SourceKind != domain.FeederSourceAgent {
		return errors.New("feeder source kind must be local or agent")
	}
	now := value.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	created := value.CreatedAt.UTC()
	if created.IsZero() {
		created = now
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO feeders(id, guild_id, display_name, public_area, airport_icao, weather_station_icao, latitude, longitude, has_center, source_kind, enabled, owner_user_id, public_key, last_sequence, last_payload_hash, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name, public_area=excluded.public_area, airport_icao=excluded.airport_icao, weather_station_icao=excluded.weather_station_icao, latitude=excluded.latitude, longitude=excluded.longitude, has_center=excluded.has_center, enabled=excluded.enabled, owner_user_id=excluded.owner_user_id, updated_at=excluded.updated_at`,
		id, value.GuildID, name, strings.TrimSpace(descriptor.PublicArea), strings.ToUpper(strings.TrimSpace(descriptor.AirportICAO)), strings.ToUpper(strings.TrimSpace(descriptor.WeatherStationICAO)), nullableCoordinate(descriptor.Latitude, descriptor.HasCenter), nullableCoordinate(descriptor.Longitude, descriptor.HasCenter), boolToInt(descriptor.HasCenter), descriptor.SourceKind, boolToInt(descriptor.Enabled), value.OwnerUserID, nullableBytes(value.PublicKey), value.LastSequence, nullableBytes(value.LastPayloadHash), nullableTime(value.LastSeenAt), formatTime(created), formatTime(now))
	return wrap("upsert feeder", err)
}

func nullableCoordinate(value float64, present bool) any {
	if !present {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (store *Store) Feeder(ctx context.Context, id domain.FeederID) (storage.Feeder, error) {
	return scanFeeder(store.db.QueryRowContext(ctx, feederSelect+` WHERE id=?`, id))
}

func (store *Store) Feeders(ctx context.Context, guildID uint64, limit int) ([]storage.Feeder, error) {
	limit = min(max(limit, 1), 250)
	rows, err := store.db.QueryContext(ctx, feederSelect+` WHERE guild_id=? ORDER BY display_name, id LIMIT ?`, guildID, limit)
	if err != nil {
		return nil, fmt.Errorf("list feeders: %w", err)
	}
	defer rows.Close()
	result := make([]storage.Feeder, 0, min(limit, 16))
	for rows.Next() {
		value, scanErr := scanFeeder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func scanFeeder(scanner rowScanner) (storage.Feeder, error) {
	var value storage.Feeder
	var id, sourceKind string
	var latitude, longitude sql.NullFloat64
	var hasCenter, enabled int
	var publicKey, payloadHash []byte
	var lastSeen sql.NullString
	var created, updated string
	err := scanner.Scan(&id, &value.GuildID, &value.Descriptor.DisplayName, &value.Descriptor.PublicArea, &value.Descriptor.AirportICAO, &value.Descriptor.WeatherStationICAO, &latitude, &longitude, &hasCenter, &sourceKind, &enabled, &value.OwnerUserID, &publicKey, &value.LastSequence, &payloadHash, &lastSeen, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Feeder{}, ErrNotFound
	}
	if err != nil {
		return storage.Feeder{}, fmt.Errorf("scan feeder: %w", err)
	}
	value.Descriptor.ID = domain.FeederID(id)
	value.Descriptor.SourceKind = domain.FeederSourceKind(sourceKind)
	value.Descriptor.Enabled = enabled == 1
	value.Descriptor.HasCenter = hasCenter == 1 && latitude.Valid && longitude.Valid
	if value.Descriptor.HasCenter {
		value.Descriptor.Latitude = latitude.Float64
		value.Descriptor.Longitude = longitude.Float64
	}
	value.PublicKey = append([]byte(nil), publicKey...)
	value.LastPayloadHash = append([]byte(nil), payloadHash...)
	if lastSeen.Valid {
		value.LastSeenAt, err = parseTime(lastSeen.String)
	}
	if err == nil {
		value.CreatedAt, err = parseTime(created)
	}
	if err == nil {
		value.UpdatedAt, err = parseTime(updated)
	}
	return value, err
}

func (store *Store) CreateFeederEnrollment(ctx context.Context, enrollment storage.FeederEnrollment) error {
	if len(enrollment.TokenHash) != 32 {
		return errors.New("enrollment token hash must be 32 bytes")
	}
	if !enrollment.FeederID.Valid() || enrollment.FeederID == domain.FeederAll {
		return errors.New("invalid enrollment feeder ID")
	}
	created := enrollment.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if !enrollment.ExpiresAt.After(created) {
		return errors.New("enrollment expiry must be after creation")
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO feeder_enrollments(token_hash, feeder_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, enrollment.TokenHash, enrollment.FeederID, formatTime(enrollment.ExpiresAt.UTC()), formatTime(created))
	return wrap("create feeder enrollment", err)
}

func (store *Store) ConsumeFeederEnrollment(ctx context.Context, tokenHash, publicKey []byte, now time.Time) (storage.Feeder, error) {
	if len(tokenHash) != 32 || len(publicKey) != 32 {
		return storage.Feeder{}, storage.ErrEnrollmentInvalid
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.Feeder{}, fmt.Errorf("begin feeder enrollment: %w", err)
	}
	defer transaction.Rollback()
	var feederID, expires string
	var consumed, revoked sql.NullString
	err = transaction.QueryRowContext(ctx, `SELECT feeder_id, expires_at, consumed_at, revoked_at FROM feeder_enrollments WHERE token_hash=?`, tokenHash).Scan(&feederID, &expires, &consumed, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.Feeder{}, storage.ErrEnrollmentInvalid
		}
		return storage.Feeder{}, fmt.Errorf("find feeder enrollment: %w", err)
	}
	expiresAt, err := parseTime(expires)
	if err != nil || consumed.Valid || revoked.Valid || !now.UTC().Before(expiresAt) {
		return storage.Feeder{}, storage.ErrEnrollmentInvalid
	}
	result, err := transaction.ExecContext(ctx, `UPDATE feeder_enrollments SET consumed_at=? WHERE token_hash=? AND consumed_at IS NULL AND revoked_at IS NULL`, formatTime(now.UTC()), tokenHash)
	if err != nil {
		return storage.Feeder{}, fmt.Errorf("consume feeder enrollment: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return storage.Feeder{}, storage.ErrEnrollmentInvalid
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE feeders SET public_key=?, last_sequence=0, last_payload_hash=NULL, enabled=1, updated_at=? WHERE id=?`, publicKey, formatTime(now.UTC()), feederID); err != nil {
		return storage.Feeder{}, fmt.Errorf("activate feeder enrollment: %w", err)
	}
	if err = transaction.Commit(); err != nil {
		return storage.Feeder{}, fmt.Errorf("commit feeder enrollment: %w", err)
	}
	return store.Feeder(ctx, domain.FeederID(feederID))
}

func (store *Store) RevokeFeeder(ctx context.Context, id domain.FeederID, now time.Time) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin feeder revocation: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE feeders SET enabled=0, public_key=NULL, last_payload_hash=NULL, updated_at=? WHERE id=? AND source_kind='agent'`, formatTime(now.UTC()), id)
	if err == nil {
		_, err = transaction.ExecContext(ctx, `UPDATE feeder_enrollments SET revoked_at=? WHERE feeder_id=? AND consumed_at IS NULL AND revoked_at IS NULL`, formatTime(now.UTC()), id)
	}
	if err != nil {
		_ = transaction.Rollback()
		return fmt.Errorf("revoke feeder: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		_ = transaction.Rollback()
		return ErrNotFound
	}
	return transaction.Commit()
}

func (store *Store) AcceptFeederSequence(ctx context.Context, id domain.FeederID, sequence uint64, payloadHash []byte, seenAt time.Time) (storage.SequenceAcceptance, error) {
	if sequence == 0 || len(payloadHash) != 32 {
		return storage.SequenceRejected, storage.ErrSequenceRejected
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.SequenceRejected, fmt.Errorf("begin feeder sequence: %w", err)
	}
	defer transaction.Rollback()
	var previous uint64
	var previousHash []byte
	var enabled int
	var publicKey []byte
	err = transaction.QueryRowContext(ctx, `SELECT last_sequence, last_payload_hash, enabled, public_key FROM feeders WHERE id=?`, id).Scan(&previous, &previousHash, &enabled, &publicKey)
	if errors.Is(err, sql.ErrNoRows) || enabled != 1 || len(publicKey) != 32 {
		return storage.SequenceRejected, storage.ErrSequenceRejected
	}
	if err != nil {
		return storage.SequenceRejected, fmt.Errorf("read feeder sequence: %w", err)
	}
	switch {
	case sequence < previous:
		return storage.SequenceRejected, storage.ErrSequenceRejected
	case sequence == previous:
		if bytes.Equal(payloadHash, previousHash) {
			return storage.SequenceDuplicate, nil
		}
		return storage.SequenceRejected, storage.ErrSequenceRejected
	}
	result, err := transaction.ExecContext(ctx, `UPDATE feeders SET last_sequence=?, last_payload_hash=?, last_seen_at=?, updated_at=? WHERE id=? AND last_sequence=?`, sequence, payloadHash, formatTime(seenAt.UTC()), formatTime(seenAt.UTC()), id, previous)
	if err != nil {
		return storage.SequenceRejected, fmt.Errorf("update feeder sequence: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return storage.SequenceRejected, storage.ErrSequenceRejected
	}
	if err := transaction.Commit(); err != nil {
		return storage.SequenceRejected, fmt.Errorf("commit feeder sequence: %w", err)
	}
	return storage.SequenceAccepted, nil
}
