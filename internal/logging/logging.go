// Package logging configures structured logging.
//
// slog from the standard library is used rather than a third-party logger:
// it produces structured JSON, supports contextual attributes, and needs no
// dependency — which matters for a service whose logs are meant to be
// correlated by request/investigation ID rather than read as prose.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// New builds the application logger. format is "json" or "text"; text is for
// local development, where a human reads the output directly.
func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(&contextHandler{Handler: handler})
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type contextKey string

const requestIDKey contextKey = "request_id"

// WithRequestID returns a context carrying the request ID, so that every log
// line emitted while handling that request can be correlated without every
// function having to accept and thread a logger.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFrom returns the request ID carried by ctx, if any.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// contextHandler stamps the request ID onto every record logged with a
// context, which is what makes the service-layer log lines (investigation
// started, decision applied) joinable with the access log entry for the
// request that caused them.
type contextHandler struct{ slog.Handler }

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestIDFrom(ctx); id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
