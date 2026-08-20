// Package investigationservice orchestrates running an investigation: starting
// one against a published playbook version, reporting what can be done next,
// applying decisions, and producing the audit report.
//
// It is the only service that reads from more than one repository, because an
// investigation is only meaningful in combination with its alert and its
// playbook version.
package investigationservice

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/alert"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// Service exposes the investigation use cases to the HTTP layer.
type Service struct {
	investigations investigation.Repository
	alerts         alert.Repository
	playbooks      playbook.Repository
	log            *slog.Logger
}

// New constructs the service.
func New(
	investigations investigation.Repository,
	alerts alert.Repository,
	playbooks playbook.Repository,
	log *slog.Logger,
) *Service {
	return &Service{investigations: investigations, alerts: alerts, playbooks: playbooks, log: log}
}

// State is everything a UI or agent needs to decide its next action in one
// response: the alert being investigated, where the investigation currently
// stands, what can be chosen from here, and what has happened so far.
//
// The alert is included in full rather than by ID so that an agent resuming an
// investigation — an LLM reasoning over the alert payload and the evidence
// gathered so far — does not need a second request to reconstruct context.
type State struct {
	Investigation    investigation.Investigation
	Alert            alert.Alert
	CurrentNode      playbook.Node
	AvailableChoices []playbook.Edge
	Steps            []investigation.Step
}

// Report is the full audit record of an investigation.
//
// The canonical playbook graph and the investigation-specific path are kept
// separate rather than annotating graph nodes with was_visited/was_selected
// flags. The graph is a property of the playbook version and is identical for
// every investigation that used it; the path is a property of this one run.
// Keeping them apart means a UI can cache the graph per version, a diff of two
// investigations on the same playbook compares two small path arrays, and the
// report of a completed investigation stays byte-identical no matter how many
// other investigations run against the same version.
type Report struct {
	Investigation investigation.Investigation
	Alert         alert.Alert
	Definition    playbook.Definition
	Steps         []investigation.Step
}

// StartInput names the alert and the published playbook version to investigate
// it with.
type StartInput struct {
	AlertID           uuid.UUID
	PlaybookVersionID uuid.UUID
}

// Start begins an investigation of an alert against a published playbook
// version.
func (s *Service) Start(ctx context.Context, in StartInput) (State, error) {
	alertRecord, err := s.alerts.Get(ctx, in.AlertID)
	if err != nil {
		return State{}, err
	}

	definition, err := s.playbooks.GetVersion(ctx, in.PlaybookVersionID)
	if err != nil {
		return State{}, err
	}

	// The playbook must be registered for this alert's type.
	//
	// This is a deliberate position rather than an oversight: without it,
	// alert_type would be decorative — a label nothing enforces — and a caller
	// could run the PLC-register-write procedure against a failed-login alert,
	// producing an audit record whose questions never applied to the alert
	// they were asked about. Requiring the match is what gives alert_type
	// semantic weight in v1.
	//
	// The cost is that a deliberately generic "triage anything" playbook is
	// not expressible today. If that becomes wanted, the clean extension is an
	// explicit opt-out on the playbook (a wildcard alert_type, or an
	// `applies_to_any_alert_type` flag) rather than dropping the check — see
	// README "Key decisions".
	owner, _, err := s.playbooks.Get(ctx, definition.Version.PlaybookID)
	if err != nil {
		return State{}, err
	}
	if owner.AlertType != alertRecord.AlertType {
		return State{}, apperror.Conflict(apperror.CodeAlertTypeMismatch,
			"playbook version %s handles alert_type %q, but alert %s is of type %q",
			in.PlaybookVersionID, owner.AlertType, alertRecord.ID, alertRecord.AlertType)
	}

	// Resolve the root node. A draft is allowed to have no root at all, so the
	// "not published" rule is reported first — otherwise pointing an
	// investigation at an empty draft would complain about a missing root
	// rather than about the thing the caller actually got wrong.
	root, rootFound := playbook.Node{}, false
	if definition.Version.RootNodeID != nil {
		root, rootFound = definition.Graph.NodeByID(*definition.Version.RootNodeID)
	}
	if !rootFound {
		if definition.Version.Status != playbook.StatusPublished {
			return State{}, apperror.Conflict(apperror.CodeVersionNotPublished,
				"playbook version %s is %s; only published versions can back an investigation",
				in.PlaybookVersionID, definition.Version.Status)
		}
		// A published version always has a valid root — publish validation
		// requires one — so reaching here means the schema's guarantees have
		// been violated, not that the caller did something wrong.
		return State{}, apperror.Internal(apperror.New(apperror.CodeInternal,
			"published playbook version %s has no usable root node", in.PlaybookVersionID))
	}

	// The domain decides whether the version may be used at all and whether a
	// terminal root means the investigation is born completed.
	inv, err := investigation.Start(alertRecord.ID, definition.Version, root, time.Now().UTC())
	if err != nil {
		return State{}, err
	}

	created, err := s.investigations.Create(ctx, inv)
	if err != nil {
		return State{}, err
	}

	s.log.InfoContext(ctx, "investigation started",
		slog.String("investigation_id", created.ID.String()),
		slog.String("alert_id", alertRecord.ID.String()),
		slog.String("playbook_version_id", definition.Version.ID.String()),
		slog.String("status", string(created.Status)))

	if created.IsCompleted() {
		s.logCompletion(ctx, created)
	}

	return State{
		Investigation:    created,
		Alert:            alertRecord,
		CurrentNode:      root,
		AvailableChoices: choicesFor(definition.Graph, created),
		Steps:            []investigation.Step{},
	}, nil
}

// Get returns the current state of an investigation.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (State, error) {
	inv, steps, err := s.investigations.GetWithSteps(ctx, id)
	if err != nil {
		return State{}, err
	}
	return s.buildState(ctx, inv, steps)
}

// SubmitDecision advances an investigation along the selected edge.
//
// This is a thin pass-through by design: the whole operation must be one
// transaction, and that transaction is owned by the repository (see
// docs/package-structure.md). The rule it applies — is this edge legal from
// here — is a pure domain function, unit-tested without a database.
func (s *Service) SubmitDecision(
	ctx context.Context,
	investigationID uuid.UUID,
	in investigation.DecisionInput,
) (State, error) {
	if !in.Actor.Type.Valid() {
		return State{}, apperror.Validation("actor.type must be %q or %q",
			investigation.ActorHuman, investigation.ActorAgent)
	}
	if in.EdgeID == uuid.Nil {
		return State{}, apperror.Validation("edge_id is required")
	}
	if err := investigation.ValidateEvidence(in.Evidence); err != nil {
		return State{}, err
	}

	result, err := s.investigations.ApplyDecision(ctx, investigationID, in)
	if err != nil {
		return State{}, err
	}

	if result.Replayed {
		// Worth logging: a stream of replays usually means an agent is timing
		// out and retrying, which is an operational signal even though each
		// individual replay is correct behaviour.
		s.log.InfoContext(ctx, "decision replayed from idempotency key",
			slog.String("investigation_id", investigationID.String()),
			slog.Int("sequence_number", result.Step.SequenceNumber))
	} else {
		// Rationale and evidence are deliberately not logged — they are the
		// audit record's content and can contain sensitive detail; they live
		// in the database, which is where an auditor reads them.
		s.log.InfoContext(ctx, "investigation decision applied",
			slog.String("investigation_id", investigationID.String()),
			slog.String("playbook_version_id", result.Investigation.PlaybookVersionID.String()),
			slog.Int("sequence_number", result.Step.SequenceNumber),
			slog.String("node_id", result.Step.NodeID.String()),
			slog.String("selected_edge_id", result.Step.SelectedEdgeID.String()),
			slog.String("actor_type", string(result.Step.Actor.Type)),
			slog.String("actor_id", actorID(result.Step.Actor)))

		if result.Investigation.IsCompleted() {
			s.logCompletion(ctx, result.Investigation)
		}
	}

	// Re-read rather than reporting result.Investigation directly: another
	// agent may have advanced the investigation further in the meantime, and
	// the caller is better served by a current, self-consistent snapshot than
	// by a view that was accurate only at commit time. This is also what the
	// idempotency contract promises for a replayed key.
	return s.Get(ctx, investigationID)
}

// Report returns the complete audit record.
func (s *Service) Report(ctx context.Context, id uuid.UUID) (Report, error) {
	// The path comes from the step records, never from current_node_id: the
	// steps are the authoritative history, the pointer is only a projection.
	// Both are read from one snapshot so a report can never show a resolution
	// its own path does not reach.
	inv, steps, err := s.investigations.GetWithSteps(ctx, id)
	if err != nil {
		return Report{}, err
	}

	alertRecord, err := s.alerts.Get(ctx, inv.AlertID)
	if err != nil {
		return Report{}, err
	}

	definition, err := s.playbooks.GetVersion(ctx, inv.PlaybookVersionID)
	if err != nil {
		return Report{}, err
	}

	return Report{
		Investigation: inv,
		Alert:         alertRecord,
		Definition:    definition,
		Steps:         steps,
	}, nil
}

// buildState assembles the current-state view shared by Get, Start's result
// after a decision, and SubmitDecision.
func (s *Service) buildState(
	ctx context.Context,
	inv investigation.Investigation,
	steps []investigation.Step,
) (State, error) {
	alertRecord, err := s.alerts.Get(ctx, inv.AlertID)
	if err != nil {
		return State{}, err
	}

	definition, err := s.playbooks.GetVersion(ctx, inv.PlaybookVersionID)
	if err != nil {
		return State{}, err
	}

	currentNode, ok := definition.Graph.NodeByID(inv.CurrentNodeID)
	if !ok {
		// The schema's composite foreign key makes this impossible; treating
		// it as an internal error rather than ignoring it means a schema
		// regression surfaces loudly instead of producing a half-empty
		// response.
		return State{}, apperror.Internal(apperror.New(apperror.CodeInternal,
			"investigation %s points at node %s, which is not in playbook version %s",
			inv.ID, inv.CurrentNodeID, inv.PlaybookVersionID))
	}

	return State{
		Investigation:    inv,
		Alert:            alertRecord,
		CurrentNode:      currentNode,
		AvailableChoices: choicesFor(definition.Graph, inv),
		Steps:            steps,
	}, nil
}

// choicesFor lists the edges selectable right now. A completed investigation
// offers none, even if its current node somehow had outgoing edges — the
// status, not the graph shape, decides whether anything can still be chosen.
func choicesFor(g playbook.Graph, inv investigation.Investigation) []playbook.Edge {
	if inv.IsCompleted() {
		return []playbook.Edge{}
	}
	choices := g.OutgoingEdges(inv.CurrentNodeID)
	if choices == nil {
		return []playbook.Edge{}
	}
	return choices
}

func (s *Service) logCompletion(ctx context.Context, inv investigation.Investigation) {
	s.log.InfoContext(ctx, "investigation completed",
		slog.String("investigation_id", inv.ID.String()),
		slog.String("alert_id", inv.AlertID.String()),
		slog.String("playbook_version_id", inv.PlaybookVersionID.String()),
		slog.String("final_resolution", derefString(inv.FinalResolution)))
}

func actorID(a investigation.Actor) string { return derefString(a.ID) }

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
