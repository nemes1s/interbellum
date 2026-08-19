package investigation

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for investigations.
type Repository interface {
	// Create persists a freshly-started investigation (see Start). It takes an
	// already-computed value rather than raw inputs because whether the
	// investigation is born completed is a domain decision, not a storage one.
	Create(ctx context.Context, inv Investigation) (Investigation, error)

	// Get returns an investigation by ID, or apperror.CodeNotFound.
	Get(ctx context.Context, id uuid.UUID) (Investigation, error)

	// Steps returns the append-only audit trail in sequence order. This — not
	// Investigation.CurrentNodeID — is the authoritative history.
	Steps(ctx context.Context, investigationID uuid.UUID) ([]Step, error)

	// ApplyDecision advances an investigation by one edge, atomically.
	//
	// This is the one operation whose correctness depends on doing several
	// statements as a unit, so the implementation owns the transaction:
	//   BEGIN
	//   SELECT ... FROM investigations WHERE id = $1 FOR UPDATE
	//   load the candidate edge + destination node (scoped to this version)
	//   Decide(...)                       <- pure domain rule, no I/O
	//   INSERT investigation_steps        <- sequence_number = MAX + 1
	//   UPDATE investigations             <- current node, maybe completion
	//   COMMIT
	// The row lock serializes concurrent submissions on the same investigation,
	// so exactly one of two racing decisions applies and the other is rejected
	// with CodeInvalidTransition (its edge no longer leaves the current node).
	//
	// When in.IdempotencyKey matches a step already recorded for this
	// investigation, no new step is written: an equivalent request returns the
	// current state with Replayed=true, and a differing one is rejected with
	// apperror.CodeIdempotencyKeyReused.
	ApplyDecision(ctx context.Context, investigationID uuid.UUID, in DecisionInput) (DecisionResult, error)
}
