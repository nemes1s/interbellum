package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/service/alertservice"
	"github.com/indurex/interbellum/internal/service/investigationservice"
	"github.com/indurex/interbellum/internal/service/playbookservice"
)

// Dependencies is everything the router needs. Passing one struct keeps the
// signature stable as the API grows, and makes the test setup in
// test/integration read as a list of what the API actually depends on.
type Dependencies struct {
	Playbooks      *playbookservice.Service
	Alerts         *alertservice.Service
	Investigations *investigationservice.Service
	Logger         *slog.Logger

	// Ping reports database reachability for the readiness probe. A function
	// rather than the pool itself, so the HTTP layer keeps its promise of
	// never importing pgx.
	Ping func(context.Context) error

	// MaxRequestBytes caps request bodies.
	MaxRequestBytes int64
}

// NewRouter builds the HTTP handler for the whole API.
func NewRouter(deps Dependencies) http.Handler {
	log := deps.Logger

	r := chi.NewRouter()

	// Order matters: request IDs must exist before anything logs, panics must
	// be caught inside the access log so a failed request is still recorded,
	// and the body limit must wrap every handler that reads a body.
	r.Use(requestID)
	r.Use(accessLog(log))
	r.Use(recoverPanic(log))
	r.Use(limitBody(deps.MaxRequestBytes))

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(r.Context(), w, log, apperror.NotFound("endpoint"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(r.Context(), w, log, apperror.New(apperror.CodeMethodNotAllowed,
			"method %s is not allowed for this endpoint", r.Method))
	})

	// Probes answer HEAD as well as GET. Container and load-balancer health
	// checks routinely use HEAD — `wget --spider` does, which is exactly what
	// docker-compose.yml runs — and a probe that 405s reads as an unhealthy
	// instance, so a GET-only health endpoint keeps a perfectly healthy
	// container out of service.
	health := &healthHandler{ping: deps.Ping, log: log}
	r.Get("/healthz", health.live)
	r.Head("/healthz", health.live)
	r.Get("/readyz", health.ready)
	r.Head("/readyz", health.ready)

	playbooks := &playbookHandler{svc: deps.Playbooks, log: log}
	alerts := &alertHandler{svc: deps.Alerts, log: log}
	investigations := &investigationHandler{svc: deps.Investigations, log: log}

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/playbooks", func(r chi.Router) {
			r.Post("/", playbooks.create)
			r.Get("/", playbooks.list)
			r.Get("/{playbookId}", playbooks.get)
			r.Post("/{playbookId}/versions", playbooks.createVersion)
		})

		// Versions are addressed at the top level rather than nested under
		// their playbook: a version ID is globally unique, and an investigation
		// references it directly, so requiring the playbook ID too would add a
		// lookup for callers without adding safety.
		r.Route("/playbook-versions/{versionId}", func(r chi.Router) {
			r.Get("/", playbooks.getVersion)
			r.Put("/", playbooks.replaceGraph)
			r.Post("/publish", playbooks.publish)
		})

		r.Route("/alerts", func(r chi.Router) {
			r.Post("/", alerts.create)
			r.Get("/{alertId}", alerts.get)
			r.Post("/{alertId}/investigations", investigations.start)
		})

		// No PUT or DELETE on steps anywhere: the audit trail is append-only,
		// and the absence of a route is the first line of that guarantee.
		r.Route("/investigations/{investigationId}", func(r chi.Router) {
			r.Get("/", investigations.get)
			r.Post("/decisions", investigations.submitDecision)
			r.Get("/report", investigations.report)
		})
	})

	return r
}
