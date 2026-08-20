package http

import (
	"net/http"

	"github.com/nemes1s/interbellum/internal/apperror"
)

// statusFor maps an application error code to an HTTP status.
//
// This mapping lives in the HTTP layer rather than alongside the codes
// themselves because it is a transport decision: apperror.CodeInvalidTransition
// means "that edge is not available from the current node", and it is only
// this layer that decides such a thing is a 409. Keeping it here is also what
// lets internal/domain use the code vocabulary without acquiring a net/http
// dependency, transitively or otherwise.
//
// Unknown codes map to 500, which is the safe default for a code someone added
// without classifying it.
func statusFor(code apperror.Code) int {
	switch code {
	case apperror.CodeBadRequest, apperror.CodeValidationFailed:
		return http.StatusBadRequest
	case apperror.CodeNotFound:
		return http.StatusNotFound
	case apperror.CodeConflict,
		apperror.CodeVersionNotDraft,
		apperror.CodeVersionNotPublished,
		apperror.CodeInvestigationCompleted,
		apperror.CodeInvalidTransition,
		apperror.CodeIdempotencyKeyReused,
		apperror.CodeAlertTypeMismatch:
		return http.StatusConflict
	case apperror.CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case apperror.CodeInvalidPlaybookGraph:
		return http.StatusUnprocessableEntity
	case apperror.CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperror.CodeNotReady:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
