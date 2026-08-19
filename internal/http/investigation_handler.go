package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/http/httpdto"
	"github.com/indurex/interbellum/internal/service/investigationservice"
)

// idempotencyKeyHeader lets an agent make decision submission safe to retry.
const idempotencyKeyHeader = "Idempotency-Key"

// maxIdempotencyKeyLength bounds the key so a client cannot use it as
// unbounded storage. 255 is generous for a UUID or a request hash.
const maxIdempotencyKeyLength = 255

// investigationHandler serves the investigation lifecycle endpoints.
type investigationHandler struct {
	svc *investigationservice.Service
	log *slog.Logger
}

// start handles POST /api/v1/alerts/{alertId}/investigations.
func (h *investigationHandler) start(w http.ResponseWriter, r *http.Request) {
	alertID, err := pathUUID(r, "alertId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	var req httpdto.StartInvestigationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	state, err := h.svc.Start(r.Context(), investigationservice.StartInput{
		AlertID:           alertID,
		PlaybookVersionID: req.PlaybookVersionID,
	})
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusCreated, httpdto.ToInvestigationState(state))
}

// get handles GET /api/v1/investigations/{investigationId} — the endpoint an
// agent polls to work out what it may do next.
func (h *investigationHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "investigationId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	state, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToInvestigationState(state))
}

// submitDecision handles POST /api/v1/investigations/{investigationId}/decisions.
func (h *investigationHandler) submitDecision(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "investigationId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	idempotencyKey, err := idempotencyKeyFrom(r)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	var req httpdto.SubmitDecisionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	state, err := h.svc.SubmitDecision(r.Context(), id, req.ToDecisionInput(idempotencyKey))
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToInvestigationState(state))
}

// report handles GET /api/v1/investigations/{investigationId}/report.
func (h *investigationHandler) report(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "investigationId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	report, err := h.svc.Report(r.Context(), id)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToInvestigationReport(report))
}

// idempotencyKeyFrom extracts and bounds the optional retry key.
func idempotencyKeyFrom(r *http.Request) (*string, error) {
	raw := strings.TrimSpace(r.Header.Get(idempotencyKeyHeader))
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxIdempotencyKeyLength {
		return nil, apperror.BadRequest("%s must be at most %d characters",
			idempotencyKeyHeader, maxIdempotencyKeyLength)
	}
	return &raw, nil
}
