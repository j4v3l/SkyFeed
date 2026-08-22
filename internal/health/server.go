package health

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Component struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type State struct {
	started time.Time
	live    atomic.Bool
	ready   atomic.Bool
	mu      sync.RWMutex
	parts   map[string]Component
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

type snapshot struct {
	Status        string               `json:"status"`
	Live          bool                 `json:"live"`
	Ready         bool                 `json:"ready"`
	UptimeSeconds float64              `json:"uptime_seconds"`
	Components    map[string]Component `json:"components,omitempty"`
}

func (s *State) Snapshot(now time.Time) snapshot {
	s.mu.RLock()
	components := make(map[string]Component, len(s.parts))
	for name, component := range s.parts {
		components[name] = component
	}
	s.mu.RUnlock()

	live := s.live.Load()
	ready := s.ready.Load()
	status := "healthy"
	if !live {
		status = "offline"
	} else if !ready {
		status = "not_ready"
	}
	return snapshot{
		Status:        status,
		Live:          live,
		Ready:         ready,
		UptimeSeconds: now.Sub(s.started).Seconds(),
		Components:    components,
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
		s.writeStatus(writer, s.state.live.Load() && s.state.ready.Load())
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
