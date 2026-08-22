package discord

import (
	"errors"
	"testing"
	"time"
)

func TestSessionBindingExpiryAndCustomID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	manager := NewSessionManager(4, 2, time.Minute)
	manager.now = func() time.Time { return now }
	session, err := manager.Create(1, 2, 3, "nearby", "distance")
	if err != nil {
		t.Fatal(err)
	}
	customID, err := CustomID(session.ID, "next")
	if err != nil {
		t.Fatal(err)
	}
	id, action, err := ParseCustomID(customID)
	if err != nil || id != session.ID || action != "next" {
		t.Fatalf("parse result: %q %q %v", id, action, err)
	}
	if _, err = manager.Get(id, 9, 2, 3); !errors.Is(err, ErrSessionOwner) {
		t.Fatalf("expected owner error, got %v", err)
	}
	now = now.Add(time.Minute)
	if _, err = manager.Get(id, 1, 2, 3); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestSessionCaps(t *testing.T) {
	manager := NewSessionManager(2, 1, time.Minute)
	if _, err := manager.Create(1, 2, 3, "nearby", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(1, 2, 3, "nearby", ""); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("expected per-user cap, got %v", err)
	}
	if _, err := manager.Create(2, 2, 3, "nearby", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(3, 2, 3, "nearby", ""); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("expected global cap, got %v", err)
	}
}

func TestSessionCanUseShorterConfirmationTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	manager := NewSessionManager(2, 1, 15*time.Minute)
	manager.now = func() time.Time { return now }
	session, err := manager.CreateWithTTL(1, 2, 3, "moderation", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ExpiresAt.Sub(session.CreatedAt); got != time.Minute {
		t.Fatalf("confirmation TTL=%s", got)
	}
}

func FuzzParseCustomID(f *testing.F) {
	f.Add("sf:v1:abc:next")
	f.Add("raw-user-input")
	f.Fuzz(func(t *testing.T, value string) {
		id, action, err := ParseCustomID(value)
		if err == nil {
			roundTrip, buildErr := CustomID(id, action)
			if buildErr != nil || roundTrip != value {
				t.Fatalf("round trip %q: %q %v", value, roundTrip, buildErr)
			}
		}
	})
}
