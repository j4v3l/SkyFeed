package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestFeederEnrollmentSequenceAndRevocation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "feeders.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	feeder := storage.Feeder{GuildID: 42, OwnerUserID: 7, Descriptor: domain.FeederDescriptor{
		ID: "community-one", DisplayName: "Community One", PublicArea: "Palm Beach", AirportICAO: "KPBI",
		WeatherStationICAO: "KDJT", SourceKind: domain.FeederSourceAgent, Enabled: true,
	}, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertFeeder(ctx, feeder); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("one-time-high-entropy-token"))
	if err := store.CreateFeederEnrollment(ctx, storage.FeederEnrollment{TokenHash: tokenHash[:], FeederID: feeder.Descriptor.ID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	publicKey := bytes.Repeat([]byte{1}, 32)
	enrolled, err := store.ConsumeFeederEnrollment(ctx, tokenHash[:], publicKey, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if enrolled.Descriptor.WeatherStationICAO != "KDJT" || enrolled.OwnerUserID != 7 || !bytes.Equal(enrolled.PublicKey, publicKey) {
		t.Fatalf("enrolled feeder = %+v", enrolled)
	}
	if _, err := store.ConsumeFeederEnrollment(ctx, tokenHash[:], publicKey, now.Add(2*time.Minute)); !errors.Is(err, storage.ErrEnrollmentInvalid) {
		t.Fatalf("reused enrollment error = %v", err)
	}
	payloadHash := sha256.Sum256([]byte("payload-one"))
	if result, err := store.AcceptFeederSequence(ctx, feeder.Descriptor.ID, 1, payloadHash[:], now.Add(3*time.Minute)); err != nil || result != storage.SequenceAccepted {
		t.Fatalf("first sequence result=%v err=%v", result, err)
	}
	if result, err := store.AcceptFeederSequence(ctx, feeder.Descriptor.ID, 1, payloadHash[:], now.Add(4*time.Minute)); err != nil || result != storage.SequenceDuplicate {
		t.Fatalf("duplicate sequence result=%v err=%v", result, err)
	}
	altered := sha256.Sum256([]byte("altered"))
	if _, err := store.AcceptFeederSequence(ctx, feeder.Descriptor.ID, 1, altered[:], now.Add(5*time.Minute)); !errors.Is(err, storage.ErrSequenceRejected) {
		t.Fatalf("altered replay error = %v", err)
	}
	if err := store.RevokeFeeder(ctx, feeder.Descriptor.ID, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptFeederSequence(ctx, feeder.Descriptor.ID, 2, altered[:], now.Add(7*time.Minute)); !errors.Is(err, storage.ErrSequenceRejected) {
		t.Fatalf("revoked sequence error = %v", err)
	}
}

func TestEnsureGuildCreatesLocalFeeder(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 99); err != nil {
		t.Fatal(err)
	}
	feeder, err := store.Feeder(ctx, domain.FeederLocal)
	if err != nil {
		t.Fatal(err)
	}
	if feeder.GuildID != 99 || feeder.Descriptor.SourceKind != domain.FeederSourceLocal || !feeder.Descriptor.Enabled {
		t.Fatalf("local feeder = %+v", feeder)
	}
}

func TestExpiredEnrollmentCannotBeConsumed(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "expired.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	feeder := storage.Feeder{GuildID: 42, Descriptor: domain.FeederDescriptor{ID: "expired-agent", DisplayName: "Expired", SourceKind: domain.FeederSourceAgent, Enabled: true}}
	if err := store.UpsertFeeder(ctx, feeder); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("expired-one-time-enrollment"))
	if err := store.CreateFeederEnrollment(ctx, storage.FeederEnrollment{TokenHash: tokenHash[:], FeederID: feeder.Descriptor.ID, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeFeederEnrollment(ctx, tokenHash[:], bytes.Repeat([]byte{1}, 32), now.Add(time.Minute)); !errors.Is(err, storage.ErrEnrollmentInvalid) {
		t.Fatalf("expired enrollment error = %v", err)
	}
}
