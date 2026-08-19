package http

import (
	"log/slog"
	"net/http"

	"github.com/indurex/interbellum/internal/http/httpdto"
	"github.com/indurex/interbellum/internal/service/alertservice"
)

// alertHandler serves alert ingestion and retrieval.
type alertHandler struct {
	svc *alertservice.Service
	log *slog.Logger
}

// create handles POST /api/v1/alerts.
//
// Returns 201 for a new alert and 200 when an alert with the same external_id
// already existed — the distinction lets a caller tell whether its retry was
// the write that landed, without either response being an error.
func (h *alertHandler) create(w http.ResponseWriter, r *http.Request) {
	var req httpdto.CreateAlertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	created, isNew, err := h.svc.Create(r.Context(), req.ToNewAlert())
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	writeJSON(w, h.log, status, httpdto.ToAlert(created))
}

// get handles GET /api/v1/alerts/{alertId}.
func (h *alertHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "alertId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	found, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToAlert(found))
}
