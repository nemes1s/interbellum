package integration

import (
	"net/http"
	"testing"
)

// TestCreateVersionAcceptsAbsentBody: creating an empty draft is the common
// case, so `POST .../versions` with no body at all must work.
func TestCreateVersionAcceptsAbsentBody(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	playbookID := c.get("/api/v1/playbook-versions/" + versionID).str("playbook_id")

	created := c.post("/api/v1/playbooks/"+playbookID+"/versions", nil).
		expectStatus(http.StatusCreated)

	if created.Body["version"] != float64(2) {
		t.Fatalf("got version %v, want 2", created.Body["version"])
	}
	if created.Body["status"] != "draft" {
		t.Fatalf("a new version must start as a draft")
	}
	if len(created.arr("nodes")) != 0 {
		t.Fatalf("an uncloned version should start empty, got %d nodes", len(created.arr("nodes")))
	}

	// A malformed body is still rejected — "optional" means absent, not
	// "anything goes".
	c.post("/api/v1/playbooks/"+playbookID+"/versions", map[string]any{"nonsense_field": 1}).
		expectStatus(http.StatusBadRequest).expectCode("BAD_REQUEST")
}

// TestExplicitJSONNullIsTreatedAsAbsent: a client that sends `"evidence": null`
// means "no evidence", and must get the same behaviour as omitting the field —
// including for idempotency comparison, where a JSONB null and a SQL NULL
// would otherwise compare unequal and turn a correct retry into a 409.
func TestExplicitJSONNullIsTreatedAsAbsent(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)

	// An alert with an explicit null payload reads back as {}.
	alertID := c.post("/api/v1/alerts", map[string]any{
		"alert_type":  "unauthorized_plc_register_write",
		"title":       "Null payload",
		"occurred_at": "2026-08-19T10:30:00Z",
		"payload":     nil,
	}).expectStatus(http.StatusCreated).str("id")

	fetched := c.get("/api/v1/alerts/" + alertID).expectStatus(http.StatusOK)
	if payload, ok := fetched.Body["payload"].(map[string]any); !ok || len(payload) != 0 {
		t.Fatalf("a null payload should read back as {}, got %v", fetched.Body["payload"])
	}

	investigationID := startInvestigation(t, c, alertID, versionID)

	headers := map[string]string{"Idempotency-Key": "null-evidence-key"}
	body := map[string]any{
		"edge_id":   edgeKnownWorkstationYes,
		"actor":     agentActor(),
		"rationale": nil,
		"evidence":  nil,
	}

	first := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)

	// Null evidence reads back as an empty array, so clients can iterate it
	// unconditionally.
	step, _ := first.arr("steps")[0].(map[string]any)
	evidence, ok := step["evidence"].([]any)
	if !ok || len(evidence) != 0 {
		t.Fatalf("null evidence should read back as [], got %v", step["evidence"])
	}

	// The retry is recognised as equivalent, not rejected as a key reuse.
	replay := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)
	if len(replay.arr("steps")) != 1 {
		t.Fatalf("the retry must not append a step, got %d", len(replay.arr("steps")))
	}

	// And omitting the fields entirely is treated the same as sending null.
	omitted := map[string]any{"edge_id": edgeKnownWorkstationYes, "actor": agentActor()}
	c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", omitted, headers).
		expectStatus(http.StatusOK)
	if len(c.get("/api/v1/investigations/"+investigationID).arr("steps")) != 1 {
		t.Fatalf("an omitted-field retry must also be a no-op")
	}
}

// TestOversizedBodyIsRejected checks the request body limit is actually wired
// in front of the handlers.
func TestOversizedBodyIsRejected(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	huge := make([]byte, 2<<20) // 2 MiB, over the 1 MiB limit
	for i := range huge {
		huge[i] = 'a'
	}

	resp := c.post("/api/v1/alerts", map[string]any{
		"alert_type":  "test",
		"title":       "Too big",
		"occurred_at": "2026-08-19T10:30:00Z",
		"payload":     map[string]any{"blob": string(huge)},
	})
	if resp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("got status %d, want 413; body: %s", resp.Status, resp.Raw)
	}
	resp.expectCode("PAYLOAD_TOO_LARGE")
}

// TestUnknownEndpointAndMethodReturnStructuredErrors: even the routing edges
// return the machine-readable envelope, so a client never has to parse HTML or
// an empty body.
func TestUnknownEndpointAndMethodReturnStructuredErrors(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	c.get("/api/v1/nope").expectStatus(http.StatusNotFound).expectCode("RESOURCE_NOT_FOUND")
	c.do(http.MethodDelete, "/api/v1/playbooks", nil, nil).
		expectStatus(http.StatusMethodNotAllowed).expectCode("METHOD_NOT_ALLOWED")
}

// TestHealthProbesAnswerGetAndHead: container and load-balancer health checks
// commonly use HEAD (`wget --spider`, which docker-compose.yml runs). A
// GET-only probe would 405 and keep a perfectly healthy instance out of
// service, so both verbs must work.
func TestHealthProbesAnswerGetAndHead(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	for _, path := range []string{"/healthz", "/readyz"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			resp := c.do(method, path, nil, nil)
			if resp.Status != http.StatusOK {
				t.Fatalf("%s %s returned %d, want 200", method, path, resp.Status)
			}
		}
	}
}
