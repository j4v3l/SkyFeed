// Package agent defines the replay and identity checks shared by a future LAN
// agent transport. No general-purpose LAN proxy or inbound listener is enabled.
package agent

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrIdentity = errors.New("agent source identity mismatch")
	ErrReplay   = errors.New("agent sequence replay")
	ErrClock    = errors.New("agent timestamp outside allowed skew")
	ErrPayload  = errors.New("agent payload exceeds limit")
)

type Envelope struct {
	SourceID string
	Sequence uint64
	SentAt   time.Time
	Payload  []byte
}

type Validator struct {
	mu         sync.Mutex
	last       map[string]uint64
	maxSkew    time.Duration
	maxPayload int
	now        func() time.Time
}

func NewValidator(maxSkew time.Duration, maxPayload int) *Validator {
	return &Validator{last: make(map[string]uint64), maxSkew: maxSkew, maxPayload: maxPayload, now: time.Now}
}

// Validate binds the mTLS certificate identity supplied by the transport to
// the source ID, rejects oversized snapshots, and advances a monotonic replay
// window. Authentication of the peer certificate remains the TLS server's job.
func (validator *Validator) Validate(certificateIdentity string, envelope Envelope) error {
	if certificateIdentity == "" || certificateIdentity != envelope.SourceID {
		return ErrIdentity
	}
	if len(envelope.Payload) == 0 || len(envelope.Payload) > validator.maxPayload {
		return ErrPayload
	}
	now := validator.now()
	if envelope.SentAt.Before(now.Add(-validator.maxSkew)) || envelope.SentAt.After(now.Add(validator.maxSkew)) {
		return ErrClock
	}
	validator.mu.Lock()
	defer validator.mu.Unlock()
	if envelope.Sequence <= validator.last[envelope.SourceID] {
		return fmt.Errorf("%w: got %d after %d", ErrReplay, envelope.Sequence, validator.last[envelope.SourceID])
	}
	validator.last[envelope.SourceID] = envelope.Sequence
	return nil
}
