// Package apperror defines the single error type that crosses layer
// boundaries, and the stable code vocabulary that goes with it.
//
// It depends on nothing but the standard library's errors and fmt — in
// particular it does NOT import net/http. The mapping from Code to HTTP status
// lives in internal/http, because that is a transport concern: a code means
// "the investigation is already completed", and only the HTTP layer decides
// that this is a 409. Keeping the mapping out of here is what lets
// internal/domain use these codes while still having no transport dependency,
// direct or transitive.
//
// Domain, service and repository code returns *apperror.Error; the HTTP layer
// is the only place that turns it into a status code and body. Raw database
// errors never reach a client: the repository layer translates the ones it
// understands (unique violations, FK violations) into codes and wraps anything
// else as CodeInternal.
package apperror

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error identifier. Clients (including
// automated agents) are expected to branch on these, not on message text.
type Code string

const (
	// CodeBadRequest — malformed syntax, unparseable body, bad UUID. 400.
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeValidationFailed — well-formed but semantically invalid input. 400.
	CodeValidationFailed Code = "VALIDATION_FAILED"
	// CodeNotFound — referenced resource does not exist. 404.
	CodeNotFound Code = "RESOURCE_NOT_FOUND"
	// CodeConflict — the request contradicts current resource state. 409.
	CodeConflict Code = "CONFLICT"
	// CodeVersionNotDraft — attempted to modify a published/archived version. 409.
	CodeVersionNotDraft Code = "PLAYBOOK_VERSION_NOT_DRAFT"
	// CodeVersionNotPublished — attempted to investigate against a non-published version. 409.
	CodeVersionNotPublished Code = "PLAYBOOK_VERSION_NOT_PUBLISHED"
	// CodeInvalidPlaybookGraph — publish-time graph validation failed. 422.
	CodeInvalidPlaybookGraph Code = "INVALID_PLAYBOOK_GRAPH"
	// CodeInvestigationCompleted — decision submitted to a finished investigation. 409.
	CodeInvestigationCompleted Code = "INVESTIGATION_ALREADY_COMPLETED"
	// CodeInvalidTransition — the selected edge is not available at the current node. 409.
	CodeInvalidTransition Code = "INVALID_TRANSITION"
	// CodeIdempotencyKeyReused — same key, different request body. 409.
	CodeIdempotencyKeyReused Code = "IDEMPOTENCY_KEY_REUSED"
	// CodeAlertTypeMismatch — the playbook is registered for a different
	// alert_type than the alert being investigated. 409.
	CodeAlertTypeMismatch Code = "ALERT_TYPE_MISMATCH"
	// CodeMethodNotAllowed — the path exists but not for this HTTP method. 405.
	CodeMethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	// CodePayloadTooLarge — request body exceeded the configured limit. 413.
	CodePayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	// CodeNotReady — readiness probe failed (dependency unavailable). 503.
	CodeNotReady Code = "NOT_READY"
	// CodeInternal — anything unexpected. 500. Message is never derived from
	// the underlying error, so database details cannot leak.
	CodeInternal Code = "INTERNAL_ERROR"
)

// Error is the application error carried across layers.
type Error struct {
	Code    Code
	Message string
	// Details carries structured, code-specific context (e.g. the list of
	// graph validation issues for CodeInvalidPlaybookGraph). Serialized as-is.
	Details []any
	// cause is retained for logging only; it is never serialized to a client.
	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is/As for logging purposes.
func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches a non-client-visible cause for logging.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

// New builds an *Error with a code and formatted message.
func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Common constructors, kept short because they appear on many call sites.

func BadRequest(format string, args ...any) *Error {
	return New(CodeBadRequest, format, args...)
}

func Validation(format string, args ...any) *Error {
	return New(CodeValidationFailed, format, args...)
}

func NotFound(resource string) *Error {
	return New(CodeNotFound, "%s not found", resource)
}

func Conflict(code Code, format string, args ...any) *Error {
	return New(code, format, args...)
}

// Internal wraps an unexpected error. The client-facing message is fixed so
// that database or driver detail cannot leak; cause is preserved for logs.
func Internal(cause error) *Error {
	return &Error{
		Code:    CodeInternal,
		Message: "internal server error",
		cause:   cause,
	}
}

// From coerces any error into an *Error. Errors that are not already
// application errors become CodeInternal, which guarantees that an
// unclassified failure is reported as 500 with an opaque message rather than
// accidentally surfacing driver text.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}

// IsCode reports whether err is an *Error carrying the given code.
func IsCode(err error, code Code) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Code == code
}
