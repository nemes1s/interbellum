package integration

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The tests in this file cover §28 of the assignment: the properties the
// backend must enforce regardless of what a client sends. Each one is a thing
// a buggy or malicious agent might attempt.

// TestClientCannotSubmitEdgeFromAnotherNode is the central safety property: the
// only edges that may be selected are those leaving the current node.
func TestClientCannotSubmitEdgeFromAnotherNode(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)

	// The investigation is at the root; this edge leaves a node three levels
	// down, so selecting it would skip the whole procedure.
	c.post("/api/v1/investigations/"+investigationID+"/decisions", map[string]any{
		"edge_id": edgeSISRegisterNo,
		"actor":   agentActor(),
	}).expectStatus(http.StatusConflict).expectCode("INVALID_TRANSITION")

	// Nothing was recorded.
	state := c.get("/api/v1/investigations/" + investigationID).expectStatus(http.StatusOK)
	if len(state.arr("steps")) != 0 {
		t.Fatalf("a rejected decision must not append a step")
	}
	if state.obj("current_node")["id"] != nodeKnownWorkstation {
		t.Fatalf("a rejected decision must not move the investigation")
	}
}

// TestClientCannotSubmitEdgeFromAnotherPlaybook stops an agent from borrowing
// an edge ID that belongs to a different playbook version entirely.
func TestClientCannotSubmitEdgeFromAnotherPlaybook(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)

	// A second, unrelated published playbook with its own root edge.
	const (
		otherRoot     = "d0000000-0000-4000-8000-000000000001"
		otherTerminal = "d0000000-0000-4000-8000-000000000002"
		otherEdge     = "d0000000-0000-4000-8000-000000000003"
	)
	other := c.post("/api/v1/playbooks", map[string]any{
		"name":       "Unrelated playbook",
		"alert_type": "other_type",
		"definition": map[string]any{
			"root_node_id": otherRoot,
			"nodes": []any{
				map[string]any{"id": otherRoot, "kind": "decision", "title": "Anything?"},
				map[string]any{
					"id": otherTerminal, "kind": "terminal", "title": "Done",
					"terminal_resolution": "done",
				},
			},
			"edges": []any{map[string]any{
				"id": otherEdge, "from_node_id": otherRoot, "to_node_id": otherTerminal, "label": "Yes",
			}},
		},
	}).expectStatus(http.StatusCreated)
	otherVersionID, _ := other.arr("versions")[0].(map[string]any)["id"].(string)
	c.post("/api/v1/playbook-versions/"+otherVersionID+"/publish", nil).expectStatus(http.StatusOK)

	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)

	c.post("/api/v1/investigations/"+investigationID+"/decisions", map[string]any{
		"edge_id": otherEdge,
		"actor":   agentActor(),
	}).expectStatus(http.StatusConflict).expectCode("INVALID_TRANSITION")
}

// TestClientCannotAdvanceCompletedInvestigation covers the "rewrite history"
// attempt: once an investigation is closed it stays closed.
func TestClientCannotAdvanceCompletedInvestigation(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)

	// Drive it straight to a terminal node in two decisions.
	submitDecision(t, c, investigationID, edgeKnownWorkstationYes)
	completed := submitDecision(t, c, investigationID, edgeMaintWindowNo)
	if completed.str("status") != "completed" {
		t.Fatalf("expected the investigation to be completed")
	}

	// Any further decision is refused, whichever edge is offered.
	c.post("/api/v1/investigations/"+investigationID+"/decisions", map[string]any{
		"edge_id": edgeKnownWorkstationNo,
		"actor":   agentActor(),
	}).expectStatus(http.StatusConflict).expectCode("INVESTIGATION_ALREADY_COMPLETED")

	// The audit trail is unchanged.
	report := c.get("/api/v1/investigations/" + investigationID + "/report").
		expectStatus(http.StatusOK)
	if len(report.arr("path")) != 2 {
		t.Fatalf("the completed path must remain exactly 2 steps")
	}
}

// TestPublishedPlaybookCannotBeModified protects the property everything else
// depends on: a historical investigation's definition never changes.
func TestPublishedPlaybookCannotBeModified(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)

	c.put("/api/v1/playbook-versions/"+versionID, map[string]any{
		"root_node_id": nil,
		"nodes":        []any{},
		"edges":        []any{},
	}).expectStatus(http.StatusConflict).expectCode("PLAYBOOK_VERSION_NOT_DRAFT")

	// The graph is untouched.
	definition := c.get("/api/v1/playbook-versions/" + versionID).expectStatus(http.StatusOK)
	if len(definition.arr("nodes")) != 11 {
		t.Fatalf("the published graph must be unchanged, got %d nodes", len(definition.arr("nodes")))
	}

	// Publishing again is also refused.
	c.post("/api/v1/playbook-versions/"+versionID+"/publish", nil).
		expectStatus(http.StatusConflict).expectCode("PLAYBOOK_VERSION_NOT_DRAFT")
}

// TestInvestigationCannotStartAgainstDraftVersion enforces that only reviewed,
// published procedures produce auditable investigations.
func TestInvestigationCannotStartAgainstDraftVersion(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	created := c.post("/api/v1/playbooks", loadFixture(t, "example-playbook.json")).
		expectStatus(http.StatusCreated)
	draftVersionID, _ := created.arr("versions")[0].(map[string]any)["id"].(string)

	alertID := createExampleAlert(t, c)

	c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": draftVersionID,
	}).expectStatus(http.StatusConflict).expectCode("PLAYBOOK_VERSION_NOT_PUBLISHED")
}

// TestHistoricalInvestigationSurvivesPlaybookRevision is the auditability
// guarantee stated in the README: publishing a revised version must not change
// what an earlier investigation means.
func TestHistoricalInvestigationSurvivesPlaybookRevision(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	v1 := publishExamplePlaybook(t, c)
	playbookID := c.get("/api/v1/playbook-versions/" + v1).str("playbook_id")

	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, v1)
	submitDecision(t, c, investigationID, edgeKnownWorkstationYes)

	before := c.get("/api/v1/investigations/" + investigationID + "/report").
		expectStatus(http.StatusOK)

	// The designer revises the playbook: a new version cloned from v1, edited
	// down to a single terminal node, and published.
	v2Response := c.post("/api/v1/playbooks/"+playbookID+"/versions", map[string]any{
		"clone_from_version_id": v1,
	}).expectStatus(http.StatusCreated)
	v2 := v2Response.str("id")

	const newRoot = "f0000000-0000-4000-8000-000000000001"
	c.put("/api/v1/playbook-versions/"+v2, map[string]any{
		"root_node_id": newRoot,
		"nodes": []any{map[string]any{
			"id": newRoot, "kind": "terminal", "title": "Close everything",
			"terminal_resolution": "close_all",
		}},
		"edges": []any{},
	}).expectStatus(http.StatusOK)
	c.post("/api/v1/playbook-versions/"+v2+"/publish", nil).expectStatus(http.StatusOK)

	// The in-flight investigation is unaffected: same version, same graph,
	// same path, and it can still be advanced along the original procedure.
	after := c.get("/api/v1/investigations/" + investigationID + "/report").
		expectStatus(http.StatusOK)

	if after.obj("playbook_version")["id"] != v1 {
		t.Fatalf("the investigation must stay bound to the version it started on")
	}
	if len(after.obj("playbook_version")["nodes"].([]any)) != 11 {
		t.Fatalf("the original graph must still be retrievable in full")
	}
	if len(after.arr("path")) != len(before.arr("path")) {
		t.Fatalf("the recorded path must not change when the playbook is revised")
	}

	// And the original procedure still works.
	next := submitDecision(t, c, investigationID, edgeMaintWindowYes)
	if next.obj("current_node")["id"] != nodeSISRegister {
		t.Fatalf("the investigation must keep following the version it started on")
	}
}

// TestCloneCreatesIndependentGraph verifies that editing a cloned draft cannot
// reach back and mutate the published version it was copied from.
func TestCloneCreatesIndependentGraph(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	v1 := publishExamplePlaybook(t, c)
	playbookID := c.get("/api/v1/playbook-versions/" + v1).str("playbook_id")

	v2 := c.post("/api/v1/playbooks/"+playbookID+"/versions", map[string]any{
		"clone_from_version_id": v1,
	}).expectStatus(http.StatusCreated)

	// Same shape...
	if len(v2.arr("nodes")) != 11 || len(v2.arr("edges")) != 10 {
		t.Fatalf("clone should reproduce the graph shape, got %d nodes / %d edges",
			len(v2.arr("nodes")), len(v2.arr("edges")))
	}
	// ...but no shared rows: fresh node IDs mean an edit to the draft cannot
	// touch the published version's graph.
	for _, node := range v2.arr("nodes") {
		if node.(map[string]any)["id"] == nodeKnownWorkstation {
			t.Fatalf("cloned nodes must get fresh IDs, not reuse the source version's")
		}
	}
	if v2.Body["root_node_id"] == nil {
		t.Fatalf("clone should carry over a root, remapped to the new node IDs")
	}
}

// TestDecisionValidationRejectsBadRequests covers the input-validation surface
// of the decision endpoint.
func TestDecisionValidationRejectsBadRequests(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)
	base := "/api/v1/investigations/" + investigationID + "/decisions"

	t.Run("unknown edge", func(t *testing.T) {
		c.post(base, map[string]any{"edge_id": uuid.NewString(), "actor": agentActor()}).
			expectStatus(http.StatusConflict).expectCode("INVALID_TRANSITION")
	})

	t.Run("invalid actor type", func(t *testing.T) {
		c.post(base, map[string]any{
			"edge_id": edgeKnownWorkstationYes,
			"actor":   map[string]any{"type": "robot"},
		}).expectStatus(http.StatusBadRequest).expectCode("VALIDATION_FAILED")
	})

	t.Run("unknown field is rejected rather than silently dropped", func(t *testing.T) {
		c.post(base, map[string]any{
			"edge_id":   edgeKnownWorkstationYes,
			"actor":     agentActor(),
			"rationaal": "typo that would otherwise lose the rationale",
		}).expectStatus(http.StatusBadRequest).expectCode("BAD_REQUEST")
	})

	t.Run("malformed investigation id", func(t *testing.T) {
		c.post("/api/v1/investigations/not-a-uuid/decisions", map[string]any{
			"edge_id": edgeKnownWorkstationYes, "actor": agentActor(),
		}).expectStatus(http.StatusBadRequest).expectCode("BAD_REQUEST")
	})

	t.Run("unknown investigation", func(t *testing.T) {
		c.post("/api/v1/investigations/"+uuid.NewString()+"/decisions", map[string]any{
			"edge_id": edgeKnownWorkstationYes, "actor": agentActor(),
		}).expectStatus(http.StatusNotFound).expectCode("RESOURCE_NOT_FOUND")
	})
}

// TestNoRouteMutatesInvestigationHistory documents the append-only guarantee at
// the routing level: there is simply no verb that edits or deletes a step.
func TestNoRouteMutatesInvestigationHistory(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)
	submitDecision(t, c, investigationID, edgeKnownWorkstationYes)

	for _, target := range []string{
		"/api/v1/investigations/" + investigationID + "/decisions",
		"/api/v1/investigations/" + investigationID + "/steps",
		"/api/v1/investigations/" + investigationID + "/steps/1",
	} {
		for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
			resp := c.do(method, target, nil, nil)
			if resp.Status != http.StatusNotFound && resp.Status != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s returned %d; no route may mutate recorded history",
					method, target, resp.Status)
			}
		}
	}

	// The one step recorded is still there and unchanged.
	if len(c.get("/api/v1/investigations/"+investigationID+"/report").arr("path")) != 1 {
		t.Fatalf("history should still contain exactly one step")
	}
}

// startInvestigation is the shared three-line setup used across safety tests.
func startInvestigation(t *testing.T, c *client, alertID, versionID string) string {
	t.Helper()
	return c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusCreated).str("id")
}

func submitDecision(t *testing.T, c *client, investigationID, edgeID string) *response {
	t.Helper()
	return c.post("/api/v1/investigations/"+investigationID+"/decisions", map[string]any{
		"edge_id":   edgeID,
		"actor":     agentActor(),
		"rationale": "test decision",
	}).expectStatus(http.StatusOK)
}
