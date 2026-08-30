package discord

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const customIDPrefix = "sf:v1:"

var (
	ErrInvalidCustomID = errors.New("invalid component custom ID")
	ErrSessionExpired  = errors.New("component session expired")
	ErrSessionOwner    = errors.New("component session belongs to another user or location")
	ErrSessionCapacity = errors.New("component session capacity reached")
)

type Session struct {
	ID                     string
	UserID                 uint64
	GuildID                uint64
	ChannelID              uint64
	View                   string
	Sort                   string
	Query                  string
	Squawk                 string
	FeederID               domain.FeederID
	Units                  domain.UnitSystem
	Page                   int
	PageSize               int
	RadiusNM               float64
	MinFeet                int
	MaxFeet                int
	HasMin                 bool
	HasMax                 bool
	Action                 string
	TargetID               uint64
	TargetChannelID        uint64
	TargetMessageID        uint64
	TargetMessageCreatedAt time.Time
	TargetPreview          string
	Reason                 string
	Duration               time.Duration
	DeleteMessageDuration  time.Duration
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

type SessionManager struct {
	mu         sync.Mutex
	sessions   map[string]Session
	perUser    map[uint64]int
	maxGlobal  int
	maxPerUser int
	ttl        time.Duration
	now        func() time.Time
	random     func([]byte) (int, error)
}

func NewSessionManager(maxGlobal, maxPerUser int, ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions:   make(map[string]Session),
		perUser:    make(map[uint64]int),
		maxGlobal:  maxGlobal,
		maxPerUser: maxPerUser,
		ttl:        ttl,
		now:        time.Now,
		random:     rand.Read,
	}
}

func (manager *SessionManager) Create(userID, guildID, channelID uint64, view, sort, query, squawk string) (Session, error) {
	return manager.CreateWithTTL(userID, guildID, channelID, view, sort, query, squawk, manager.ttl)
}

func (manager *SessionManager) CreateWithTTL(userID, guildID, channelID uint64, view, sort, query, squawk string, ttl time.Duration) (Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupLocked(manager.now())
	if len(manager.sessions) >= manager.maxGlobal || manager.perUser[userID] >= manager.maxPerUser {
		return Session{}, ErrSessionCapacity
	}
	var id string
	for attempt := 0; attempt < 4; attempt++ {
		bytes := make([]byte, 12)
		if _, err := manager.random(bytes); err != nil {
			return Session{}, fmt.Errorf("generate session ID: %w", err)
		}
		id = base64.RawURLEncoding.EncodeToString(bytes)
		if _, exists := manager.sessions[id]; !exists {
			break
		}
		id = ""
	}
	if id == "" {
		return Session{}, errors.New("generate unique session ID")
	}
	now := manager.now()
	if ttl <= 0 || ttl > manager.ttl {
		ttl = manager.ttl
	}
	session := Session{ID: id, UserID: userID, GuildID: guildID, ChannelID: channelID, View: view, Sort: sort, Query: query, Squawk: squawk, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	manager.sessions[id] = session
	manager.perUser[userID]++
	return session, nil
}

func (manager *SessionManager) Get(id string, userID, guildID, channelID uint64) (Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	session, ok := manager.sessions[id]
	if !ok {
		return Session{}, ErrSessionExpired
	}
	if !manager.now().Before(session.ExpiresAt) {
		manager.deleteLocked(id, session)
		return Session{}, ErrSessionExpired
	}
	if session.UserID != userID || session.GuildID != guildID || session.ChannelID != channelID {
		return Session{}, ErrSessionOwner
	}
	return session, nil
}

func (manager *SessionManager) Update(session Session) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	current, ok := manager.sessions[session.ID]
	if !ok || !manager.now().Before(current.ExpiresAt) {
		return ErrSessionExpired
	}
	if current.UserID != session.UserID || current.GuildID != session.GuildID || current.ChannelID != session.ChannelID {
		return ErrSessionOwner
	}
	session.CreatedAt = current.CreatedAt
	session.ExpiresAt = current.ExpiresAt
	manager.sessions[session.ID] = session
	return nil
}

func (manager *SessionManager) Delete(id string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if session, ok := manager.sessions[id]; ok {
		manager.deleteLocked(id, session)
	}
}

func (manager *SessionManager) Cleanup(now time.Time) int {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.cleanupLocked(now)
}

func (manager *SessionManager) RunCleanup(ctxDone <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case now := <-ticker.C:
			manager.Cleanup(now)
		}
	}
}

func (manager *SessionManager) Len() int {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return len(manager.sessions)
}

func CustomID(sessionID, action string) (string, error) {
	if invalidCustomIDPart(sessionID) || invalidCustomIDPart(action) {
		return "", ErrInvalidCustomID
	}
	value := customIDPrefix + sessionID + ":" + action
	if len(value) > 100 {
		return "", ErrInvalidCustomID
	}
	return value, nil
}

func ParseCustomID(value string) (sessionID, action string, err error) {
	if !strings.HasPrefix(value, customIDPrefix) || len(value) > 100 {
		return "", "", ErrInvalidCustomID
	}
	parts := strings.Split(strings.TrimPrefix(value, customIDPrefix), ":")
	if len(parts) != 2 || invalidCustomIDPart(parts[0]) || invalidCustomIDPart(parts[1]) {
		return "", "", ErrInvalidCustomID
	}
	return parts[0], parts[1], nil
}

func invalidCustomIDPart(value string) bool {
	return value == "" || strings.Contains(value, ":") || strings.IndexFunc(value, unicode.IsSpace) >= 0
}

func (manager *SessionManager) cleanupLocked(now time.Time) int {
	removed := 0
	for id, session := range manager.sessions {
		if !now.Before(session.ExpiresAt) {
			manager.deleteLocked(id, session)
			removed++
		}
	}
	return removed
}

func (manager *SessionManager) deleteLocked(id string, session Session) {
	delete(manager.sessions, id)
	manager.perUser[session.UserID]--
	if manager.perUser[session.UserID] <= 0 {
		delete(manager.perUser, session.UserID)
	}
}
