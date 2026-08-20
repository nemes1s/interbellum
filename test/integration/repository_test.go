package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/alert"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// These tests drive the repositories directly. They exist to cover the things
// the database itself enforces — constraints, ordering, transaction
// boundaries — which a mock would silently accept.

func ctx() context.Context { return context.Background() }

// simplePlaybook builds a two-node graph: one decision leading to one terminal.
func simplePlaybook(name string) (playbook.NewPlaybook, uuid.UUID, uuid.UUID, uuid.UUID) {
	root := uuid.New()
	terminal := uuid.New()
	edge := uuid.New()
	resolution := "closed"

	return playbook.NewPlaybook{
		Name:      name,
		AlertType: "test_type",
		Graph: playbook.Graph{
			RootNodeID: &root,
			Nodes: []playbook.Node{
				{ID: root, Kind: playbook.KindDecision, Title: "Proceed?"},
				{ID: terminal, Kind: playbook.KindTerminal, Title: "Done", TerminalResolution: &resolution},
			},
			Edges: []playbook.Edge{
				{ID: edge, FromNodeID: root, ToNodeID: terminal, Label: "Yes"},
			},
		},
	}, root, terminal, edge
}

func TestPlaybookPersistenceRoundTrip(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	in, root, terminal, edge := simplePlaybook("Round trip")
	pb, def, err := repos.playbooks.Create(ctx(), in)
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	if pb.ID == uuid.Nil || def.Version.Version != 1 || def.Version.Status != playbook.StatusDraft {
		t.Fatalf("a new playbook should get version 1 in draft, got %+v", def.Version)
	}

	loaded, err := repos.playbooks.GetVersion(ctx(), def.Version.ID)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if loaded.Graph.RootNodeID == nil || *loaded.Graph.RootNodeID != root {
		t.Fatalf("root node did not round-trip")
	}
	if len(loaded.Graph.Nodes) != 2 || len(loaded.Graph.Edges) != 1 {
		t.Fatalf("graph did not round-trip: %d nodes, %d edges",
			len(loaded.Graph.Nodes), len(loaded.Graph.Edges))
	}
	if _, ok := loaded.Graph.NodeByID(terminal); !ok {
		t.Fatalf("terminal node missing after round trip")
	}
	if _, ok := loaded.Graph.EdgeByID(edge); !ok {
		t.Fatalf("edge missing after round trip")
	}
}

func TestPublishValidatesGraphAndFreezesVersion(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	// A deliberately broken graph: a decision node with no outgoing edges.
	root := uuid.New()
	_, def, err := repos.playbooks.Create(ctx(), playbook.NewPlaybook{
		Name:      "Broken",
		AlertType: "test_type",
		Graph: playbook.Graph{
			RootNodeID: &root,
			Nodes:      []playbook.Node{{ID: root, Kind: playbook.KindDecision, Title: "Dead end?"}},
		},
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	_, err = repos.playbooks.Publish(ctx(), def.Version.ID)
	if !apperror.IsCode(err, apperror.CodeInvalidPlaybookGraph) {
		t.Fatalf("got %v, want INVALID_PLAYBOOK_GRAPH", err)
	}

	// A failed publish must leave the version editable.
	still, err := repos.playbooks.GetVersion(ctx(), def.Version.ID)
	if err != nil || still.Version.Status != playbook.StatusDraft {
		t.Fatalf("a rejected publish must leave the version in draft, got %v (%v)",
			still.Version.Status, err)
	}

	// Fix the graph and publish for real.
	fixed, fixedRoot, _, _ := simplePlaybook("ignored")
	if _, err := repos.playbooks.ReplaceGraph(ctx(), def.Version.ID, fixed.Graph); err != nil {
		t.Fatalf("replace graph: %v", err)
	}
	published, err := repos.playbooks.Publish(ctx(), def.Version.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Version.Status != playbook.StatusPublished || published.Version.PublishedAt == nil {
		t.Fatalf("publish should set status and published_at, got %+v", published.Version)
	}
	if published.Graph.RootNodeID == nil || *published.Graph.RootNodeID != fixedRoot {
		t.Fatalf("published graph should be the replacement graph")
	}

	// Now frozen.
	if _, err := repos.playbooks.ReplaceGraph(ctx(), def.Version.ID, fixed.Graph); !apperror.IsCode(err, apperror.CodeVersionNotDraft) {
		t.Fatalf("got %v, want PLAYBOOK_VERSION_NOT_DRAFT", err)
	}
}

func TestReplaceGraphIsAWholesaleSwap(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	in, _, _, _ := simplePlaybook("Replace me")
	_, def, err := repos.playbooks.Create(ctx(), in)
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Replacing with an empty graph must clear the old nodes/edges and release
	// the root pointer — the FK ordering this exercises is the reason
	// ReplaceGraph is a single transactional operation.
	replaced, err := repos.playbooks.ReplaceGraph(ctx(), def.Version.ID, playbook.Graph{})
	if err != nil {
		t.Fatalf("replace with empty graph: %v", err)
	}
	if len(replaced.Graph.Nodes) != 0 || len(replaced.Graph.Edges) != 0 {
		t.Fatalf("old graph was not cleared: %+v", replaced.Graph)
	}
	if replaced.Graph.RootNodeID != nil {
		t.Fatalf("root pointer should be released, got %v", replaced.Graph.RootNodeID)
	}
}

func TestAlertIngestionIsIdempotentOnExternalID(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	externalID := "INTERBELLUM-TEST-0001"
	first, created, err := repos.alerts.Create(ctx(), alert.New{
		ExternalID: &externalID,
		AlertType:  "test_type",
		Title:      "Original title",
		Payload:    []byte(`{"a":1}`),
		OccurredAt: time.Now().UTC(),
	})
	if err != nil || !created {
		t.Fatalf("first ingestion should create: created=%v err=%v", created, err)
	}

	// A retry with drifted content returns the original: first write wins, so
	// an investigation already referencing this alert never sees it change.
	second, created, err := repos.alerts.Create(ctx(), alert.New{
		ExternalID: &externalID,
		AlertType:  "test_type",
		Title:      "Different title",
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if created {
		t.Fatalf("a retry must not create a second alert")
	}
	if second.ID != first.ID || second.Title != "Original title" {
		t.Fatalf("retry should return the originally stored alert, got %+v", second)
	}

	// Without an external_id, every request is a distinct alert.
	a, created, err := repos.alerts.Create(ctx(), alert.New{
		AlertType: "test_type", Title: "Anonymous", OccurredAt: time.Now().UTC(),
	})
	if err != nil || !created || a.ID == first.ID {
		t.Fatalf("an alert without external_id should always be created fresh")
	}
}

func TestInvestigationStepsAreOrderedAndAppendOnly(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	versionID, root, _, edgeID := publishSimplePlaybook(t, repos)
	alertID := createAlert(t, repos)

	inv, err := repos.investigations.Create(ctx(), investigation.Investigation{
		AlertID:           alertID,
		PlaybookVersionID: versionID,
		Status:            investigation.StatusInProgress,
		CurrentNodeID:     root,
		StartedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	rationale := "looks fine"
	result, err := repos.investigations.ApplyDecision(ctx(), inv.ID, investigation.DecisionInput{
		EdgeID:    edgeID,
		Actor:     investigation.Actor{Type: investigation.ActorAgent},
		Rationale: &rationale,
		Evidence: []investigation.EvidenceItem{
			{Type: "note", Summary: "ok", Data: []byte(`{"checked":true}`)},
		},
	})
	if err != nil {
		t.Fatalf("apply decision: %v", err)
	}
	if result.Step.SequenceNumber != 1 {
		t.Fatalf("first step should be sequence 1, got %d", result.Step.SequenceNumber)
	}
	if !result.Investigation.IsCompleted() {
		t.Fatalf("reaching the terminal node should complete the investigation")
	}
	if result.Investigation.FinalResolution == nil || *result.Investigation.FinalResolution != "closed" {
		t.Fatalf("final resolution not copied from the terminal node")
	}

	_, steps, err := repos.investigations.GetWithSteps(ctx(), inv.ID)
	if err != nil {
		t.Fatalf("load steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	if steps[0].Rationale == nil || *steps[0].Rationale != rationale {
		t.Fatalf("rationale did not persist")
	}
	if len(steps[0].Evidence) != 1 {
		t.Fatalf("evidence did not persist: %+v", steps[0].Evidence)
	}
	if steps[0].Evidence[0].Type != "note" || steps[0].Evidence[0].Summary != "ok" {
		t.Fatalf("evidence fields did not round-trip: %+v", steps[0].Evidence[0])
	}
	if string(steps[0].Evidence[0].Data) != `{"checked": true}` {
		t.Fatalf("evidence data did not round-trip: %s", steps[0].Evidence[0].Data)
	}
	// The step records the node it was made *from*, not the destination —
	// that is what makes the path reconstructable.
	if steps[0].NodeID != root {
		t.Fatalf("step should record the originating node")
	}
}

// TestStepCannotReferenceForeignPlaybookVersion checks the composite foreign
// keys: even a service-layer bug cannot record a step against a node or edge
// from a different playbook version.
func TestStepCannotReferenceForeignPlaybookVersion(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	versionA, rootA, _, _ := publishSimplePlaybook(t, repos)
	_, _, _, edgeB := publishSimplePlaybook(t, repos)
	alertID := createAlert(t, repos)

	inv, err := repos.investigations.Create(ctx(), investigation.Investigation{
		AlertID:           alertID,
		PlaybookVersionID: versionA,
		Status:            investigation.StatusInProgress,
		CurrentNodeID:     rootA,
		StartedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	// edgeB belongs to a different version; the scoped lookup rejects it.
	_, err = repos.investigations.ApplyDecision(ctx(), inv.ID, investigation.DecisionInput{
		EdgeID: edgeB,
		Actor:  investigation.Actor{Type: investigation.ActorAgent},
	})
	if !apperror.IsCode(err, apperror.CodeInvalidTransition) {
		t.Fatalf("got %v, want INVALID_TRANSITION", err)
	}

	// Bypassing the repository entirely, the database still refuses.
	_, err = pool.Exec(ctx(), `
		INSERT INTO investigation_steps
			(investigation_id, playbook_version_id, sequence_number, node_id, selected_edge_id, actor_type)
		VALUES ($1, $2, 1, $3, $4, 'agent')`,
		inv.ID, versionA, rootA, edgeB)
	if err == nil {
		t.Fatalf("the database must reject a step whose edge belongs to another version")
	}
}

// TestInvestigationCannotOutliveItsVersionBinding checks the other composite
// FK: a step's playbook_version_id must equal its investigation's.
func TestStepVersionMustMatchInvestigationVersion(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	versionA, rootA, _, edgeA := publishSimplePlaybook(t, repos)
	versionB, _, _, _ := publishSimplePlaybook(t, repos)
	alertID := createAlert(t, repos)

	inv, err := repos.investigations.Create(ctx(), investigation.Investigation{
		AlertID:           alertID,
		PlaybookVersionID: versionA,
		Status:            investigation.StatusInProgress,
		CurrentNodeID:     rootA,
		StartedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	_, err = pool.Exec(ctx(), `
		INSERT INTO investigation_steps
			(investigation_id, playbook_version_id, sequence_number, node_id, selected_edge_id, actor_type)
		VALUES ($1, $2, 1, $3, $4, 'agent')`,
		inv.ID, versionB, rootA, edgeA)
	if err == nil {
		t.Fatalf("a step claiming a different playbook version than its investigation must be rejected")
	}
}

// TestDuplicateEdgeLabelsRejectedAtWriteTime documents why publish-time
// validation has no "duplicate label" reason: the constraint catches it first.
func TestDuplicateEdgeLabelsRejectedAtWriteTime(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	root, a, b := uuid.New(), uuid.New(), uuid.New()
	resolution := "closed"

	_, _, err := repos.playbooks.Create(ctx(), playbook.NewPlaybook{
		Name:      "Ambiguous choices",
		AlertType: "test_type",
		Graph: playbook.Graph{
			RootNodeID: &root,
			Nodes: []playbook.Node{
				{ID: root, Kind: playbook.KindDecision, Title: "Which way?"},
				{ID: a, Kind: playbook.KindTerminal, Title: "A", TerminalResolution: &resolution},
				{ID: b, Kind: playbook.KindTerminal, Title: "B", TerminalResolution: &resolution},
			},
			Edges: []playbook.Edge{
				{ID: uuid.New(), FromNodeID: root, ToNodeID: a, Label: "Yes"},
				{ID: uuid.New(), FromNodeID: root, ToNodeID: b, Label: "Yes"},
			},
		},
	})
	if !apperror.IsCode(err, apperror.CodeValidationFailed) {
		t.Fatalf("got %v, want VALIDATION_FAILED for duplicate edge labels", err)
	}

	// And nothing partial was left behind — the whole create was one transaction.
	playbooks, listErr := repos.playbooks.List(ctx(), "test_type")
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(playbooks) != 0 {
		t.Fatalf("a failed create must not leave a playbook behind, got %d", len(playbooks))
	}
}

func TestGetMissingResourcesReturnsNotFound(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	_, _, err := repos.playbooks.Get(ctx(), uuid.New())
	if !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("playbook: got %v, want RESOURCE_NOT_FOUND", err)
	}
	if _, err := repos.playbooks.GetVersion(ctx(), uuid.New()); !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("version: got %v, want RESOURCE_NOT_FOUND", err)
	}
	if _, err := repos.alerts.Get(ctx(), uuid.New()); !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("alert: got %v, want RESOURCE_NOT_FOUND", err)
	}
	if _, _, err := repos.investigations.GetWithSteps(ctx(), uuid.New()); !apperror.IsCode(err, apperror.CodeNotFound) {
		t.Fatalf("investigation: got %v, want RESOURCE_NOT_FOUND", err)
	}

	// Errors must be application errors, never raw driver errors.
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("repository errors must be application errors, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// Shared setup
// ---------------------------------------------------------------------------

func publishSimplePlaybook(t *testing.T, repos repositories) (versionID, root, terminal, edge uuid.UUID) {
	t.Helper()

	in, root, terminal, edge := simplePlaybook("Simple " + uuid.NewString())
	_, def, err := repos.playbooks.Create(ctx(), in)
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}
	if _, err := repos.playbooks.Publish(ctx(), def.Version.ID); err != nil {
		t.Fatalf("publish playbook: %v", err)
	}
	return def.Version.ID, root, terminal, edge
}

func createAlert(t *testing.T, repos repositories) uuid.UUID {
	t.Helper()

	a, _, err := repos.alerts.Create(ctx(), alert.New{
		AlertType:  "test_type",
		Title:      "Test alert",
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}
	return a.ID
}
