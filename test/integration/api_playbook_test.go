package integration

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestEmptyDraftIsStorableButNotPublishable covers the deliberate split
// between what a draft may be (incomplete — designers save work in progress)
// and what a published version must be (complete and valid).
func TestEmptyDraftIsStorableButNotPublishable(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	created := c.post("/api/v1/playbooks", map[string]any{
		"name":       "Work in progress",
		"alert_type": "wip_type",
	}).expectStatus(http.StatusCreated)

	versionID, _ := created.arr("versions")[0].(map[string]any)["id"].(string)

	// An empty draft round-trips fine.
	definition := c.get("/api/v1/playbook-versions/" + versionID).expectStatus(http.StatusOK)
	if definition.Body["root_node_id"] != nil {
		t.Fatalf("an empty draft should have no root")
	}
	if len(definition.arr("nodes")) != 0 {
		t.Fatalf("an empty draft should have no nodes")
	}

	// Saving a partial graph — a decision node with no choices yet — is also
	// allowed; publish is the gate, not the editor.
	const partial = "c1000000-0000-4000-8000-000000000001"
	c.put("/api/v1/playbook-versions/"+versionID, map[string]any{
		"root_node_id": partial,
		"nodes": []any{map[string]any{
			"id": partial, "kind": "decision", "title": "Not finished yet",
		}},
		"edges": []any{},
	}).expectStatus(http.StatusOK)

	// But publishing it is rejected, with the specific problem named.
	failed := c.post("/api/v1/playbook-versions/"+versionID+"/publish", nil).
		expectStatus(http.StatusUnprocessableEntity).
		expectCode("INVALID_PLAYBOOK_GRAPH")

	details := failed.arr("details")
	if len(details) != 1 {
		t.Fatalf("got %d validation issues, want 1: %s", len(details), failed.Raw)
	}
	if reason, _ := details[0].(map[string]any)["reason"].(string); reason != "decision_node_without_edges" {
		t.Fatalf("got reason %q, want decision_node_without_edges", reason)
	}

	// Starting an investigation against the unpublished draft names the real
	// problem — that it is not published — rather than complaining about the
	// graph.
	alertID := c.post("/api/v1/alerts", map[string]any{
		"alert_type": "wip_type", "title": "Test", "occurred_at": "2026-08-19T10:30:00Z",
	}).expectStatus(http.StatusCreated).str("id")

	c.post("/api/v1/alerts/"+alertID+"/investigations", map[string]any{
		"playbook_version_id": versionID,
	}).expectStatus(http.StatusConflict).expectCode("PLAYBOOK_VERSION_NOT_PUBLISHED")
}

// TestPublishReportsEveryProblemAtOnce: a designer fixing a broken graph should
// need one publish attempt, not one per mistake.
func TestPublishReportsEveryProblemAtOnce(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	const (
		a      = "c2000000-0000-4000-8000-000000000001"
		b      = "c2000000-0000-4000-8000-000000000002"
		orphan = "c2000000-0000-4000-8000-000000000003"
	)

	created := c.post("/api/v1/playbooks", map[string]any{
		"name":       "Multiple problems",
		"alert_type": "broken_type",
		"definition": map[string]any{
			"root_node_id": a,
			"nodes": []any{
				map[string]any{"id": a, "kind": "decision", "title": "Loop?"},
				map[string]any{"id": b, "kind": "decision", "title": "Back?"},
				map[string]any{"id": orphan, "kind": "terminal", "title": "Unused",
					"terminal_resolution": "never"},
			},
			"edges": []any{
				map[string]any{"id": uuid.NewString(), "from_node_id": a, "to_node_id": b, "label": "Yes"},
				map[string]any{"id": uuid.NewString(), "from_node_id": b, "to_node_id": a, "label": "Back"},
			},
		},
	}).expectStatus(http.StatusCreated)

	versionID, _ := created.arr("versions")[0].(map[string]any)["id"].(string)

	failed := c.post("/api/v1/playbook-versions/"+versionID+"/publish", nil).
		expectStatus(http.StatusUnprocessableEntity).
		expectCode("INVALID_PLAYBOOK_GRAPH")

	reasons := map[string]int{}
	for _, detail := range failed.arr("details") {
		reason, _ := detail.(map[string]any)["reason"].(string)
		reasons[reason]++
	}

	// Both nodes on the cycle, plus the disconnected node.
	if reasons["cycle_detected"] != 2 {
		t.Fatalf("expected both cycle nodes to be reported, got %v", reasons)
	}
	if reasons["unreachable_from_root"] != 1 {
		t.Fatalf("expected the orphan node to be reported, got %v", reasons)
	}
}

// TestGraphInputValidationRejectsIncoherentDrafts: a draft may be incomplete,
// but it may not be nonsense — an unknown node kind or an edge with no label
// would corrupt the stored graph rather than leave it unfinished.
func TestGraphInputValidationRejectsIncoherentDrafts(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	nodeID := uuid.NewString()

	cases := []struct {
		name  string
		graph map[string]any
	}{
		{"unknown node kind", map[string]any{
			"nodes": []any{map[string]any{"id": nodeID, "kind": "maybe", "title": "?"}},
		}},
		{"node without title", map[string]any{
			"nodes": []any{map[string]any{"id": nodeID, "kind": "decision", "title": ""}},
		}},
		{"duplicate node id", map[string]any{
			"nodes": []any{
				map[string]any{"id": nodeID, "kind": "decision", "title": "One"},
				map[string]any{"id": nodeID, "kind": "decision", "title": "Two"},
			},
		}},
		{"edge without label", map[string]any{
			"nodes": []any{map[string]any{"id": nodeID, "kind": "decision", "title": "Q"}},
			"edges": []any{map[string]any{
				"id": uuid.NewString(), "from_node_id": nodeID, "to_node_id": nodeID, "label": "",
			}},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"name":       "Incoherent",
				"alert_type": "bad_type",
				"definition": tc.graph,
			}
			c.post("/api/v1/playbooks", body).
				expectStatus(http.StatusBadRequest).expectCode("VALIDATION_FAILED")
		})
	}
}

// TestPlaybookListingAndRetrieval covers the read side of playbook design.
func TestPlaybookListingAndRetrieval(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	playbookID := c.get("/api/v1/playbook-versions/" + versionID).str("playbook_id")

	c.post("/api/v1/playbooks", map[string]any{
		"name": "Unrelated", "alert_type": "some_other_type",
	}).expectStatus(http.StatusCreated)

	all := c.get("/api/v1/playbooks").expectStatus(http.StatusOK)
	if len(all.arr("items")) != 2 {
		t.Fatalf("got %d playbooks, want 2", len(all.arr("items")))
	}

	filtered := c.get("/api/v1/playbooks?alert_type=unauthorized_plc_register_write").
		expectStatus(http.StatusOK)
	if len(filtered.arr("items")) != 1 {
		t.Fatalf("filtering by alert_type should return 1, got %d", len(filtered.arr("items")))
	}

	// A playbook detail response carries its version summaries.
	detail := c.get("/api/v1/playbooks/" + playbookID).expectStatus(http.StatusOK)
	versions := detail.arr("versions")
	if len(versions) != 1 {
		t.Fatalf("got %d versions, want 1", len(versions))
	}
	if summary, _ := versions[0].(map[string]any); summary["status"] != "published" ||
		summary["published_at"] == nil {
		t.Fatalf("published version summary is incomplete: %v", versions[0])
	}

	c.get("/api/v1/playbooks/" + uuid.NewString()).
		expectStatus(http.StatusNotFound).expectCode("RESOURCE_NOT_FOUND")
}

// TestVersionNumbersIncrementPerPlaybook checks that versioning is per
// playbook, not global.
func TestVersionNumbersIncrementPerPlaybook(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	playbookID := c.get("/api/v1/playbook-versions/" + versionID).str("playbook_id")

	for want := 2; want <= 4; want++ {
		created := c.post("/api/v1/playbooks/"+playbookID+"/versions", nil).
			expectStatus(http.StatusCreated)
		if created.Body["version"] != float64(want) {
			t.Fatalf("got version %v, want %d", created.Body["version"], want)
		}
	}

	// A different playbook starts its own numbering at 1.
	other := c.post("/api/v1/playbooks", map[string]any{
		"name": "Separate", "alert_type": "separate_type",
	}).expectStatus(http.StatusCreated)
	if summary, _ := other.arr("versions")[0].(map[string]any); summary["version"] != float64(1) {
		t.Fatalf("a new playbook should start at version 1, got %v", summary["version"])
	}
}

// TestCloneFromForeignPlaybookIsRejected: cloning must stay within a playbook,
// or version lineage stops meaning anything.
func TestCloneFromForeignPlaybookIsRejected(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	foreignVersionID := publishExamplePlaybook(t, c)

	other := c.post("/api/v1/playbooks", map[string]any{
		"name": "Separate", "alert_type": "separate_type",
	}).expectStatus(http.StatusCreated)

	c.post("/api/v1/playbooks/"+other.str("id")+"/versions", map[string]any{
		"clone_from_version_id": foreignVersionID,
	}).expectStatus(http.StatusBadRequest).expectCode("VALIDATION_FAILED")
}
