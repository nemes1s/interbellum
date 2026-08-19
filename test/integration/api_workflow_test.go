package integration

import (
	"net/http"
	"testing"
)

// TestFullInvestigationWorkflow walks the assignment's headline scenario end to
// end and then asserts that the report reproduces the exact path, rationale,
// evidence and resolution. This is the single test that would catch most
// regressions in the system.
func TestFullInvestigationWorkflow(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	// 1. Create and publish the example playbook.
	versionID := publishExamplePlaybook(t, c)

	// 2. Ingest the alert.
	alertID := createExampleAlert(t, c)

	// 3. Start the investigation.
	started := c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusCreated)

	investigationID := started.str("id")
	if got := started.str("status"); got != "in_progress" {
		t.Fatalf("got status %q, want in_progress", got)
	}
	if got := started.obj("current_node")["id"]; got != nodeKnownWorkstation {
		t.Fatalf("investigation should start at the playbook root, got %v", got)
	}
	if got := len(started.arr("available_choices")); got != 2 {
		t.Fatalf("root node should offer 2 choices, got %d", got)
	}

	// 4. Submit the decisions an analyst (or agent) would make:
	//    known workstation -> in maintenance window -> not a safety register.
	decision := func(edgeID, rationale string, evidence []any) *response {
		return c.post("/api/v1/investigations/"+investigationID+"/decisions", map[string]any{
			"edge_id":   edgeID,
			"actor":     agentActor(),
			"rationale": rationale,
			"evidence":  evidence,
		}).expectStatus(http.StatusOK)
	}

	afterFirst := decision(edgeKnownWorkstationYes,
		"The source address belongs to ENG-WS-14, a registered engineering workstation.",
		[]any{map[string]any{
			"type":    "asset_inventory_lookup",
			"summary": "10.20.1.44 maps to ENG-WS-14",
			"data":    map[string]any{"asset": "ENG-WS-14", "trusted": true},
		}})
	if got := afterFirst.obj("current_node")["id"]; got != nodeMaintWindow {
		t.Fatalf("after the first decision the investigation should be at the maintenance-window node, got %v", got)
	}
	if afterFirst.str("status") != "in_progress" {
		t.Fatalf("investigation completed too early")
	}

	afterSecond := decision(edgeMaintWindowYes,
		"Change calendar shows an approved window 10:00-12:00 covering PLC-17.",
		[]any{map[string]any{
			"type":    "change_calendar_lookup",
			"summary": "Approved maintenance window 10:00-12:00 UTC",
			"data":    map[string]any{"change_id": "CHG-4471"},
		}})
	if got := afterSecond.obj("current_node")["id"]; got != nodeSISRegister {
		t.Fatalf("expected the SIS-register node, got %v", got)
	}

	// 5. Reach the terminal node.
	completed := decision(edgeSISRegisterNo,
		"Register 40021 is a non-safety setpoint; the SIS range is 41000-41100.",
		[]any{map[string]any{
			"type":    "register_map_lookup",
			"summary": "40021 is outside the SIS register range",
			"data":    map[string]any{"sis_range": "41000-41100"},
		}})

	if got := completed.str("status"); got != "completed" {
		t.Fatalf("got status %q, want completed", got)
	}
	if got := completed.Body["final_resolution"]; got != "close_authorized_maintenance" {
		t.Fatalf("got final_resolution %v, want close_authorized_maintenance", got)
	}
	if got := len(completed.arr("available_choices")); got != 0 {
		t.Fatalf("a completed investigation must offer no choices, got %d", got)
	}
	if completed.Body["completed_at"] == nil {
		t.Fatalf("completed_at must be set on completion")
	}

	// 6. Request the report.
	report := c.get("/api/v1/investigations/" + investigationID + "/report").
		expectStatus(http.StatusOK)

	// The alert is reproduced in full, including its opaque payload.
	reportAlert := report.obj("alert")
	if reportAlert["id"] != alertID {
		t.Fatalf("report references the wrong alert")
	}
	payload, ok := reportAlert["payload"].(map[string]any)
	if !ok || payload["source_ip"] != "10.20.1.44" {
		t.Fatalf("alert payload should round-trip unchanged, got %v", reportAlert["payload"])
	}

	// The canonical graph is returned in full, separate from the path.
	version := report.obj("playbook_version")
	if version["id"] != versionID || version["status"] != "published" {
		t.Fatalf("report should carry the published version it ran against, got %v", version)
	}
	if nodes, _ := version["nodes"].([]any); len(nodes) != 11 {
		t.Fatalf("report should include the whole graph (11 nodes), got %d", len(nodes))
	}
	if edges, _ := version["edges"].([]any); len(edges) != 10 {
		t.Fatalf("report should include the whole graph (10 edges), got %d", len(edges))
	}

	// 7. Assert the exact path, in order, with rationale, evidence and actor.
	path := report.arr("path")
	if len(path) != 3 {
		t.Fatalf("got %d path steps, want 3", len(path))
	}

	wantPath := []struct {
		node, edge, evidenceType string
	}{
		{nodeKnownWorkstation, edgeKnownWorkstationYes, "asset_inventory_lookup"},
		{nodeMaintWindow, edgeMaintWindowYes, "change_calendar_lookup"},
		{nodeSISRegister, edgeSISRegisterNo, "register_map_lookup"},
	}

	for i, want := range wantPath {
		step, _ := path[i].(map[string]any)
		if got := step["step_number"]; got != float64(i+1) {
			t.Fatalf("path[%d].step_number = %v, want %d", i, got, i+1)
		}
		if step["node_id"] != want.node {
			t.Fatalf("path[%d].node_id = %v, want %s", i, step["node_id"], want.node)
		}
		if step["selected_edge_id"] != want.edge {
			t.Fatalf("path[%d].selected_edge_id = %v, want %s", i, step["selected_edge_id"], want.edge)
		}
		if step["rationale"] == nil || step["rationale"] == "" {
			t.Fatalf("path[%d] lost its rationale", i)
		}

		actor, _ := step["actor"].(map[string]any)
		if actor["type"] != "agent" || actor["id"] != "investigation-agent-v1" {
			t.Fatalf("path[%d] lost its actor information: %v", i, actor)
		}

		evidence, _ := step["evidence"].([]any)
		if len(evidence) != 1 {
			t.Fatalf("path[%d] should carry one evidence item, got %d", i, len(evidence))
		}
		item, _ := evidence[0].(map[string]any)
		if item["type"] != want.evidenceType {
			t.Fatalf("path[%d] evidence type = %v, want %s", i, item["type"], want.evidenceType)
		}
		if item["data"] == nil {
			t.Fatalf("path[%d] evidence lost its data payload", i)
		}
		if step["created_at"] == nil {
			t.Fatalf("path[%d] must carry a timestamp", i)
		}
	}

	// 8. The final resolution is on the report too.
	reportInvestigation := report.obj("investigation")
	if reportInvestigation["status"] != "completed" ||
		reportInvestigation["final_resolution"] != "close_authorized_maintenance" {
		t.Fatalf("report investigation summary is wrong: %v", reportInvestigation)
	}
}

// TestInvestigationStateIsSelfContainedForAnAgent checks the property that
// makes the API usable by an automated agent: one GET returns everything
// needed to choose the next action.
func TestInvestigationStateIsSelfContainedForAnAgent(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusCreated).str("id")

	state := c.get("/api/v1/investigations/" + investigationID).expectStatus(http.StatusOK)

	// The alert payload the agent must reason over, without a second request.
	alertPayload, ok := state.obj("alert")["payload"].(map[string]any)
	if !ok || alertPayload["plc"] != "PLC-17" {
		t.Fatalf("state should embed the full alert payload, got %v", state.obj("alert")["payload"])
	}

	// The question being asked, and the designer's hints for answering it.
	currentNode := state.obj("current_node")
	if currentNode["title"] == "" {
		t.Fatalf("current node must carry its question text")
	}
	metadata, ok := currentNode["metadata"].(map[string]any)
	if !ok || metadata["suggested_evidence"] == nil {
		t.Fatalf("designer metadata should reach the agent, got %v", currentNode["metadata"])
	}

	// The choices, each with the edge ID to submit.
	choices := state.arr("available_choices")
	if len(choices) != 2 {
		t.Fatalf("got %d choices, want 2", len(choices))
	}
	first, _ := choices[0].(map[string]any)
	if first["edge_id"] == nil || first["label"] == nil {
		t.Fatalf("a choice must expose the edge_id to submit and its label: %v", first)
	}
	// Choices arrive in the designer's declared order.
	second, _ := choices[1].(map[string]any)
	if first["label"] != "Yes" || second["label"] != "No" {
		t.Fatalf("choices should follow sort_order, got %v then %v", first["label"], second["label"])
	}

	if len(state.arr("steps")) != 0 {
		t.Fatalf("a fresh investigation has no history yet")
	}
}

// TestTerminalRootCompletesImmediately covers the degenerate playbook: a
// single terminal node. The investigation is born completed with no steps.
func TestTerminalRootCompletesImmediately(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	const rootID = "c0000000-0000-4000-8000-000000000001"
	created := c.post("/api/v1/playbooks", map[string]any{
		"name":       "Always close",
		"alert_type": "benign_noise",
		"definition": map[string]any{
			"root_node_id": rootID,
			"nodes": []any{map[string]any{
				"id":                  rootID,
				"kind":                "terminal",
				"title":               "Close: known-good noise",
				"terminal_resolution": "auto_close",
			}},
			"edges": []any{},
		},
	}).expectStatus(http.StatusCreated)

	versionID, _ := created.arr("versions")[0].(map[string]any)["id"].(string)
	c.post("/api/v1/playbook-versions/"+versionID+"/publish", nil).expectStatus(http.StatusOK)

	alertID := c.post("/api/v1/alerts", map[string]any{
		"alert_type":  "benign_noise",
		"title":       "Routine scan",
		"occurred_at": "2026-08-19T10:30:00Z",
	}).expectStatus(http.StatusCreated).str("id")

	started := c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusCreated)

	if started.str("status") != "completed" {
		t.Fatalf("a terminal root should complete the investigation immediately")
	}
	if started.Body["final_resolution"] != "auto_close" {
		t.Fatalf("got final_resolution %v, want auto_close", started.Body["final_resolution"])
	}
	if len(started.arr("steps")) != 0 {
		t.Fatalf("no synthetic step should be invented for a terminal root")
	}

	// And the report is coherent: a complete graph with an empty path.
	report := c.get("/api/v1/investigations/" + started.str("id") + "/report").
		expectStatus(http.StatusOK)
	if len(report.arr("path")) != 0 {
		t.Fatalf("path should be empty for a zero-step investigation")
	}
}
