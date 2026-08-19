// Package investigation holds the runtime side of the domain: one alert being
// worked through one immutable playbook version, and the append-only audit
// trail that results.
//
// The central rule of this package — "given the current state and a candidate
// edge, what happens next" — lives in decision.go as a pure function with no
// I/O, so it is unit-testable without a database.
package investigation

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status is the investigation lifecycle: in_progress -> completed.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// ActorType distinguishes a human analyst from an automated agent. Both use
// exactly the same API; only this label differs.
type ActorType string

const (
	ActorHuman ActorType = "human"
	ActorAgent ActorType = "agent"
)

// Valid reports whether t is a known actor type.
func (t ActorType) Valid() bool { return t == ActorHuman || t == ActorAgent }

// Actor records who or what submitted a decision. The engine does not
// authenticate actors, it only records the claim; production would populate
// this from a verified identity rather than the request body (see README).
type Actor struct {
	Type ActorType
	ID   *string
}

// Investigation is the mutable "where are we now" projection of a run.
// It is NOT the audit trail: Step records are. CurrentNodeID exists so the
// hot path (what can I do next?) is a single row read.
type Investigation struct {
	ID                uuid.UUID
	AlertID           uuid.UUID
	PlaybookVersionID uuid.UUID
	Status            Status
	CurrentNodeID     uuid.UUID
	StartedAt         time.Time
	CompletedAt       *time.Time
	FinalResolution   *string
}

// IsCompleted reports whether the investigation has reached a terminal node.
func (i Investigation) IsCompleted() bool { return i.Status == StatusCompleted }

// Step is one append-only audit record: exactly one edge selected from exactly
// one node. There is no evidence-only step — see docs/domain-model.md §8.
type Step struct {
	ID                uuid.UUID
	InvestigationID   uuid.UUID
	PlaybookVersionID uuid.UUID
	SequenceNumber    int
	NodeID            uuid.UUID
	SelectedEdgeID    uuid.UUID
	Actor             Actor
	Rationale         *string
	// Evidence is a JSON array of evidence items, opaque to the engine beyond
	// being valid JSON. See EvidenceItem for the conventional shape.
	Evidence       []byte
	IdempotencyKey *string
	CreatedAt      time.Time
}

// EvidenceItem documents the conventional evidence shape. It is not persisted
// as a table: evidence is stored as a JSONB array on the step, because
// producers (humans, automation, other security systems, an LLM tool call)
// disagree about everything except roughly "what kind, one-line summary,
// arbitrary detail".
type EvidenceItem struct {
	Type    string          `json:"type"`
	Summary string          `json:"summary"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// DecisionInput is a request to advance an investigation by one edge.
type DecisionInput struct {
	EdgeID    uuid.UUID
	Actor     Actor
	Rationale *string
	Evidence  []byte
	// IdempotencyKey, when set, makes a retry of the identical request a no-op
	// instead of a second step. Unique per investigation.
	IdempotencyKey *string
}

// DecisionResult is what applying a decision produced.
type DecisionResult struct {
	Investigation Investigation
	Step          Step
	// Replayed is true when an Idempotency-Key matched an existing step and no
	// new step was written. The investigation returned is the *current* state,
	// which may have advanced further since the original decision.
	Replayed bool
}
