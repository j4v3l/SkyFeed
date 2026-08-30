package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/state"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const (
	defaultIngressWorkers = 4
	defaultIngressQueue   = 256
	maxEnvelopeBytes      = 3 << 20
	maxEnrollmentBytes    = 4 << 10
)

type SnapshotPublisher interface {
	Register(domain.FeederDescriptor) error
	Publish(domain.FeederID, *domain.Snapshot) bool
}

type IngressServer struct {
	addr       string
	repository storage.Repository
	publisher  SnapshotPublisher
	logger     *slog.Logger
	now        func() time.Time
	jobs       chan ingressJob
	workers    int
	maxBody    int64
	server     *http.Server
	feederLock sync.Map
}

type IngressConfig struct {
	Addr         string
	Workers      int
	Queue        int
	MaxBodyBytes int
}

type ingressJob struct {
	envelope SignedEnvelope
	result   chan ingressResult
}

type ingressResult struct {
	duplicate bool
	err       error
}

type enrollmentRequest struct {
	Token     string `json:"token"`
	PublicKey []byte `json:"public_key"`
}

type enrollmentResponse struct {
	Version     int             `json:"version"`
	FeederID    domain.FeederID `json:"feeder_id"`
	DisplayName string          `json:"display_name"`
}

func NewIngressServer(config IngressConfig, repository storage.Repository, publisher SnapshotPublisher, logger *slog.Logger) (*IngressServer, error) {
	if strings.TrimSpace(config.Addr) == "" || repository == nil || publisher == nil {
		return nil, errors.New("agent ingress requires an address, repository, and publisher")
	}
	if _, _, err := net.SplitHostPort(config.Addr); err != nil {
		return nil, fmt.Errorf("agent ingress address: %w", err)
	}
	if config.Workers <= 0 {
		config.Workers = defaultIngressWorkers
	}
	if config.Queue <= 0 {
		config.Queue = defaultIngressQueue
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = maxEnvelopeBytes
	}
	if logger == nil {
		logger = slog.Default()
	}
	ingress := &IngressServer{
		addr: config.Addr, repository: repository, publisher: publisher, logger: logger, now: time.Now,
		jobs: make(chan ingressJob, config.Queue), workers: config.Workers, maxBody: int64(config.MaxBodyBytes),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agent/enroll", ingress.handleEnrollment)
	mux.HandleFunc("POST /v1/agent/snapshots", ingress.handleSnapshot)
	ingress.server = &http.Server{
		Addr: config.Addr, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	return ingress, nil
}

func (ingress *IngressServer) Run(ctx context.Context) error {
	workerContext, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	for range ingress.workers {
		go ingress.worker(workerContext)
	}
	listener, err := net.Listen("tcp", ingress.addr)
	if err != nil {
		return fmt.Errorf("listen agent ingress: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		err := ingress.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ingress.server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown agent ingress: %w", err)
		}
		return <-done
	case err := <-done:
		return err
	}
}

func (ingress *IngressServer) Handler() http.Handler { return ingress.server.Handler }

func (ingress *IngressServer) handleEnrollment(writer http.ResponseWriter, request *http.Request) {
	var input enrollmentRequest
	if err := decodeBoundedJSON(writer, request, maxEnrollmentBytes, &input); err != nil {
		writeAgentError(writer, http.StatusBadRequest, "invalid enrollment request")
		return
	}
	input.Token = strings.TrimSpace(input.Token)
	if len(input.Token) < 32 || len(input.Token) > 256 || len(input.PublicKey) != ed25519.PublicKeySize {
		writeAgentError(writer, http.StatusUnauthorized, "invalid enrollment")
		return
	}
	tokenHash := sha256.Sum256([]byte(input.Token))
	feeder, err := ingress.repository.ConsumeFeederEnrollment(request.Context(), tokenHash[:], input.PublicKey, ingress.now().UTC())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrEnrollmentInvalid) {
			status = http.StatusUnauthorized
		}
		writeAgentError(writer, status, "enrollment was rejected")
		return
	}
	if err := ingress.publisher.Register(feeder.Descriptor); err != nil {
		writeAgentError(writer, http.StatusInternalServerError, "feeder could not be activated")
		return
	}
	writeAgentJSON(writer, http.StatusCreated, enrollmentResponse{Version: ProtocolVersion, FeederID: feeder.Descriptor.ID, DisplayName: feeder.Descriptor.DisplayName})
}

func (ingress *IngressServer) handleSnapshot(writer http.ResponseWriter, request *http.Request) {
	var envelope SignedEnvelope
	if err := decodeBoundedJSON(writer, request, ingress.maxBody, &envelope); err != nil {
		writeAgentError(writer, http.StatusBadRequest, "invalid snapshot envelope")
		return
	}
	job := ingressJob{envelope: envelope, result: make(chan ingressResult, 1)}
	select {
	case ingress.jobs <- job:
	default:
		writer.Header().Set("Retry-After", "1")
		writeAgentError(writer, http.StatusTooManyRequests, "snapshot queue is full")
		return
	}
	select {
	case <-request.Context().Done():
		return
	case result := <-job.result:
		if result.err != nil {
			status := http.StatusBadRequest
			if errors.Is(result.err, ErrSignature) || errors.Is(result.err, ErrIdentity) {
				status = http.StatusUnauthorized
			} else if errors.Is(result.err, storage.ErrSequenceRejected) || errors.Is(result.err, ErrClock) {
				status = http.StatusConflict
			} else if !errors.Is(result.err, ErrPayload) && !errors.Is(result.err, ErrVersion) {
				status = http.StatusInternalServerError
			}
			writeAgentError(writer, status, "snapshot was rejected")
			return
		}
		writeAgentJSON(writer, http.StatusOK, map[string]any{"accepted": true, "duplicate": result.duplicate})
	}
}

func (ingress *IngressServer) worker(ctx context.Context) {
	decoder, decoderErr := NewSnapshotDecoder()
	if decoderErr == nil {
		defer decoder.Close()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-ingress.jobs:
			if decoderErr != nil {
				job.result <- ingressResult{err: decoderErr}
				continue
			}
			result := ingress.process(ctx, decoder, job.envelope)
			job.result <- result
		}
	}
}

func (ingress *IngressServer) process(ctx context.Context, decoder *SnapshotDecoder, envelope SignedEnvelope) ingressResult {
	lockValue, _ := ingress.feederLock.LoadOrStore(envelope.FeederID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	feeder, err := ingress.repository.Feeder(ctx, envelope.FeederID)
	if err != nil || !feeder.Descriptor.Enabled || feeder.Descriptor.SourceKind != domain.FeederSourceAgent {
		return ingressResult{err: ErrIdentity}
	}
	hash, err := VerifyEnvelope(ed25519.PublicKey(feeder.PublicKey), envelope, ingress.now().UTC(), DefaultMaxClockSkew)
	if err != nil {
		return ingressResult{err: err}
	}
	snapshot, err := decoder.Decode(envelope.FeederID, envelope.Payload, ingress.now().UTC())
	if err != nil {
		return ingressResult{err: err}
	}
	applyPublicCenter(snapshot, feeder.Descriptor)
	acceptance, err := ingress.repository.AcceptFeederSequence(ctx, envelope.FeederID, envelope.Sequence, hash[:], ingress.now().UTC())
	if err != nil {
		return ingressResult{err: err}
	}
	if acceptance == storage.SequenceDuplicate {
		if !ingress.publisher.Publish(envelope.FeederID, snapshot) {
			return ingressResult{err: errors.New("duplicate feeder publication refused")}
		}
		return ingressResult{duplicate: true}
	}
	if !ingress.publisher.Publish(envelope.FeederID, snapshot) {
		return ingressResult{err: errors.New("feeder publication refused")}
	}
	return ingressResult{}
}

func applyPublicCenter(snapshot *domain.Snapshot, descriptor domain.FeederDescriptor) {
	if snapshot == nil {
		return
	}
	snapshot.Receiver.Latitude = 0
	snapshot.Receiver.Longitude = 0
	snapshot.Receiver.HasPosition = false
	for index := range snapshot.Aircraft {
		aircraft := &snapshot.Aircraft[index]
		aircraft.DistanceNM = 0
		aircraft.BearingDegrees = 0
		aircraft.HasDistance = false
		if descriptor.HasCenter && aircraft.HasPosition {
			aircraft.DistanceNM, aircraft.BearingDegrees = state.DistanceBearing(descriptor.Latitude, descriptor.Longitude, aircraft.Latitude, aircraft.Longitude)
			aircraft.HasDistance = true
		}
	}
}

func decodeBoundedJSON(writer http.ResponseWriter, request *http.Request, maximum int64, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maximum)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeAgentError(writer http.ResponseWriter, status int, message string) {
	writeAgentJSON(writer, status, map[string]string{"error": message})
}

func writeAgentJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
