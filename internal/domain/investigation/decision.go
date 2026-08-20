package investigation

import (
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// Start computes the initial state of an investigation against a published
// playbook version, given that version's root node.
//
// A version whose root is itself terminal is degenerate but valid: the
// investigation is born completed, with zero steps. No synthetic "arrived at
// the root" step is invented, which keeps the invariant "every step is an edge
// selection" true without exception. See docs/domain-model.md §4.
func Start(alertID uuid.UUID, version playbook.Version, root playbook.Node, now time.Time) (Investigation, error) {
	if version.Status != playbook.StatusPublished {
		return Investigation{}, apperror.New(apperror.CodeVersionNotPublished,
			"playbook version %s is %s; only published versions can back an investigation",
			version.ID, version.Status)
	}

	inv := Investigation{
		AlertID:           alertID,
		PlaybookVersionID: version.ID,
		Status:            StatusInProgress,
		CurrentNodeID:     root.ID,
		StartedAt:         now,
	}

	if root.Kind == playbook.KindTerminal {
		inv.Status = StatusCompleted
		inv.CompletedAt = &now
		inv.FinalResolution = root.TerminalResolution
	}
	return inv, nil
}

// Decide is the pure heart of the engine: given the current investigation, the
// edge a client selected, and the node that edge leads to, it either returns
// the resulting investigation state or explains why the decision is illegal.
//
// It performs no I/O and never trusts the client for anything but the edge ID —
// the destination node comes from the edge, so a client cannot jump to an
// arbitrary node. The result is a new value rather than a mutation, so the
// caller (the repository, inside its transaction) decides when it becomes
// durable.
//
// The caller is responsible for having loaded `edge` and `destination` from the
// investigation's own playbook version; the database enforces that too, via
// composite foreign keys on investigation_steps.
func Decide(inv Investigation, edge playbook.Edge, destination playbook.Node, now time.Time) (Investigation, error) {
	if inv.IsCompleted() {
		return Investigation{}, apperror.New(apperror.CodeInvestigationCompleted,
			"investigation %s is already completed and cannot be advanced", inv.ID)
	}

	// The decisive check: the selected edge must leave the node the
	// investigation is actually sitting on. This is what stops a client from
	// replaying a stale choice or skipping ahead in the graph.
	if edge.FromNodeID != inv.CurrentNodeID {
		return Investigation{}, apperror.New(apperror.CodeInvalidTransition,
			"edge %s originates at node %s, but the investigation is at node %s",
			edge.ID, edge.FromNodeID, inv.CurrentNodeID)
	}

	if edge.ToNodeID != destination.ID {
		// Defensive: a caller loaded a destination that is not this edge's
		// target. Not reachable through the HTTP API; it would be a bug.
		return Investigation{}, apperror.Internal(apperror.New(apperror.CodeInternal,
			"edge %s points at node %s but node %s was loaded",
			edge.ID, edge.ToNodeID, destination.ID))
	}

	next := inv
	next.CurrentNodeID = destination.ID

	if destination.Kind == playbook.KindTerminal {
		next.Status = StatusCompleted
		next.CompletedAt = &now
		next.FinalResolution = destination.TerminalResolution
	}
	return next, nil
}
