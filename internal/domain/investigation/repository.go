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

	// GetWithSteps returns an investigation together with its append-only audit
	// trail in sequence order, read from a single database snapshot.
	//
	// The two are returned together, rather than as separate calls, because a
	// concurrent decision commits between them otherwise: a caller could read
	// the investigation at node A, then read a step list that already contains
	// the A->B transition, and report a current node its own history
	// contradicts. Nothing is corrupted by that — a stale decision is still
	// rejected on submission — but it is a confusing thing to hand an agent
	// that is deciding what to do next.
	//
	// The steps, not Investigation.CurrentNodeID, are the authoritative history.
	GetWithSteps(ctx context.Context, id uuid.UUID) (Investigation, []Step, error)

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
