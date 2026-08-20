package integration

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
)

// TestConcurrentDecisionsSerialize is the concurrency guarantee: two agents
// submitting different decisions on the same investigation at the same instant
// must not both apply. The row lock inside ApplyDecision forces them into an
// order; the loser's edge no longer leaves the (now advanced) current node, so
// it is rejected.
func TestConcurrentDecisionsSerialize(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	// A playbook whose root offers two distinct choices, so the two workers
	// genuinely compete rather than submitting the same edge.
	versionID, root, edgeYes, edgeNo := publishForkPlaybook(t, repos)
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

	// Both goroutines block on the same barrier so they hit the transaction
	// as close to simultaneously as the runtime allows.
	var (
		start   sync.WaitGroup
		done    sync.WaitGroup
		results = make([]error, 2)
	)
	start.Add(1)
	done.Add(2)

	for i, edgeID := range []uuid.UUID{edgeYes, edgeNo} {
		go func(slot int, edge uuid.UUID) {
			defer done.Done()
			start.Wait()
			_, results[slot] = repos.investigations.ApplyDecision(ctx(), inv.ID,
				investigation.DecisionInput{
					EdgeID: edge,
					Actor:  investigation.Actor{Type: investigation.ActorAgent},
				})
		}(i, edgeID)
	}

	start.Done()
	done.Wait()

	successes, rejections := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case apperror.IsCode(err, apperror.CodeInvalidTransition),
			apperror.IsCode(err, apperror.CodeInvestigationCompleted):
			rejections++
		default:
			t.Fatalf("unexpected error from a concurrent decision: %v", err)
		}
	}

	if successes != 1 || rejections != 1 {
		t.Fatalf("expected exactly one decision to apply, got %d successes and %d rejections",
			successes, rejections)
	}

	// The audit trail proves it: exactly one step, and the investigation moved
	// exactly once.
	_, steps, err := repos.investigations.GetWithSteps(ctx(), inv.ID)
	if err != nil {
		t.Fatalf("load steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps after a concurrent race, want exactly 1", len(steps))
	}
	if steps[0].SequenceNumber != 1 {
		t.Fatalf("sequence numbering must stay gapless, got %d", steps[0].SequenceNumber)
	}
}

// TestConcurrentIdenticalRetriesWithIdempotencyKeyApplyOnce covers the retry
// case rather than the race case: the same agent, having timed out, sends the
// same decision twice.
func TestConcurrentIdenticalRetriesWithIdempotencyKeyApplyOnce(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	versionID, root, edgeYes, _ := publishForkPlaybook(t, repos)
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

	key := "retry-" + uuid.NewString()
	input := investigation.DecisionInput{
		EdgeID: edgeYes,
		Actor:  investigation.Actor{Type: investigation.ActorAgent},
		Evidence: []investigation.EvidenceItem{
			{Type: "note", Summary: "same request"},
		},
		IdempotencyKey: &key,
	}

	const attempts = 4
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		errs  = make([]error, attempts)
	)
	start.Add(1)
	done.Add(attempts)

	for i := range attempts {
		go func(slot int) {
			defer done.Done()
			start.Wait()
			_, errs[slot] = repos.investigations.ApplyDecision(ctx(), inv.ID, input)
		}(i)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("retry %d should be a no-op, not an error: %v", i, err)
		}
	}

	_, steps, err := repos.investigations.GetWithSteps(ctx(), inv.ID)
	if err != nil {
		t.Fatalf("load steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("%d identical retries produced %d steps, want 1", attempts, len(steps))
	}
}

// TestIdempotencyKeyReusedWithDifferentBodyIsRejected: reusing a key for a
// genuinely different decision is a client bug, and silently returning the old
// step would hide it.
func TestIdempotencyKeyReusedWithDifferentBodyIsRejected(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)

	headers := map[string]string{"Idempotency-Key": "agent-attempt-1"}
	body := map[string]any{
		"edge_id":   edgeKnownWorkstationYes,
		"actor":     agentActor(),
		"rationale": "known workstation",
	}

	first := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)
	if first.obj("current_node")["id"] != nodeMaintWindow {
		t.Fatalf("first submission should advance the investigation")
	}

	// An identical retry is a no-op returning current state.
	replayed := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)
	if replayed.obj("current_node")["id"] != nodeMaintWindow {
		t.Fatalf("a replay must not advance the investigation a second time")
	}
	if len(replayed.arr("steps")) != 1 {
		t.Fatalf("a replay must not append a step, got %d", len(replayed.arr("steps")))
	}

	// The same key with a different decision is refused.
	different := map[string]any{
		"edge_id":   edgeKnownWorkstationNo,
		"actor":     agentActor(),
		"rationale": "actually unknown workstation",
	}
	c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", different, headers).
		expectStatus(http.StatusConflict).expectCode("IDEMPOTENCY_KEY_REUSED")

	// Still exactly one step recorded.
	if len(c.get("/api/v1/investigations/"+investigationID).arr("steps")) != 1 {
		t.Fatalf("the rejected reuse must not have written anything")
	}
}

// TestIdempotentRetryAfterCompletionIsStillANoOp covers the subtle case: the
// original decision completed the investigation, and the retry arrives after.
// Without recognising the key first, the retry would be rejected as advancing
// a completed investigation — a spurious error for a correct client.
func TestIdempotentRetryAfterCompletionIsStillANoOp(t *testing.T) {
	pool := requireDB(t)
	c := newClient(t, newTestServer(t, pool))

	versionID := publishExamplePlaybook(t, c)
	alertID := createExampleAlert(t, c)
	investigationID := startInvestigation(t, c, alertID, versionID)

	submitDecision(t, c, investigationID, edgeKnownWorkstationYes)

	headers := map[string]string{"Idempotency-Key": "final-decision"}
	body := map[string]any{
		"edge_id":   edgeMaintWindowNo,
		"actor":     agentActor(),
		"rationale": "outside the approved window",
	}

	completed := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)
	if completed.str("status") != "completed" {
		t.Fatalf("expected the investigation to complete")
	}

	// The retry succeeds as a no-op and reports the completed state.
	retry := c.postWithHeaders("/api/v1/investigations/"+investigationID+"/decisions", body, headers).
		expectStatus(http.StatusOK)
	if retry.str("status") != "completed" {
		t.Fatalf("retry should report the completed state")
	}
	if len(retry.arr("steps")) != 2 {
		t.Fatalf("retry must not append a step, got %d", len(retry.arr("steps")))
	}
}

// publishForkPlaybook publishes a playbook whose root offers two distinct
// terminal choices — the minimal shape for testing competing decisions.
func publishForkPlaybook(t *testing.T, repos repositories) (versionID, root, edgeYes, edgeNo uuid.UUID) {
	t.Helper()

	root, yesNode, noNode := uuid.New(), uuid.New(), uuid.New()
	edgeYes, edgeNo = uuid.New(), uuid.New()
	yesResolution, noResolution := "went_yes", "went_no"

	_, def, err := repos.playbooks.Create(ctx(), newForkPlaybook(
		root, yesNode, noNode, edgeYes, edgeNo, yesResolution, noResolution))
	if err != nil {
		t.Fatalf("create fork playbook: %v", err)
	}
	if _, err := repos.playbooks.Publish(ctx(), def.Version.ID); err != nil {
		t.Fatalf("publish fork playbook: %v", err)
	}
	return def.Version.ID, root, edgeYes, edgeNo
}
