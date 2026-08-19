package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/indurex/interbellum/internal/apperror"
)

// readinessTimeout bounds the database check so a hung connection cannot make
// the readiness probe itself hang — an unresponsive probe is worse than a
// failing one, because an orchestrator learns nothing from it.
const readinessTimeout = 2 * time.Second

// healthHandler serves the liveness and readiness probes.
type healthHandler struct {
	ping func(context.Context) error
	log  *slog.Logger
}

// live handles GET /healthz: is the process running at all.
//
// Deliberately checks nothing external. If liveness depended on the database,
// a database blip would make an orchestrator restart every healthy API
// replica — turning a recoverable outage into a total one.
func (h *healthHandler) live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.log, http.StatusOK, map[string]string{"status": "ok"})
}

// ready handles GET /readyz: can this instance actually serve traffic.
//
// This is where the database is checked, because an instance that cannot reach
// PostgreSQL can serve nothing useful and should be taken out of the load
// balancer rotation — without being restarted.
func (h *healthHandler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	if err := h.ping(ctx); err != nil {
		h.log.WarnContext(ctx, "readiness check failed", slog.Any("error", err))
		writeError(ctx, w, h.log, apperror.New(apperror.CodeNotReady, "database is not reachable"))
		return
	}
	writeJSON(w, h.log, http.StatusOK, map[string]string{"status": "ok"})
}
