package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ServerConfig holds the HTTP server's operational settings.
type ServerConfig struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// Server wraps http.Server with startup and graceful-shutdown behaviour.
type Server struct {
	http            *http.Server
	shutdownTimeout time.Duration
	log             *slog.Logger
}

// NewServer builds the HTTP server. Every timeout is set explicitly: Go's
// defaults are "no timeout", which lets a slow or malicious client hold a
// connection indefinitely.
func NewServer(cfg ServerConfig, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadTimeout:       cfg.ReadTimeout,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			// Route slog through as the server's own error logger so protocol
			// errors land in the same structured stream as everything else.
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		log:             log,
	}
}

// Run serves until ctx is cancelled, then shuts down gracefully: it stops
// accepting new connections and gives in-flight requests up to
// ShutdownTimeout to finish. That matters here because a decision submission
// holds a database transaction — cutting it off mid-flight would abort work a
// client believes succeeded.
func (s *Server) Run(ctx context.Context) error {
	errs := make(chan error, 1)

	go func() {
		s.log.Info("http server listening", slog.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		s.log.Info("shutdown signal received, draining connections",
			slog.Duration("timeout", s.shutdownTimeout))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()

		if err := s.http.Shutdown(shutdownCtx); err != nil {
			// Requests still running at the deadline are terminated; report it
			// rather than exiting 0, so a deploy pipeline can notice.
			s.log.Error("graceful shutdown did not complete in time", slog.Any("error", err))
			return err
		}
		s.log.Info("http server stopped cleanly")
		return nil
	}
}
