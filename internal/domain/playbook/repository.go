package playbook

import (
	"context"

	"github.com/google/uuid"
)

// NewPlaybook is the input for creating a playbook together with its initial
// draft version. The graph is optional — a draft may legitimately start empty.
type NewPlaybook struct {
	Name        string
	Description string
	AlertType   string
	Graph       Graph
}

// Repository is the persistence port for playbook design.
//
// Several methods are deliberately coarse-grained ("do the whole thing
// atomically") rather than a set of fine-grained primitives the service would
// stitch together. That is what lets the implementation own a transaction
// without the codebase needing a generic Transactor abstraction — see
// docs/package-structure.md.
type Repository interface {
	// Create inserts the playbook, its version 1 draft, and the initial graph
	// in one transaction.
	Create(ctx context.Context, in NewPlaybook) (Playbook, Definition, error)

	// List returns playbooks, optionally filtered by alert type.
	List(ctx context.Context, alertType string) ([]Playbook, error)

	// Get returns a playbook with the summaries of all its versions, newest
	// version number first.
	Get(ctx context.Context, id uuid.UUID) (Playbook, []Version, error)

	// CreateVersion adds a new draft version to an existing playbook, with the
	// next version number. When cloneFrom is non-nil the new draft's graph is
	// seeded with a structural copy of that version's graph (fresh node/edge
	// IDs, so the two versions never share rows).
	CreateVersion(ctx context.Context, playbookID uuid.UUID, cloneFrom *uuid.UUID) (Definition, error)

	// GetVersion returns a version with its full graph.
	GetVersion(ctx context.Context, versionID uuid.UUID) (Definition, error)

	// ReplaceGraph replaces a draft version's entire graph in one transaction.
	// It fails with apperror.CodeVersionNotDraft if the version is published or
	// archived — published definitions are immutable.
	ReplaceGraph(ctx context.Context, versionID uuid.UUID, g Graph) (Definition, error)

	// Publish validates the version's graph and, only if it is valid, marks the
	// version published. Load-validate-write happens under a row lock so a
	// concurrent ReplaceGraph cannot slip an unvalidated graph in between.
	//
	// Returns apperror.CodeInvalidPlaybookGraph (with the full issue list in
	// Details) when validation fails, and apperror.CodeVersionNotDraft when the
	// version is not a draft.
	//
	// Note there is deliberately no "resolve the published version for this
	// alert type" method. The assignment offers server-side playbook
	// resolution as optional; the API contract requires callers to name a
	// playbook_version_id explicitly, and keeping it that way means the
	// version an investigation is bound to is always the caller's recorded
	// choice rather than whatever happened to be published at that instant.
	Publish(ctx context.Context, versionID uuid.UUID) (Definition, error)
}
