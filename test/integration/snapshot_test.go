package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// TestCloneWaitsForAnInFlightGraphEdit asserts the lock that keeps a clone
// from reading a half-replaced graph.
//
// Loading a definition takes three statements (version/root, then nodes, then
// edges). Under PostgreSQL's default READ COMMITTED each one gets its own
// snapshot, so a ReplaceGraph committing partway through would leave the clone
// holding the old root together with the new nodes — a version whose
// root_node_id names a node that is not in it.
//
// The torn read itself is not reproducible on demand: the gap between those
// three statements is microseconds wide, and hitting it would need a seam in
// production code that exists only for tests. What *is* deterministic, and is
// what actually prevents the tear, is that a clone cannot read a version while
// another transaction holds it for writing. That is asserted here directly: a
// transaction holds the version row FOR UPDATE — exactly what ReplaceGraph
// holds for its whole duration — and the clone must wait rather than read
// through it.
//
// Removing the FOR SHARE lock in CreateVersion makes this test fail: the clone
// stops blocking and returns immediately.
func TestCloneWaitsForAnInFlightGraphEdit(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	original, _, _, _ := simplePlaybook("Clone lock")
	pb, def, err := repos.playbooks.Create(ctx(), original)
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Hold the version row exactly as an in-flight ReplaceGraph would.
	holder, err := pool.Begin(ctx())
	if err != nil {
		t.Fatalf("begin holding transaction: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx()) }()

	var lockedID uuid.UUID
	if err := holder.QueryRow(ctx(),
		`SELECT id FROM playbook_versions WHERE id = $1 FOR UPDATE`, def.Version.ID).
		Scan(&lockedID); err != nil {
		t.Fatalf("lock version: %v", err)
	}

	cloneDone := make(chan error, 1)
	go func() {
		_, cloneErr := repos.playbooks.CreateVersion(ctx(), pb.ID, &def.Version.ID)
		cloneDone <- cloneErr
	}()

	// The clone must block while the writer holds the row.
	select {
	case err := <-cloneDone:
		t.Fatalf("clone completed (%v) while a writer held the source version; "+
			"it read through an in-flight graph edit instead of waiting", err)
	case <-time.After(500 * time.Millisecond):
		// Still blocked, which is the expected behaviour.
	}

	// Releasing the writer lets the clone through.
	if err := holder.Rollback(ctx()); err != nil {
		t.Fatalf("release holding transaction: %v", err)
	}

	select {
	case err := <-cloneDone:
		if err != nil {
			t.Fatalf("clone failed after the writer released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("clone never completed after the writer released its lock")
	}
}

// TestInvestigationStateIsAConsistentSnapshot races reading investigation
// state against a decision being applied.
//
// The state read must never show a current node that its own step list has
// already moved past — an agent deciding what to do next would otherwise see a
// question it has already answered.
func TestInvestigationStateIsAConsistentSnapshot(t *testing.T) {
	pool := requireDB(t)
	repos := newRepositories(pool)

	// A three-step chain, so there is a real sequence to be caught mid-flight.
	versionID, nodes, edges := publishChainPlaybook(t, repos, 3)
	alertID := createAlert(t, repos)

	// A stress loop, not a deterministic one: the window is real but narrow, so
	// a single pass proves little. Verified to reproduce the inconsistency
	// within 400 attempts when the REPEATABLE READ snapshot is removed.
	for attempt := range 400 {
		inv, err := repos.investigations.Create(ctx(), investigation.Investigation{
			AlertID:           alertID,
			PlaybookVersionID: versionID,
			Status:            investigation.StatusInProgress,
			CurrentNodeID:     nodes[0],
			StartedAt:         time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("attempt %d: create investigation: %v", attempt, err)
		}

		var (
			start sync.WaitGroup
			done  sync.WaitGroup

			readInv   investigation.Investigation
			readSteps []investigation.Step
			readErr   error
		)
		start.Add(1)
		done.Add(2)

		go func() {
			defer done.Done()
			start.Wait()
			readInv, readSteps, readErr = repos.investigations.GetWithSteps(ctx(), inv.ID)
		}()
		go func() {
			defer done.Done()
			start.Wait()
			for _, edge := range edges {
				_, _ = repos.investigations.ApplyDecision(ctx(), inv.ID,
					investigation.DecisionInput{
						EdgeID: edge,
						Actor:  investigation.Actor{Type: investigation.ActorAgent},
					})
			}
		}()

		start.Done()
		done.Wait()

		if readErr != nil {
			t.Fatalf("attempt %d: read state: %v", attempt, readErr)
		}

		// The invariant: current_node_id must be exactly where the recorded
		// steps say the investigation is. With N steps applied, the current
		// node is nodes[N].
		wantNode := nodes[len(readSteps)]
		if readInv.CurrentNodeID != wantNode {
			t.Fatalf("attempt %d: inconsistent snapshot — %d steps recorded, so current node "+
				"should be %s, but state says %s",
				attempt, len(readSteps), wantNode, readInv.CurrentNodeID)
		}

		// Completion must agree with the step count too.
		if readInv.IsCompleted() != (len(readSteps) == len(edges)) {
			t.Fatalf("attempt %d: status %q disagrees with %d/%d steps recorded",
				attempt, readInv.Status, len(readSteps), len(edges))
		}
	}
}

// publishChainPlaybook publishes a linear playbook: length decision nodes in a
// chain, ending in a terminal node. Returns the node IDs in order (the last
// being the terminal) and the edge IDs in order.
func publishChainPlaybook(t *testing.T, repos repositories, length int) (uuid.UUID, []uuid.UUID, []uuid.UUID) {
	t.Helper()

	nodes := make([]uuid.UUID, 0, length+1)
	graphNodes := make([]playbook.Node, 0, length+1)
	for i := range length {
		id := uuid.New()
		nodes = append(nodes, id)
		graphNodes = append(graphNodes, playbook.Node{
			ID: id, Kind: playbook.KindDecision, Title: "Step " + uuid.NewString()[:8],
		})
		_ = i
	}
	terminalID := uuid.New()
	nodes = append(nodes, terminalID)
	resolution := "chain_complete"
	graphNodes = append(graphNodes, playbook.Node{
		ID: terminalID, Kind: playbook.KindTerminal, Title: "Done", TerminalResolution: &resolution,
	})

	edges := make([]uuid.UUID, 0, length)
	graphEdges := make([]playbook.Edge, 0, length)
	for i := range length {
		id := uuid.New()
		edges = append(edges, id)
		graphEdges = append(graphEdges, playbook.Edge{
			ID: id, FromNodeID: nodes[i], ToNodeID: nodes[i+1], Label: "Next",
		})
	}

	_, def, err := repos.playbooks.Create(ctx(), playbook.NewPlaybook{
		Name:      "Chain " + uuid.NewString(),
		AlertType: "test_type",
		Graph:     playbook.Graph{RootNodeID: &nodes[0], Nodes: graphNodes, Edges: graphEdges},
	})
	if err != nil {
		t.Fatalf("create chain playbook: %v", err)
	}
	if _, err := repos.playbooks.Publish(ctx(), def.Version.ID); err != nil {
		t.Fatalf("publish chain playbook: %v", err)
	}
	return def.Version.ID, nodes, edges
}
