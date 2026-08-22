package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

func RunPprof(ctx context.Context, address string, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	shutdown := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdown <- server.Shutdown(shutdownContext)
	}()
	logger.Info("pprof server listening", "component", "telemetry", "event", "pprof_listen", "address", listener.Addr().String())
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return <-shutdown
}
