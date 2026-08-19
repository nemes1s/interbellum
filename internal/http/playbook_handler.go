package http

import (
	"log/slog"
	"net/http"

	"github.com/indurex/interbellum/internal/domain/playbook"
	"github.com/indurex/interbellum/internal/http/httpdto"
	"github.com/indurex/interbellum/internal/service/playbookservice"
)

// playbookHandler serves the playbook design endpoints.
type playbookHandler struct {
	svc *playbookservice.Service
	log *slog.Logger
}

// create handles POST /api/v1/playbooks.
func (h *playbookHandler) create(w http.ResponseWriter, r *http.Request) {
	var req httpdto.CreatePlaybookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	in := playbook.NewPlaybook{
		Name:        req.Name,
		Description: req.Description,
		AlertType:   req.AlertType,
	}
	if req.Definition != nil {
		in.Graph = req.Definition.ToGraph()
	}

	pb, def, err := h.svc.Create(r.Context(), in)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	// The response carries the version summary rather than the full graph the
	// caller just sent; GET /playbook-versions/{id} returns the definition.
	writeJSON(w, h.log, http.StatusCreated, httpdto.ToPlaybookWithVersions(
		playbookservice.PlaybookWithVersions{
			Playbook: pb,
			Versions: []playbook.Version{def.Version},
		}))
}

// list handles GET /api/v1/playbooks.
func (h *playbookHandler) list(w http.ResponseWriter, r *http.Request) {
	playbooks, err := h.svc.List(r.Context(), r.URL.Query().Get("alert_type"))
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToPlaybookList(playbooks))
}

// get handles GET /api/v1/playbooks/{playbookId}.
func (h *playbookHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "playbookId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	result, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToPlaybookWithVersions(result))
}

// createVersion handles POST /api/v1/playbooks/{playbookId}/versions.
func (h *playbookHandler) createVersion(w http.ResponseWriter, r *http.Request) {
	playbookID, err := pathUUID(r, "playbookId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	// The body is optional — creating an empty draft is the common case — so an
	// absent body is accepted, but a malformed one is still an error. Testing
	// for an empty body rather than trusting Content-Length also covers
	// chunked requests, where the length is not known up front.
	var req httpdto.CreatePlaybookVersionRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	def, err := h.svc.CreateVersion(r.Context(), playbookID, req.CloneFromVersionID)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusCreated, httpdto.ToDefinition(def))
}

// getVersion handles GET /api/v1/playbook-versions/{versionId}.
func (h *playbookHandler) getVersion(w http.ResponseWriter, r *http.Request) {
	versionID, err := pathUUID(r, "versionId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	def, err := h.svc.GetVersion(r.Context(), versionID)
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToDefinition(def))
}

// replaceGraph handles PUT /api/v1/playbook-versions/{versionId}.
func (h *playbookHandler) replaceGraph(w http.ResponseWriter, r *http.Request) {
	versionID, err := pathUUID(r, "versionId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	var req httpdto.GraphInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	def, err := h.svc.ReplaceGraph(r.Context(), versionID, req.ToGraph())
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToDefinition(def))
}

// publish handles POST /api/v1/playbook-versions/{versionId}/publish.
func (h *playbookHandler) publish(w http.ResponseWriter, r *http.Request) {
	versionID, err := pathUUID(r, "versionId")
	if err != nil {
		writeError(r.Context(), w, h.log, err)
		return
	}

	def, err := h.svc.Publish(r.Context(), versionID)
	if err != nil {
		// A graph rejection arrives here as CodeInvalidPlaybookGraph carrying
		// every problem found, and writeError renders it as 422 with the full
		// details array — the designer's feedback loop.
		writeError(r.Context(), w, h.log, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, httpdto.ToDefinition(def))
}
