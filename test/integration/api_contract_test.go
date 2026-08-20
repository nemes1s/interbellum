package integration

import (
	"net/http"
	"testing"
)

// The tests in this file pin the API to the shapes api/openapi.yaml publishes.
// They exist because a JSON body that decodes without error is not the same
// thing as a body that matches the contract: before these, `"evidence": 123`
// was accepted, stored as JSONB, and handed back from an endpoint whose schema
// promises an array of objects.

// TestEvidenceMustMatchPublishedSchema rejects every shape the OpenAPI
// EvidenceItem schema does not permit.
func TestEvidenceMustMatchPublishedSchema(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)
	path := "/api/v1/investigations/" + investigationID + "/decisions"

	cases := []struct {
		name     string
		evidence any
		wantCode string
	}{
		{"scalar number", 123, "BAD_REQUEST"},
		{"scalar string", "some evidence", "BAD_REQUEST"},
		{"object instead of array", map[string]any{"hello": "world"}, "BAD_REQUEST"},
		{"array of scalars", []any{"anything"}, "BAD_REQUEST"},
		{"item missing type", []any{map[string]any{"summary": "no type"}}, "VALIDATION_FAILED"},
		{"item missing summary", []any{map[string]any{"type": "note"}}, "VALIDATION_FAILED"},
		{"item with blank type", []any{map[string]any{"type": "  ", "summary": "s"}}, "VALIDATION_FAILED"},
		{"data must be an object", []any{map[string]any{
			"type": "note", "summary": "s", "data": "not an object",
		}}, "BAD_REQUEST"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := c.post(path, map[string]any{
				"edge_id":  edgeKnownWorkstationYes,
				"actor":    agentActor(),
				"evidence": tc.evidence,
			})
			if resp.Status != http.StatusBadRequest {
				t.Fatalf("got status %d, want 400; body: %s", resp.Status, resp.Raw)
			}
			resp.expectCode(tc.wantCode)
		})
	}

	// None of the rejected requests advanced the investigation or wrote a step.
	state := c.get("/api/v1/investigations/" + investigationID).expectStatus(http.StatusOK)
	if len(state.arr("steps")) != 0 {
		t.Fatalf("a rejected decision must not append a step")
	}

	// A conforming request is accepted and round-trips unchanged.
	accepted := c.post(path, map[string]any{
		"edge_id": edgeKnownWorkstationYes,
		"actor":   agentActor(),
		"evidence": []any{map[string]any{
			"type":    "asset_inventory_lookup",
			"summary": "10.20.1.44 maps to ENG-WS-14",
			"data":    map[string]any{"asset": "ENG-WS-14", "trusted": true},
		}},
	}).expectStatus(http.StatusOK)

	step, _ := accepted.arr("steps")[0].(map[string]any)
	evidence, _ := step["evidence"].([]any)
	if len(evidence) != 1 {
		t.Fatalf("got %d evidence items, want 1", len(evidence))
	}
	item, _ := evidence[0].(map[string]any)
	if item["type"] != "asset_inventory_lookup" || item["summary"] != "10.20.1.44 maps to ENG-WS-14" {
		t.Fatalf("evidence did not round-trip: %v", item)
	}
	data, ok := item["data"].(map[string]any)
	if !ok || data["asset"] != "ENG-WS-14" || data["trusted"] != true {
		t.Fatalf("evidence data did not round-trip: %v", item["data"])
	}
}

// TestAlertPayloadMustBeAnObject: the Alert schema declares payload as an
// object, so an array or scalar must not be storable.
func TestAlertPayloadMustBeAnObject(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	for _, payload := range []any{[]any{"not", "an", "object"}, 42, "a string", true} {
		resp := c.post("/api/v1/alerts", map[string]any{
			"alert_type":  "shape_test",
			"title":       "Bad payload",
			"occurred_at": "2026-08-19T10:30:00Z",
			"payload":     payload,
		})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("payload %v: got status %d, want 400; body: %s", payload, resp.Status, resp.Raw)
		}
		resp.expectCode("BAD_REQUEST")
	}

	// An object payload is accepted and returned unchanged.
	created := c.post("/api/v1/alerts", map[string]any{
		"alert_type":  "shape_test",
		"title":       "Good payload",
		"occurred_at": "2026-08-19T10:30:00Z",
		"payload":     map[string]any{"plc": "PLC-17", "register": 40021},
	}).expectStatus(http.StatusCreated)

	payload, ok := created.Body["payload"].(map[string]any)
	if !ok || payload["plc"] != "PLC-17" {
		t.Fatalf("object payload did not round-trip: %v", created.Body["payload"])
	}
}

// TestNodeMetadataMustBeAnObject: the PlaybookNode schema declares metadata as
// a nullable object.
func TestNodeMetadataMustBeAnObject(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	const nodeID = "c9000000-0000-4000-8000-000000000001"

	for _, metadata := range []any{"i am a string", 7, []any{"a", "b"}} {
		resp := c.post("/api/v1/playbooks", map[string]any{
			"name":       "Bad metadata",
			"alert_type": "shape_test",
			"definition": map[string]any{
				"nodes": []any{map[string]any{
					"id": nodeID, "kind": "decision", "title": "Q", "metadata": metadata,
				}},
			},
		})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("metadata %v: got status %d, want 400; body: %s", metadata, resp.Status, resp.Raw)
		}
	}

	// An object, and an explicit null, are both accepted.
	created := c.post("/api/v1/playbooks", map[string]any{
		"name":       "Good metadata",
		"alert_type": "shape_test",
		"definition": map[string]any{
			"nodes": []any{
				map[string]any{"id": nodeID, "kind": "decision", "title": "Q",
					"metadata": map[string]any{"suggested_evidence": []any{"historian_query"}}},
				map[string]any{"id": "c9000000-0000-4000-8000-000000000002",
					"kind": "decision", "title": "R", "metadata": nil},
			},
		},
	}).expectStatus(http.StatusCreated)

	versionID, _ := created.arr("versions")[0].(map[string]any)["id"].(string)
	definition := c.get("/api/v1/playbook-versions/" + versionID).expectStatus(http.StatusOK)

	for _, raw := range definition.arr("nodes") {
		node, _ := raw.(map[string]any)
		if node["title"] != "Q" {
			continue
		}
		metadata, ok := node["metadata"].(map[string]any)
		if !ok || metadata["suggested_evidence"] == nil {
			t.Fatalf("object metadata did not round-trip: %v", node["metadata"])
		}
	}
}

// TestInvestigationRequiresMatchingAlertType pins the deliberate decision that
// a playbook only runs against alerts of the type it is registered for — see
// README "Key decisions". Without it, alert_type would be a label nothing
// enforces.
func TestInvestigationRequiresMatchingAlertType(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	// The PLC playbook, published.
	versionID := publishExamplePlaybook(t, c)

	// An alert of an entirely unrelated type.
	alertID := c.post("/api/v1/alerts", map[string]any{
		"alert_type":  "failed_login_burst",
		"title":       "Unrelated alert",
		"occurred_at": "2026-08-19T10:30:00Z",
	}).expectStatus(http.StatusCreated).str("id")

	c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusConflict).expectCode("ALERT_TYPE_MISMATCH")

	// The matching alert type still works.
	matching := createExampleAlert(t, c)
	c.post("/api/v1/alerts/"+matching+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusCreated)
}
