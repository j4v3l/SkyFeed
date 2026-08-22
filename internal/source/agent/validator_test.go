package agent

import (
	"errors"
	"testing"
	"time"
)

func TestValidatorBindsIdentityAndRejectsReplay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	validator := NewValidator(time.Minute, 1024)
	validator.now = func() time.Time { return now }
	envelope := Envelope{SourceID: "feeder-home", Sequence: 1, SentAt: now, Payload: []byte("snapshot")}
	if err := validator.Validate("feeder-home", envelope); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate("feeder-home", envelope); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error=%v", err)
	}
	if err := validator.Validate("other", Envelope{SourceID: "feeder-home", Sequence: 2, SentAt: now, Payload: []byte("snapshot")}); !errors.Is(err, ErrIdentity) {
		t.Fatalf("identity error=%v", err)
	}
}
