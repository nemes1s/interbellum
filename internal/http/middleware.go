package http

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/logging"
)

// requestIDHeader is both accepted from callers (so a request can be traced
// across a gateway) and echoed back on every response.
const requestIDHeader = "X-Request-Id"

// requestID assigns or adopts a request ID and puts it in the request context,
// where the logging handler picks it up automatically. A caller-supplied value
// is trusted for correlation only — nothing authorizes on it.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

// statusRecorder captures the status code and response size for the access
// log, since http.ResponseWriter does not expose them after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// accessLog emits one structured line per request. Query strings and bodies
// are deliberately excluded: alert payloads and evidence can carry sensitive
// operational detail and belong in the database, not in log aggregation.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			level := slog.LevelInfo
			if recorder.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			log.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Int("bytes", recorder.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// recoverPanic turns a panic into a 500 rather than a dropped connection, and
// logs the stack so the bug is diagnosable. Without it, one bad request could
// take down an in-flight batch of others sharing the process.
func recoverPanic(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					// A panic after headers are sent cannot be turned into a
					// clean error response, but it must still be logged.
					log.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", recovered),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("stack", string(debug.Stack())))

					writeError(r.Context(), w, log,
						apperror.New(apperror.CodeInternal, "internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// limitBody caps how much of a request body will be read. Applied globally
// because every write endpoint takes JSON, and an unbounded body is the
// cheapest available denial-of-service.
func limitBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
