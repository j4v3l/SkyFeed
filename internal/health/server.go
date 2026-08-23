package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/j4v3l/SkyFeed/internal/privacy"
)

type Component struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type State struct {
	started    time.Time
	live       atomic.Bool
	ready      atomic.Bool
	mu         sync.RWMutex
	parts      map[string]Component
	privacy    privacy.Disclosure
	hasPrivacy bool
}

func NewState(now time.Time) *State {
	state := &State{
		started: now,
		parts:   make(map[string]Component),
	}
	state.live.Store(true)
	return state
}

func (s *State) SetLive(value bool) {
	s.live.Store(value)
}

func (s *State) SetReady(value bool) {
	s.ready.Store(value)
}

func (s *State) SetComponent(name, status, message string) {
	s.mu.Lock()
	s.parts[name] = Component{Status: status, Message: message}
	s.mu.Unlock()
}

func (s *State) SetPrivacyDisclosure(disclosure privacy.Disclosure) {
	s.mu.Lock()
	s.privacy = disclosure.Clone()
	s.hasPrivacy = true
	s.mu.Unlock()
}

func (s *State) healthy() bool {
	if !s.live.Load() || !s.ready.Load() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, component := range s.parts {
		switch component.Status {
		case "healthy", "disabled":
		default:
			return false
		}
	}
	return true
}

type snapshot struct {
	Status        string               `json:"status"`
	Live          bool                 `json:"live"`
	Ready         bool                 `json:"ready"`
	UptimeSeconds float64              `json:"uptime_seconds"`
	Components    map[string]Component `json:"components,omitempty"`
	Privacy       *privacy.Disclosure  `json:"privacy,omitempty"`
}

func (s *State) Snapshot(now time.Time) snapshot {
	s.mu.RLock()
	components := make(map[string]Component, len(s.parts))
	for name, component := range s.parts {
		components[name] = component
	}
	var disclosure *privacy.Disclosure
	if s.hasPrivacy {
		value := s.privacy.Clone()
		disclosure = &value
	}
	s.mu.RUnlock()

	live := s.live.Load()
	ready := s.ready.Load()
	status := "healthy"
	if !live {
		status = "offline"
	} else if !ready {
		status = "not_ready"
	} else {
		for _, component := range components {
			switch component.Status {
			case "offline":
				status = "offline"
			case "healthy", "disabled":
			default:
				if status == "healthy" {
					status = "degraded"
				}
			}
		}
	}
	return snapshot{
		Status:        status,
		Live:          live,
		Ready:         ready,
		UptimeSeconds: now.Sub(s.started).Seconds(),
		Components:    components,
		Privacy:       disclosure,
	}
}

type Server struct {
	address         string
	state           *State
	logger          *slog.Logger
	shutdownTimeout time.Duration
	handler         http.Handler
	metrics         http.Handler
}

func (s *Server) SetMetrics(handler http.Handler) {
	s.metrics = handler
	s.handler = s.routes()
}

func NewServer(address string, state *State, logger *slog.Logger) *Server {
	server := &Server{
		address:         address,
		state:           state,
		logger:          logger,
		shutdownTimeout: 5 * time.Second,
	}
	server.handler = server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		shutdownDone <- httpServer.Shutdown(shutdownContext)
	}()

	s.logger.Info("health server listening", "component", "health", "event", "listen", "address", listener.Addr().String())
	serveErr := httpServer.Serve(listener)
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	if ctx.Err() != nil {
		return <-shutdownDone
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(writer http.ResponseWriter, _ *http.Request) {
		s.writeStatus(writer, s.state.live.Load())
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		s.writeStatus(writer, s.state.ready.Load())
	})
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		s.writeStatus(writer, s.state.healthy())
	})
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		s.writePrivacyPage(writer)
	})
	if s.metrics != nil {
		mux.Handle("GET /metrics", s.metrics)
	}
	return mux
}

func (s *Server) writeStatus(writer http.ResponseWriter, ok bool) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if !ok {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(writer).Encode(s.state.Snapshot(time.Now()))
}

func (s *Server) writePrivacyPage(writer http.ResponseWriter) {
	snapshot := s.state.Snapshot(time.Now())
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(privacyHTML(snapshot)))
}

func privacyHTML(snapshot snapshot) string {
	providers := "readsb"
	airport := "not configured"
	radius := 0
	if snapshot.Privacy != nil {
		if len(snapshot.Privacy.Providers) > 0 {
			providers = strings.Join(snapshot.Privacy.Providers, ", ")
		}
		if snapshot.Privacy.PublicAirportCode != "" {
			airport = snapshot.Privacy.PublicAirportCode
		}
		radius = snapshot.Privacy.RadiusNM
	}
	var builder strings.Builder
	builder.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	builder.WriteString("<title>SkyFeed</title><style>body{font-family:ui-sans-serif,system-ui,sans-serif;margin:2rem;max-width:40rem;line-height:1.5;color:#111}h1{font-size:1.5rem}code{background:#f3f4f6;padding:.1rem .3rem;border-radius:.25rem}</style></head><body>")
	builder.WriteString("<h1>SkyFeed</h1>")
	builder.WriteString("<p>Privacy-safe status page. Receiver coordinates and external query center coordinates are never shown.</p>")
	builder.WriteString("<p><strong>Status:</strong> ")
	builder.WriteString(htmlEscape(snapshot.Status))
	builder.WriteString(" • live=")
	builder.WriteString(fmt.Sprintf("%t", snapshot.Live))
	builder.WriteString(" • ready=")
	builder.WriteString(fmt.Sprintf("%t", snapshot.Ready))
	builder.WriteString("</p>")
	builder.WriteString("<p><strong>Providers:</strong> ")
	builder.WriteString(htmlEscape(providers))
	builder.WriteString("</p>")
	builder.WriteString("<p><strong>Public airport center:</strong> <code>")
	builder.WriteString(htmlEscape(airport))
	builder.WriteString("</code>")
	if radius > 0 {
		builder.WriteString(fmt.Sprintf(" within %d NM", radius))
	}
	builder.WriteString("</p>")
	builder.WriteString("<p>JSON diagnostics: <a href=\"/healthz\">/healthz</a> • <a href=\"/readyz\">/readyz</a> • <a href=\"/livez\">/livez</a></p>")
	builder.WriteString("</body></html>")
	return builder.String()
}

func htmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return replacer.Replace(value)
}
