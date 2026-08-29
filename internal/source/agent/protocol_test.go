package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/state"
	"github.com/j4v3l/SkyFeed/internal/storage"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
	"github.com/klauspost/compress/zstd"
)

func TestSnapshotRoundTripRedactsReceiverCoordinates(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	snapshot := &domain.Snapshot{
		SourceGeneratedAt: now, FetchedAt: now, ActiveProvider: domain.ProviderReadsb,
		Receiver: domain.Receiver{Latitude: 26.7, Longitude: -80.1, HasPosition: true},
		Aircraft: []domain.Aircraft{{ICAO: "ABC123", Latitude: 26.8, Longitude: -80, HasPosition: true, HasDistance: true, DistanceNM: 4.2, BearingDegrees: 91}},
		Health:   domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}},
	}
	payload, err := EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSnapshot("community-one", payload, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Receiver.HasPosition || decoded.Receiver.Latitude != 0 || decoded.Receiver.Longitude != 0 {
		t.Fatalf("private receiver position leaked: %+v", decoded.Receiver)
	}
	if !decoded.Aircraft[0].HasPosition || decoded.Aircraft[0].Latitude == 0 {
		t.Fatalf("public aircraft position removed: %+v", decoded.Aircraft[0])
	}
	if decoded.Aircraft[0].HasDistance || decoded.Aircraft[0].DistanceNM != 0 || decoded.Aircraft[0].BearingDegrees != 0 {
		t.Fatalf("private receiver-relative values leaked: %+v", decoded.Aircraft[0])
	}
}

func TestApplyPublicCenterRecomputesOnlyApprovedDistance(t *testing.T) {
	snapshot := &domain.Snapshot{Receiver: domain.Receiver{HasPosition: true, Latitude: 1, Longitude: 2}, Aircraft: []domain.Aircraft{{
		ICAO: "ABC123", HasPosition: true, Latitude: 26.1, Longitude: -80.1, HasDistance: true, DistanceNM: 999,
	}}}
	applyPublicCenter(snapshot, domain.FeederDescriptor{HasCenter: true, Latitude: 26, Longitude: -80})
	if snapshot.Receiver.HasPosition || !snapshot.Aircraft[0].HasDistance || snapshot.Aircraft[0].DistanceNM <= 0 || snapshot.Aircraft[0].DistanceNM >= 999 {
		t.Fatalf("public-center snapshot = %+v", snapshot)
	}
	applyPublicCenter(snapshot, domain.FeederDescriptor{})
	if snapshot.Aircraft[0].HasDistance {
		t.Fatalf("unapproved center retained distance: %+v", snapshot.Aircraft[0])
	}
}

func TestEnvelopeRejectsClockSkewAndInvalidSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, err := SignEnvelope(privateKey, "community-one", 1, now.Add(-3*time.Minute), []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEnvelope(publicKey, envelope, now, 2*time.Minute); !errors.Is(err, ErrClock) {
		t.Fatalf("clock skew error = %v", err)
	}
	envelope.SentAt = now
	envelope.Signature[0] ^= 0xff
	if _, err := VerifyEnvelope(publicKey, envelope, now, 2*time.Minute); !errors.Is(err, ErrSignature) {
		t.Fatalf("signature error = %v", err)
	}
}

func TestDecodeRejectsDuplicateAircraftAndDecompressionBomb(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	payload, err := EncodeSnapshot(&domain.Snapshot{Aircraft: []domain.Aircraft{{ICAO: "ABC123"}, {ICAO: "ABC123"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSnapshot("community-one", payload, now); err == nil {
		t.Fatal("duplicate ICAO accepted")
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	bomb := encoder.EncodeAll(make([]byte, MaxDecompressedBytes+1), nil)
	encoder.Close()
	if len(bomb) > MaxCompressedBytes {
		t.Fatalf("test bomb compressed to %d bytes", len(bomb))
	}
	if _, err := DecodeSnapshot("community-one", bomb, now); !errors.Is(err, ErrPayload) {
		t.Fatalf("decompression bomb error = %v", err)
	}
}

func TestIngressReturnsTooManyRequestsWhenVerificationQueueIsFull(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := state.NewFeederManager(time.Second)
	ingress, err := NewIngressServer(IngressConfig{Addr: "127.0.0.1:0", Workers: 1, Queue: 1}, store, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(SignedEnvelope{Version: ProtocolVersion, FeederID: "community-one", Sequence: 1, SentAt: time.Now(), Payload: []byte("x"), Signature: make([]byte, ed25519.SignatureSize)})
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		request := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshots", bytes.NewReader(body))
		ingress.Handler().ServeHTTP(httptest.NewRecorder(), request)
	}()
	deadline := time.Now().Add(time.Second)
	for len(ingress.jobs) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshots", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	ingress.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("queue saturation status=%d headers=%v", recorder.Code, recorder.Header())
	}
	// Release the blocked request without starting a verifier worker.
	job := <-ingress.jobs
	job.result <- ingressResult{err: ErrPayload}
	<-firstDone
}

func TestEnvelopeSignatureCoversIdentitySequenceTimeAndPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, err := SignEnvelope(privateKey, "community-one", 4, now, []byte("compressed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEnvelope(publicKey, envelope, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SignedEnvelope){
		func(value *SignedEnvelope) { value.FeederID = "other" },
		func(value *SignedEnvelope) { value.Sequence++ },
		func(value *SignedEnvelope) { value.Payload[0] ^= 0xff },
	} {
		copyValue := envelope
		copyValue.Payload = append([]byte(nil), envelope.Payload...)
		mutate(&copyValue)
		if _, err := VerifyEnvelope(publicKey, copyValue, now, time.Minute); !errors.Is(err, ErrSignature) {
			t.Fatalf("tampered envelope error = %v", err)
		}
	}
}

func TestClientRetryKeepsTheSameSignedDeliveryUntilAccepted(t *testing.T) {
	var mutex sync.Mutex
	var deliveries []SignedEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope SignedEnvelope
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		deliveries = append(deliveries, envelope)
		attempt := len(deliveries)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if attempt == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":"retry"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client.credential = Credentials{FeederID: "community-one", PrivateKey: privateKey}
	client.now = func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }
	first := &domain.Snapshot{Aircraft: []domain.Aircraft{{ICAO: "ABC123", Callsign: "FIRST"}}}
	if err := client.Send(context.Background(), first); err == nil {
		t.Fatal("transient failure unexpectedly succeeded")
	}
	newer := &domain.Snapshot{Aircraft: []domain.Aircraft{{ICAO: "ABC123", Callsign: "NEWER"}}}
	if err := client.Send(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(deliveries) != 2 || deliveries[0].Sequence != 1 || deliveries[1].Sequence != 1 ||
		!bytes.Equal(deliveries[0].Payload, deliveries[1].Payload) || !bytes.Equal(deliveries[0].Signature, deliveries[1].Signature) {
		t.Fatalf("retry changed signed delivery: %+v", deliveries)
	}
}

func TestIngressEnrollmentPublicationDuplicateAndReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "ingress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	feeder := storage.Feeder{GuildID: 42, Descriptor: domain.FeederDescriptor{ID: "community-one", DisplayName: "Community One", SourceKind: domain.FeederSourceAgent, Enabled: true}, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertFeeder(ctx, feeder); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("synthetic-", 6)
	hash := sha256.Sum256([]byte(token))
	if err := store.CreateFeederEnrollment(ctx, storage.FeederEnrollment{TokenHash: hash[:], FeederID: feeder.Descriptor.ID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	manager := state.NewFeederManager(time.Millisecond)
	ingress, err := NewIngressServer(IngressConfig{Addr: "127.0.0.1:0"}, store, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range ingress.workers {
		go ingress.worker(ctx)
	}
	server := httptest.NewServer(ingress.Handler())
	defer server.Close()
	client, err := NewClient(server.URL, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	credential, err := client.Enroll(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &domain.Snapshot{FetchedAt: now, SourceGeneratedAt: now, ActiveProvider: domain.ProviderReadsb, Aircraft: []domain.Aircraft{{ICAO: "ABC123"}}, Health: domain.Health{Aircraft: domain.SourceHealth{Status: domain.HealthHealthy}}}
	if err := client.Send(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	manager.Rebuild()
	if published, ok := manager.Feeder(credential.FeederID); !ok || len(published.Aircraft) != 1 {
		t.Fatalf("published snapshot = %+v, %t", published, ok)
	}

	stored, err := store.Feeder(ctx, credential.FeederID)
	if err != nil {
		t.Fatal(err)
	}
	alteredSnapshot := *snapshot
	alteredSnapshot.Aircraft = []domain.Aircraft{{ICAO: "ABC123", Callsign: "ALTERED"}}
	payload, _ := EncodeSnapshot(&alteredSnapshot)
	envelope, _ := SignEnvelope(credential.PrivateKey, credential.FeederID, stored.LastSequence, now, payload)
	// A repeated sequence with a different payload hash is rejected. Ensure the
	// request itself remains well-formed so this exercises durable replay state.
	body, _ := json.Marshal(envelope)
	var response map[string]any
	if err := client.post(ctx, "/v1/agent/snapshots", body, &response); err == nil {
		t.Fatal("altered replay accepted")
	}
	if !bytes.Equal(stored.PublicKey, credential.PrivateKey.Public().(ed25519.PublicKey)) {
		t.Fatal("server public key does not match agent identity")
	}
}

func BenchmarkAgentSnapshotCodecOneThousandAircraft(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	aircraft := make([]domain.Aircraft, 1_000)
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{
			ICAO: fmt.Sprintf("%06X", index), Callsign: fmt.Sprintf("SF%04d", index),
			HasPosition: true, Latitude: 25 + float64(index)/10_000, Longitude: -80 - float64(index)/10_000,
			HasAltitude: true, AltitudeFeet: 1_000 + index*20, HasDistance: true, DistanceNM: float64(index) / 10,
		}
	}
	snapshot := &domain.Snapshot{SourceGeneratedAt: now, FetchedAt: now, Aircraft: aircraft}
	encoder, err := NewSnapshotEncoder()
	if err != nil {
		b.Fatal(err)
	}
	defer encoder.Close()
	payload, err := encoder.Encode(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	decoder, err := NewSnapshotDecoder()
	if err != nil {
		b.Fatal(err)
	}
	defer decoder.Close()
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := encoder.Encode(snapshot); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := decoder.Decode("community-one", payload, now); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAgentIngressOneThousandAircraft(b *testing.B) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(b.TempDir(), "ingress-benchmark.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 1); err != nil {
		b.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	descriptor := domain.FeederDescriptor{ID: "benchmark", DisplayName: "Benchmark", SourceKind: domain.FeederSourceAgent, Enabled: true}
	if err := store.UpsertFeeder(ctx, storage.Feeder{GuildID: 1, Descriptor: descriptor, PublicKey: publicKey, CreatedAt: now, UpdatedAt: now}); err != nil {
		b.Fatal(err)
	}
	manager := state.NewFeederManager(time.Second)
	if err := manager.Register(descriptor); err != nil {
		b.Fatal(err)
	}
	ingress, err := NewIngressServer(IngressConfig{Addr: "127.0.0.1:0"}, store, manager, nil)
	if err != nil {
		b.Fatal(err)
	}
	aircraft := make([]domain.Aircraft, 1_000)
	for index := range aircraft {
		aircraft[index] = domain.Aircraft{ICAO: fmt.Sprintf("%06X", index), Callsign: fmt.Sprintf("SF%04d", index), HasPosition: true, Latitude: 25, Longitude: -80}
	}
	payload, err := EncodeSnapshot(&domain.Snapshot{FetchedAt: now, Aircraft: aircraft})
	if err != nil {
		b.Fatal(err)
	}
	decoder, err := NewSnapshotDecoder()
	if err != nil {
		b.Fatal(err)
	}
	defer decoder.Close()
	ingress.now = func() time.Time { return now }
	b.ReportAllocs()
	b.ResetTimer()
	for sequence := uint64(1); b.Loop(); sequence++ {
		envelope, err := SignEnvelope(privateKey, descriptor.ID, sequence, now, payload)
		if err != nil {
			b.Fatal(err)
		}
		if result := ingress.process(ctx, decoder, envelope); result.err != nil {
			b.Fatal(result.err)
		}
	}
}
