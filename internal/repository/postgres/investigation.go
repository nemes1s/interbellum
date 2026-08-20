package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// InvestigationRepository implements investigation.Repository against
// PostgreSQL, including the one operation whose correctness depends on a
// transaction: ApplyDecision.
type InvestigationRepository struct {
	pool *pgxpool.Pool
}

// NewInvestigationRepository constructs the repository.
func NewInvestigationRepository(pool *pgxpool.Pool) *InvestigationRepository {
	return &InvestigationRepository{pool: pool}
}

var _ investigation.Repository = (*InvestigationRepository)(nil)

const investigationColumns = `id, alert_id, playbook_version_id, status, current_node_id,
	started_at, completed_at, final_resolution`

const stepColumns = `id, investigation_id, playbook_version_id, sequence_number, node_id,
	selected_edge_id, actor_type, actor_id, rationale, evidence, idempotency_key, created_at`

// Create persists an already-computed initial investigation state. Whether the
// investigation is born completed (terminal root) was decided by
// investigation.Start, not here.
func (r *InvestigationRepository) Create(ctx context.Context, inv investigation.Investigation) (investigation.Investigation, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO investigations
			(alert_id, playbook_version_id, status, current_node_id, started_at, completed_at, final_resolution)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+investigationColumns,
		inv.AlertID, inv.PlaybookVersionID, string(inv.Status), inv.CurrentNodeID,
		inv.StartedAt, inv.CompletedAt, inv.FinalResolution)

	created, err := scanInvestigation(row)
	if err != nil {
		return investigation.Investigation{}, translate(err)
	}
	return created, nil
}

// GetWithSteps returns an investigation and its ordered audit trail from one
// consistent snapshot.
//
// The REPEATABLE READ transaction is the whole point: under PostgreSQL's
// default READ COMMITTED, every statement takes a *fresh* snapshot, so two
// queries in one transaction can still straddle a concurrent decision's
// commit. REPEATABLE READ pins both reads to the same instant. It is read-only
// and touches two indexed rows sets, so the cost is negligible.
func (r *InvestigationRepository) GetWithSteps(ctx context.Context, id uuid.UUID) (investigation.Investigation, []investigation.Step, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return investigation.Investigation{}, nil, translate(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `SELECT `+investigationColumns+` FROM investigations WHERE id = $1`, id)
	inv, err := scanInvestigation(row)
	if err != nil {
		return investigation.Investigation{}, nil, notFound(err, "investigation")
	}

	steps, err := loadSteps(ctx, tx, id)
	if err != nil {
		return investigation.Investigation{}, nil, err
	}
	return inv, steps, nil
}

func loadSteps(ctx context.Context, q querier, investigationID uuid.UUID) ([]investigation.Step, error) {
	rows, err := q.Query(ctx, `
		SELECT `+stepColumns+`
		FROM investigation_steps
		WHERE investigation_id = $1
		ORDER BY sequence_number`, investigationID)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	steps := []investigation.Step{}
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, translate(err)
		}
		steps = append(steps, s)
	}
	return steps, translate(rows.Err())
}

// ApplyDecision advances an investigation by exactly one edge, atomically.
//
// The row lock taken on the investigation is what makes concurrent agents
// safe: two simultaneous submissions serialize, the first applies, and the
// second re-reads the *updated* current node and is rejected because its edge
// no longer originates there. Without the lock both could read the same
// current node and write conflicting steps.
func (r *InvestigationRepository) ApplyDecision(
	ctx context.Context,
	investigationID uuid.UUID,
	in investigation.DecisionInput,
) (investigation.DecisionResult, error) {
	var result investigation.DecisionResult

	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		inv, err := lockInvestigation(ctx, tx, investigationID)
		if err != nil {
			return err
		}

		// An idempotent retry must be recognised before anything else: the
		// original decision may have already completed the investigation, in
		// which case every check below would (correctly, but unhelpfully)
		// reject the retry as advancing a finished investigation.
		if in.IdempotencyKey != nil {
			existing, found, err := findStepByIdempotencyKey(ctx, tx, investigationID, in)
			if err != nil {
				return err
			}
			if found {
				result = investigation.DecisionResult{
					Investigation: inv,
					Step:          existing,
					Replayed:      true,
				}
				return nil
			}
		}

		// Checked explicitly, before the edge lookup, so that submitting any
		// edge to a finished investigation reports "already completed" rather
		// than the less informative "that edge isn't available here".
		// investigation.Decide re-checks it; that is where the rule lives.
		if inv.IsCompleted() {
			return apperror.Conflict(apperror.CodeInvestigationCompleted,
				"investigation %s is already completed and cannot be advanced", investigationID)
		}

		// Scoping the lookup to the investigation's own playbook version is
		// what rejects an edge borrowed from a different playbook: it simply
		// does not exist within this query's scope.
		edge, destination, err := loadEdgeWithDestination(ctx, tx, inv.PlaybookVersionID, in.EdgeID)
		if err != nil {
			return err
		}

		next, err := investigation.Decide(inv, edge, destination, nowUTC())
		if err != nil {
			return err
		}

		step, err := insertStep(ctx, tx, inv, in, edge)
		if err != nil {
			return err
		}

		updated, err := updateInvestigationState(ctx, tx, next)
		if err != nil {
			return err
		}

		result = investigation.DecisionResult{Investigation: updated, Step: step}
		return nil
	})
	if err != nil {
		return investigation.DecisionResult{}, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func lockInvestigation(ctx context.Context, tx pgx.Tx, id uuid.UUID) (investigation.Investigation, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+investigationColumns+` FROM investigations WHERE id = $1 FOR UPDATE`, id)
	inv, err := scanInvestigation(row)
	if err != nil {
		return investigation.Investigation{}, notFound(err, "investigation")
	}
	return inv, nil
}

// findStepByIdempotencyKey looks for a step already recorded under this key.
//
// Equivalence is evaluated in SQL so that `evidence` is compared as JSONB —
// semantically, ignoring key order and whitespace — rather than as raw bytes,
// which would make a re-serialized retry of an identical request look
// different. A key reused with a genuinely different request is a client bug
// and is rejected rather than quietly returning the wrong step.
func findStepByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	investigationID uuid.UUID,
	in investigation.DecisionInput,
) (investigation.Step, bool, error) {
	encodedEvidence, err := encodeEvidence(in.Evidence)
	if err != nil {
		return investigation.Step{}, false, err
	}

	row := tx.QueryRow(ctx, `
		SELECT `+stepColumns+`,
			(selected_edge_id = $3
				AND actor_type = $4
				AND actor_id IS NOT DISTINCT FROM $5
				AND rationale IS NOT DISTINCT FROM $6
				AND evidence IS NOT DISTINCT FROM $7::jsonb) AS equivalent
		FROM investigation_steps
		WHERE investigation_id = $1 AND idempotency_key = $2`,
		investigationID, *in.IdempotencyKey,
		in.EdgeID, string(in.Actor.Type), in.Actor.ID, in.Rationale, encodedEvidence)

	var (
		step        investigation.Step
		rawEvidence []byte
		equivalent  bool
	)
	err = row.Scan(&step.ID, &step.InvestigationID, &step.PlaybookVersionID, &step.SequenceNumber,
		&step.NodeID, &step.SelectedEdgeID, &step.Actor.Type, &step.Actor.ID, &step.Rationale,
		&rawEvidence, &step.IdempotencyKey, &step.CreatedAt, &equivalent)
	switch {
	case isNoRows(err):
		return investigation.Step{}, false, nil
	case err != nil:
		return investigation.Step{}, false, translate(err)
	case !equivalent:
		return investigation.Step{}, false, apperror.Conflict(apperror.CodeIdempotencyKeyReused,
			"idempotency key %q was already used on this investigation with a different request",
			*in.IdempotencyKey)
	}

	step.Evidence, err = decodeEvidence(rawEvidence)
	if err != nil {
		return investigation.Step{}, false, err
	}
	return step, true, nil
}

// loadEdgeWithDestination fetches an edge and the node it points at, both
// scoped to one playbook version, in a single round trip.
func loadEdgeWithDestination(
	ctx context.Context,
	tx pgx.Tx,
	versionID, edgeID uuid.UUID,
) (playbook.Edge, playbook.Node, error) {
	row := tx.QueryRow(ctx, `
		SELECT e.id, e.from_node_id, e.to_node_id, e.label, e.description, e.sort_order,
			n.id, n.kind, n.title, n.description, n.terminal_resolution, n.metadata
		FROM playbook_edges e
		JOIN playbook_nodes n
			ON n.id = e.to_node_id AND n.playbook_version_id = e.playbook_version_id
		WHERE e.id = $1 AND e.playbook_version_id = $2`, edgeID, versionID)

	var (
		edge playbook.Edge
		node playbook.Node
	)
	err := row.Scan(&edge.ID, &edge.FromNodeID, &edge.ToNodeID, &edge.Label, &edge.Description, &edge.SortOrder,
		&node.ID, &node.Kind, &node.Title, &node.Description, &node.TerminalResolution, &node.Metadata)
	switch {
	case isNoRows(err):
		// Deliberately a transition error, not a 404: from the client's point
		// of view the problem is "that is not a choice you can make here",
		// whether the edge is unknown or belongs to another playbook.
		return playbook.Edge{}, playbook.Node{}, apperror.Conflict(apperror.CodeInvalidTransition,
			"edge %s is not part of this investigation's playbook version", edgeID)
	case err != nil:
		return playbook.Edge{}, playbook.Node{}, translate(err)
	}
	return edge, node, nil
}

// insertStep appends the audit record, taking the next sequence number under
// the investigation row lock so numbering stays gapless and strictly
// increasing.
func insertStep(
	ctx context.Context,
	tx pgx.Tx,
	inv investigation.Investigation,
	in investigation.DecisionInput,
	edge playbook.Edge,
) (investigation.Step, error) {
	encodedEvidence, err := encodeEvidence(in.Evidence)
	if err != nil {
		return investigation.Step{}, err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO investigation_steps
			(investigation_id, playbook_version_id, sequence_number, node_id, selected_edge_id,
			 actor_type, actor_id, rationale, evidence, idempotency_key)
		VALUES (
			$1, $2,
			(SELECT COALESCE(MAX(sequence_number), 0) + 1
			 FROM investigation_steps WHERE investigation_id = $1),
			$3, $4, $5, $6, $7, $8::jsonb, $9)
		RETURNING `+stepColumns,
		inv.ID, inv.PlaybookVersionID, inv.CurrentNodeID, edge.ID,
		string(in.Actor.Type), in.Actor.ID, in.Rationale, encodedEvidence, in.IdempotencyKey)

	step, err := scanStep(row)
	if err != nil {
		return investigation.Step{}, translate(err)
	}
	return step, nil
}

func updateInvestigationState(ctx context.Context, tx pgx.Tx, next investigation.Investigation) (investigation.Investigation, error) {
	row := tx.QueryRow(ctx, `
		UPDATE investigations
		SET current_node_id = $2, status = $3, completed_at = $4, final_resolution = $5
		WHERE id = $1
		RETURNING `+investigationColumns,
		next.ID, next.CurrentNodeID, string(next.Status), next.CompletedAt, next.FinalResolution)

	updated, err := scanInvestigation(row)
	if err != nil {
		return investigation.Investigation{}, translate(err)
	}
	return updated, nil
}

func scanInvestigation(s scanner) (investigation.Investigation, error) {
	var inv investigation.Investigation
	err := s.Scan(&inv.ID, &inv.AlertID, &inv.PlaybookVersionID, &inv.Status, &inv.CurrentNodeID,
		&inv.StartedAt, &inv.CompletedAt, &inv.FinalResolution)
	return inv, err
}

func scanStep(s scanner) (investigation.Step, error) {
	var (
		step investigation.Step
		raw  []byte
	)
	err := s.Scan(&step.ID, &step.InvestigationID, &step.PlaybookVersionID, &step.SequenceNumber,
		&step.NodeID, &step.SelectedEdgeID, &step.Actor.Type, &step.Actor.ID, &step.Rationale,
		&raw, &step.IdempotencyKey, &step.CreatedAt)
	if err != nil {
		return investigation.Step{}, err
	}
	step.Evidence, err = decodeEvidence(raw)
	return step, err
}

// encodeEvidence renders evidence for storage. Absent evidence becomes SQL
// NULL rather than an empty string (which would fail the ::jsonb cast) or an
// empty array (which would make "no evidence" and "an empty list" two
// different stored values that must then compare equal for idempotency).
func encodeEvidence(items []investigation.EvidenceItem) (any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("encode evidence: %w", err))
	}
	return string(encoded), nil
}

// decodeEvidence reads evidence back out of JSONB.
func decodeEvidence(raw []byte) ([]investigation.EvidenceItem, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []investigation.EvidenceItem
	if err := json.Unmarshal(raw, &items); err != nil {
		// Only reachable if something wrote a shape this code never writes.
		return nil, apperror.Internal(fmt.Errorf("decode stored evidence: %w", err))
	}
	return items, nil
}
