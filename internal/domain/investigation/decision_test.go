package investigation_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

var now = time.Date(2026, 8, 19, 10, 30, 0, 0, time.UTC)

func publishedVersion() playbook.Version {
	published := now.Add(-time.Hour)
	return playbook.Version{
		ID:          uuid.New(),
		PlaybookID:  uuid.New(),
		Version:     1,
		Status:      playbook.StatusPublished,
		PublishedAt: &published,
	}
}

func decisionNode() playbook.Node {
	return playbook.Node{ID: uuid.New(), Kind: playbook.KindDecision, Title: "Known workstation?"}
}

func terminalNode(resolution string) playbook.Node {
	return playbook.Node{
		ID:                 uuid.New(),
		Kind:               playbook.KindTerminal,
		Title:              "Outcome",
		TerminalResolution: &resolution,
	}
}

func edgeBetween(from, to playbook.Node, label string) playbook.Edge {
	return playbook.Edge{ID: uuid.New(), FromNodeID: from.ID, ToNodeID: to.ID, Label: label}
}

func TestStartBeginsInProgressAtRoot(t *testing.T) {
	version := publishedVersion()
	root := decisionNode()
	alertID := uuid.New()

	inv, err := investigation.Start(alertID, version, root, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != investigation.StatusInProgress {
		t.Fatalf("got status %q, want in_progress", inv.Status)
	}
	if inv.CurrentNodeID != root.ID {
		t.Fatalf("investigation should start at the root node")
	}
	if inv.CompletedAt != nil || inv.FinalResolution != nil {
		t.Fatalf("a new investigation must not be marked completed: %+v", inv)
	}
	if inv.AlertID != alertID || inv.PlaybookVersionID != version.ID {
		t.Fatalf("investigation must bind to the given alert and version")
	}
}

func TestStartRejectsUnpublishedVersion(t *testing.T) {
	for _, status := range []playbook.VersionStatus{playbook.StatusDraft, playbook.StatusArchived} {
		t.Run(string(status), func(t *testing.T) {
			version := publishedVersion()
			version.Status = status

			_, err := investigation.Start(uuid.New(), version, decisionNode(), now)
			if !apperror.IsCode(err, apperror.CodeVersionNotPublished) {
				t.Fatalf("got %v, want PLAYBOOK_VERSION_NOT_PUBLISHED", err)
			}
		})
	}
}

func TestStartWithTerminalRootCompletesImmediately(t *testing.T) {
	// Degenerate but valid playbook: the investigation is born completed and
	// records zero steps, rather than inventing a synthetic step with no edge.
	root := terminalNode("auto_close")

	inv, err := investigation.Start(uuid.New(), publishedVersion(), root, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != investigation.StatusCompleted {
		t.Fatalf("got status %q, want completed", inv.Status)
	}
	if inv.FinalResolution == nil || *inv.FinalResolution != "auto_close" {
		t.Fatalf("final resolution should be copied from the terminal root, got %v", inv.FinalResolution)
	}
	if inv.CompletedAt == nil || !inv.CompletedAt.Equal(now) {
		t.Fatalf("completed_at should be stamped, got %v", inv.CompletedAt)
	}
}

func TestDecideAdvancesToNextDecisionNode(t *testing.T) {
	current, next := decisionNode(), decisionNode()
	edge := edgeBetween(current, next, "Yes")
	inv := investigation.Investigation{
		ID:            uuid.New(),
		Status:        investigation.StatusInProgress,
		CurrentNodeID: current.ID,
	}

	got, err := investigation.Decide(inv, edge, next, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentNodeID != next.ID {
		t.Fatalf("current node should advance to the edge's target")
	}
	if got.Status != investigation.StatusInProgress {
		t.Fatalf("advancing to a decision node must not complete the investigation, got %q", got.Status)
	}
	if got.FinalResolution != nil || got.CompletedAt != nil {
		t.Fatalf("no resolution or completion timestamp should be set mid-investigation")
	}
}

func TestDecideCompletesOnTerminalNode(t *testing.T) {
	current := decisionNode()
	terminal := terminalNode("close_authorized_maintenance")
	edge := edgeBetween(current, terminal, "No")
	inv := investigation.Investigation{
		ID:            uuid.New(),
		Status:        investigation.StatusInProgress,
		CurrentNodeID: current.ID,
	}

	got, err := investigation.Decide(inv, edge, terminal, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != investigation.StatusCompleted {
		t.Fatalf("reaching a terminal node must complete the investigation, got %q", got.Status)
	}
	if got.CurrentNodeID != terminal.ID {
		t.Fatalf("current node should advance to the terminal node")
	}
	if got.FinalResolution == nil || *got.FinalResolution != "close_authorized_maintenance" {
		t.Fatalf("final resolution must be copied from the terminal node, got %v", got.FinalResolution)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(now) {
		t.Fatalf("completed_at must be stamped")
	}
}

func TestDecideRejectsEdgeNotLeavingCurrentNode(t *testing.T) {
	// The core safety property: a client cannot replay a stale choice or jump
	// ahead by submitting an edge from elsewhere in the graph.
	current, elsewhere, target := decisionNode(), decisionNode(), decisionNode()
	edge := edgeBetween(elsewhere, target, "Yes")
	inv := investigation.Investigation{
		ID:            uuid.New(),
		Status:        investigation.StatusInProgress,
		CurrentNodeID: current.ID,
	}

	_, err := investigation.Decide(inv, edge, target, now)
	if !apperror.IsCode(err, apperror.CodeInvalidTransition) {
		t.Fatalf("got %v, want INVALID_TRANSITION", err)
	}
}

func TestDecideRejectsAdvancingCompletedInvestigation(t *testing.T) {
	current, target := decisionNode(), decisionNode()
	edge := edgeBetween(current, target, "Yes")
	completedAt := now.Add(-time.Minute)
	resolution := "already_closed"
	inv := investigation.Investigation{
		ID:              uuid.New(),
		Status:          investigation.StatusCompleted,
		CurrentNodeID:   current.ID,
		CompletedAt:     &completedAt,
		FinalResolution: &resolution,
	}

	_, err := investigation.Decide(inv, edge, target, now)
	if !apperror.IsCode(err, apperror.CodeInvestigationCompleted) {
		t.Fatalf("got %v, want INVESTIGATION_ALREADY_COMPLETED", err)
	}
}

func TestDecideLeavesOriginalInvestigationUntouched(t *testing.T) {
	// Decide returns a new value rather than mutating: the caller decides when
	// (and whether) the transition becomes durable.
	current := decisionNode()
	terminal := terminalNode("escalate")
	edge := edgeBetween(current, terminal, "Yes")
	inv := investigation.Investigation{
		ID:            uuid.New(),
		Status:        investigation.StatusInProgress,
		CurrentNodeID: current.ID,
	}

	if _, err := investigation.Decide(inv, edge, terminal, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != investigation.StatusInProgress || inv.CurrentNodeID != current.ID {
		t.Fatalf("Decide must not mutate its input, got %+v", inv)
	}
}

func TestDecideRejectsMismatchedDestination(t *testing.T) {
	// Not reachable through the HTTP API — the destination always comes from
	// the edge — but a bug here would let a caller pick the next node, which is
	// exactly the property the engine must not lose.
	current, actual, wrong := decisionNode(), decisionNode(), decisionNode()
	edge := edgeBetween(current, actual, "Yes")
	inv := investigation.Investigation{
		ID:            uuid.New(),
		Status:        investigation.StatusInProgress,
		CurrentNodeID: current.ID,
	}

	_, err := investigation.Decide(inv, edge, wrong, now)
	if !apperror.IsCode(err, apperror.CodeInternal) {
		t.Fatalf("got %v, want INTERNAL_ERROR", err)
	}
}
