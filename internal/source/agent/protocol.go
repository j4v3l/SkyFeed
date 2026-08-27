package agent

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/klauspost/compress/zstd"
)

const (
	ProtocolVersion      = 1
	MaxCompressedBytes   = 2 << 20
	MaxDecompressedBytes = 16 << 20
	MaxAircraft          = 5_000
	DefaultMaxClockSkew  = 2 * time.Minute
)

var (
	ErrVersion   = errors.New("unsupported agent protocol version")
	ErrSignature = errors.New("invalid agent signature")
)

type SignedEnvelope struct {
	Version   int             `json:"version"`
	FeederID  domain.FeederID `json:"feeder_id"`
	Sequence  uint64          `json:"sequence"`
	SentAt    time.Time       `json:"sent_at"`
	Payload   []byte          `json:"payload"`
	Signature []byte          `json:"signature"`
}

type SnapshotPayload struct {
	SourceGeneratedAt time.Time           `json:"source_generated_at"`
	FetchedAt         time.Time           `json:"fetched_at"`
	ActiveProvider    domain.ProviderID   `json:"active_provider"`
	Capabilities      domain.Capabilities `json:"capabilities"`
	Receiver          domain.Receiver     `json:"receiver"`
	Statistics        domain.Statistics   `json:"statistics"`
	ReceiverMessages  uint64              `json:"receiver_messages"`
	MessageValid      bool                `json:"message_counter_valid"`
	Aircraft          []domain.Aircraft   `json:"aircraft"`
	Health            domain.Health       `json:"health"`
}

func EncodeSnapshot(snapshot *domain.Snapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, errors.New("snapshot is required")
	}
	if len(snapshot.Aircraft) > MaxAircraft {
		return nil, fmt.Errorf("aircraft count %d exceeds %d", len(snapshot.Aircraft), MaxAircraft)
	}
	receiver := snapshot.Receiver
	// Receiver coordinates identify a private installation and never leave the
	// LAN agent. Aircraft positions remain public ADS-B observations.
	receiver.Latitude = 0
	receiver.Longitude = 0
	receiver.HasPosition = false
	aircraft := append([]domain.Aircraft(nil), snapshot.Aircraft...)
	for index := range aircraft {
		// Receiver-relative values can reveal the private installation even when
		// the raw receiver coordinates are removed. The central service rebuilds
		// these values from administrator-approved public feeder metadata.
		aircraft[index].DistanceNM = 0
		aircraft[index].BearingDegrees = 0
		aircraft[index].HasDistance = false
		aircraft[index].SeenBy = nil
	}
	payload := SnapshotPayload{
		SourceGeneratedAt: snapshot.SourceGeneratedAt, FetchedAt: snapshot.FetchedAt,
		ActiveProvider: snapshot.ActiveProvider, Capabilities: snapshot.Capabilities,
		Receiver: receiver, Statistics: snapshot.Statistics, ReceiverMessages: snapshot.ReceiverMessages,
		MessageValid: snapshot.MessageCounterValid, Aircraft: aircraft, Health: snapshot.Health,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode agent snapshot: %w", err)
	}
	if len(raw) > MaxDecompressedBytes {
		return nil, fmt.Errorf("snapshot JSON exceeds %d bytes", MaxDecompressedBytes)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, fmt.Errorf("create snapshot compressor: %w", err)
	}
	defer encoder.Close()
	compressed := encoder.EncodeAll(raw, make([]byte, 0, min(len(raw), MaxCompressedBytes)))
	if len(compressed) > MaxCompressedBytes {
		return nil, fmt.Errorf("compressed snapshot exceeds %d bytes", MaxCompressedBytes)
	}
	return compressed, nil
}

func DecodeSnapshot(feederID domain.FeederID, compressed []byte, publishedAt time.Time) (*domain.Snapshot, error) {
	if len(compressed) == 0 || len(compressed) > MaxCompressedBytes {
		return nil, ErrPayload
	}
	decoder, err := zstd.NewReader(bytes.NewReader(compressed), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(MaxDecompressedBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: create snapshot decoder", ErrPayload)
	}
	defer decoder.Close()
	limited := io.LimitReader(decoder, MaxDecompressedBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: decompress agent snapshot", ErrPayload)
	}
	if len(raw) > MaxDecompressedBytes {
		return nil, ErrPayload
	}
	var payload SnapshotPayload
	decoderJSON := json.NewDecoder(bytes.NewReader(raw))
	if err := decoderJSON.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	var extra any
	if err := decoderJSON.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrPayload
	}
	if len(payload.Aircraft) > MaxAircraft {
		return nil, ErrPayload
	}
	if !feederID.Valid() || feederID == domain.FeederAll {
		return nil, ErrIdentity
	}
	aircraft := append([]domain.Aircraft(nil), payload.Aircraft...)
	for index := range aircraft {
		aircraft[index].ICAO = strings.ToUpper(strings.TrimSpace(aircraft[index].ICAO))
		aircraft[index].SeenBy = nil
	}
	sort.Slice(aircraft, func(left, right int) bool { return aircraft[left].ICAO < aircraft[right].ICAO })
	byICAO := make(map[string]int, len(aircraft))
	search := make([]domain.AircraftKey, 0, len(aircraft))
	for index := range aircraft {
		icao := aircraft[index].ICAO
		if icao == "" {
			return nil, errors.New("agent aircraft ICAO is required")
		}
		if _, duplicate := byICAO[icao]; duplicate {
			return nil, errors.New("agent snapshot contains duplicate aircraft ICAO")
		}
		byICAO[icao] = index
		search = append(search, domain.AircraftKey{ICAO: icao, Callsign: aircraft[index].Callsign, Registration: aircraft[index].Registration})
	}
	return &domain.Snapshot{
		FeederID: feederID, ActiveProvider: payload.ActiveProvider, Capabilities: payload.Capabilities,
		SourceGeneratedAt: payload.SourceGeneratedAt, FetchedAt: payload.FetchedAt, PublishedAt: publishedAt.UTC(),
		Receiver: payload.Receiver, Statistics: payload.Statistics, ReceiverMessages: payload.ReceiverMessages,
		MessageCounterValid: payload.MessageValid, Aircraft: aircraft, ByICAO: byICAO, Search: search, Health: payload.Health,
	}, nil
}

func SignEnvelope(privateKey ed25519.PrivateKey, feederID domain.FeederID, sequence uint64, sentAt time.Time, payload []byte) (SignedEnvelope, error) {
	if len(privateKey) != ed25519.PrivateKeySize || !feederID.Valid() || feederID == domain.FeederAll || sequence == 0 || len(payload) == 0 || len(payload) > MaxCompressedBytes {
		return SignedEnvelope{}, errors.New("invalid signed envelope input")
	}
	envelope := SignedEnvelope{Version: ProtocolVersion, FeederID: feederID, Sequence: sequence, SentAt: sentAt.UTC(), Payload: payload}
	hash := sha256.Sum256(payload)
	envelope.Signature = ed25519.Sign(privateKey, signingBytes(envelope, hash))
	return envelope, nil
}

func VerifyEnvelope(publicKey ed25519.PublicKey, envelope SignedEnvelope, now time.Time, maxSkew time.Duration) ([32]byte, error) {
	if envelope.Version != ProtocolVersion {
		return [32]byte{}, ErrVersion
	}
	if len(publicKey) != ed25519.PublicKeySize || len(envelope.Signature) != ed25519.SignatureSize || !envelope.FeederID.Valid() || envelope.FeederID == domain.FeederAll || envelope.Sequence == 0 {
		return [32]byte{}, ErrSignature
	}
	if len(envelope.Payload) == 0 || len(envelope.Payload) > MaxCompressedBytes {
		return [32]byte{}, ErrPayload
	}
	if maxSkew <= 0 {
		maxSkew = DefaultMaxClockSkew
	}
	if envelope.SentAt.Before(now.Add(-maxSkew)) || envelope.SentAt.After(now.Add(maxSkew)) {
		return [32]byte{}, ErrClock
	}
	hash := sha256.Sum256(envelope.Payload)
	if !ed25519.Verify(publicKey, signingBytes(envelope, hash), envelope.Signature) {
		return [32]byte{}, ErrSignature
	}
	return hash, nil
}

func signingBytes(envelope SignedEnvelope, hash [32]byte) []byte {
	feeder := []byte(envelope.FeederID)
	result := make([]byte, 0, 2+len(feeder)+8+8+len(hash))
	result = binary.BigEndian.AppendUint16(result, uint16(envelope.Version))
	result = append(result, byte(len(feeder)))
	result = append(result, feeder...)
	result = binary.BigEndian.AppendUint64(result, envelope.Sequence)
	result = binary.BigEndian.AppendUint64(result, uint64(envelope.SentAt.UTC().UnixMilli()))
	result = append(result, hash[:]...)
	return result
}
