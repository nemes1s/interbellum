// Package http wires the API: routing, middleware, request decoding, response
// encoding, and the single place where application errors become HTTP status
// codes.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/apperror"
)

// errorBody is the machine-readable error envelope every failing endpoint
// returns (the Error schema in api/openapi.yaml).
type errorBody struct {
	Code    apperror.Code `json:"code"`
	Message string        `json:"message"`
	Details []any         `json:"details,omitempty"`
}

// writeJSON encodes a successful response.
func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged — the
		// client will see a truncated body and treat it as a transport error.
		log.Error("failed to encode response body", slog.Any("error", err))
	}
}

// writeError maps an application error to its HTTP representation.
//
// This is the only place errors become status codes, which is what keeps the
// mapping consistent across handlers, and it is where the split between what
// the client sees and what operators see is enforced: 5xx responses carry a
// fixed opaque message while the underlying cause goes to the log.
func writeError(ctx context.Context, w http.ResponseWriter, log *slog.Logger, err error) {
	appErr := apperror.From(err)
	status := appErr.HTTPStatus()

	if status >= http.StatusInternalServerError {
		log.ErrorContext(ctx, "request failed",
			slog.String("code", string(appErr.Code)),
			slog.Any("error", err))
	} else {
		log.DebugContext(ctx, "request rejected",
			slog.String("code", string(appErr.Code)),
			slog.String("message", appErr.Message),
			slog.Int("status", status))
	}

	writeJSON(w, log, status, errorBody{
		Code:    appErr.Code,
		Message: appErr.Message,
		Details: appErr.Details,
	})
}

// decodeJSON reads and strictly decodes a request body.
//
// Unknown fields are rejected rather than ignored: a client that misspells
// `rationale` should be told, not have its audit trail silently lose the
// field. Trailing content is rejected for the same reason.
func decodeJSON(r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType := strings.TrimSpace(strings.Split(ct, ";")[0])
		if !strings.EqualFold(mediaType, "application/json") {
			return apperror.BadRequest("Content-Type must be application/json, got %q", mediaType)
		}
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperror.BadRequest("request body must contain exactly one JSON object")
	}
	return nil
}

// decodeOptionalJSON decodes a body that a caller may legitimately omit,
// leaving dst untouched when there is nothing to read. A body that is present
// but malformed is still rejected — "optional" means absent, not "anything
// goes".
func decodeOptionalJSON(r *http.Request, dst any) error {
	if err := decodeJSON(r, dst); err != nil {
		if apperror.IsCode(err, apperror.CodeBadRequest) && errors.Is(err, errEmptyBody) {
			return nil
		}
		return err
	}
	return nil
}

// errEmptyBody distinguishes "the caller sent no body" from other decode
// failures, so decodeOptionalJSON can tell them apart without matching on
// message text.
var errEmptyBody = errors.New("empty request body")

// decodeError turns a JSON decoding failure into a client-readable message.
// The body-size limit surfaces here because http.MaxBytesReader reports the
// overflow at read time.
func decodeError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return apperror.New(apperror.CodePayloadTooLarge,
			"request body exceeds the %d byte limit", maxBytesErr.Limit)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return apperror.BadRequest("malformed JSON at byte offset %d", syntaxErr.Offset)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return apperror.BadRequest("field %q must be of type %s", typeErr.Field, typeErr.Type)
	}

	if errors.Is(err, io.EOF) {
		return apperror.BadRequest("request body is required").WithCause(errEmptyBody)
	}

	// DisallowUnknownFields reports via a plain error; surfacing its text is
	// safe (it names the offending field) and far more useful than "invalid".
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return apperror.BadRequest("%s", err.Error())
	}
	return apperror.BadRequest("request body is not valid JSON")
}

// pathUUID parses a UUID path parameter, reporting a 400 rather than letting a
// malformed ID reach the database as a failed query.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.BadRequest("%s must be a UUID, got %q", name, raw)
	}
	return id, nil
}
