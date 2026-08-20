package playbook_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/playbook"
)

// builder keeps the graph fixtures in these tests readable: tests should show
// the shape being validated, not UUID bookkeeping.
type builder struct {
	t     *testing.T
	ids   map[string]uuid.UUID
	nodes []playbook.Node
	edges []playbook.Edge
	root  *uuid.UUID
}

func newBuilder(t *testing.T) *builder {
	t.Helper()
	return &builder{t: t, ids: map[string]uuid.UUID{}}
}

func (b *builder) id(name string) uuid.UUID {
	if id, ok := b.ids[name]; ok {
		return id
	}
	id := uuid.New()
	b.ids[name] = id
	return id
}

func (b *builder) decision(name string) *builder {
	b.nodes = append(b.nodes, playbook.Node{
		ID:    b.id(name),
		Kind:  playbook.KindDecision,
		Title: name,
	})
	return b
}

func (b *builder) terminal(name, resolution string) *builder {
	b.nodes = append(b.nodes, playbook.Node{
		ID:                 b.id(name),
		Kind:               playbook.KindTerminal,
		Title:              name,
		TerminalResolution: &resolution,
	})
	return b
}

func (b *builder) edge(from, label, to string) *builder {
	b.edges = append(b.edges, playbook.Edge{
		ID:         uuid.New(),
		FromNodeID: b.id(from),
		ToNodeID:   b.id(to),
		Label:      label,
	})
	return b
}

func (b *builder) rootAt(name string) *builder {
	id := b.id(name)
	b.root = &id
	return b
}

func (b *builder) graph() playbook.Graph {
	return playbook.Graph{RootNodeID: b.root, Nodes: b.nodes, Edges: b.edges}
}

// reasons collects the issue reasons, which is what tests assert on: the exact
// UUIDs are incidental, the set of problems reported is the behaviour.
func reasons(issues []playbook.ValidationIssue) []playbook.IssueReason {
	out := make([]playbook.IssueReason, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Reason)
	}
	return out
}

func assertReasons(t *testing.T, issues []playbook.ValidationIssue, want ...playbook.IssueReason) {
	t.Helper()
	got := reasons(issues)
	if len(got) != len(want) {
		t.Fatalf("got %d issues %v, want %d %v", len(got), got, len(want), want)
	}
	remaining := map[playbook.IssueReason]int{}
	for _, r := range want {
		remaining[r]++
	}
	for _, r := range got {
		remaining[r]--
		if remaining[r] < 0 {
			t.Fatalf("unexpected issue %q; got %v, want %v", r, got, want)
		}
	}
}

func TestValidateAcceptsExamplePlaybook(t *testing.T) {
	// The assignment's "Unauthorized PLC Register Write" tree.
	b := newBuilder(t).
		decision("known_ws").
		decision("maint_window").
		decision("sis_register").
		decision("historian_anomaly").
		decision("seen_before").
		terminal("close_authorized", "close_authorized_maintenance").
		terminal("escalate_safety", "escalate_safety_review").
		terminal("escalate_unauthorized", "escalate_unauthorized_change").
		terminal("contain", "contain_isolate_segment").
		terminal("escalate_suspicious", "escalate_suspicious_activity").
		terminal("escalate_intrusion", "escalate_possible_intrusion").
		edge("known_ws", "Yes", "maint_window").
		edge("known_ws", "No", "historian_anomaly").
		edge("maint_window", "Yes", "sis_register").
		edge("maint_window", "No", "escalate_unauthorized").
		edge("sis_register", "No", "close_authorized").
		edge("sis_register", "Yes", "escalate_safety").
		edge("historian_anomaly", "Yes", "contain").
		edge("historian_anomaly", "No", "seen_before").
		edge("seen_before", "Yes", "escalate_suspicious").
		edge("seen_before", "No", "escalate_intrusion").
		rootAt("known_ws")

	if issues := b.graph().Validate(); issues != nil {
		t.Fatalf("expected the example playbook to be valid, got %v", issues)
	}
}

func TestValidateAcceptsSingleTerminalNode(t *testing.T) {
	// Degenerate but legal: a playbook that always reaches the same outcome.
	// Investigations started against it are born completed (see decision_test).
	b := newBuilder(t).terminal("only", "auto_close").rootAt("only")
	if issues := b.graph().Validate(); issues != nil {
		t.Fatalf("expected single-terminal graph to be valid, got %v", issues)
	}
}

func TestValidateAllowsReconvergence(t *testing.T) {
	// Two branches leading to the same terminal is a DAG, not a cycle, and is
	// natural for playbooks ("either way, escalate"). It must be accepted.
	b := newBuilder(t).
		decision("root").
		decision("mid").
		terminal("end", "escalate").
		edge("root", "Yes", "mid").
		edge("root", "No", "end").
		edge("mid", "Yes", "end").
		rootAt("root")

	if issues := b.graph().Validate(); issues != nil {
		t.Fatalf("expected re-convergent DAG to be valid, got %v", issues)
	}
}

func TestValidateRejectsMissingRoot(t *testing.T) {
	b := newBuilder(t).terminal("only", "close")
	issues := b.graph().Validate()
	assertReasons(t, issues, playbook.ReasonMissingRoot)
}

func TestValidateRejectsRootNotInGraph(t *testing.T) {
	b := newBuilder(t).terminal("only", "close")
	orphan := uuid.New()
	g := b.graph()
	g.RootNodeID = &orphan

	issues := g.Validate()
	// Only the dangling root is reported. Reachability is deliberately skipped
	// when the root is unusable: flagging every node in the graph as
	// unreachable on top of it would bury the one problem that matters.
	assertReasons(t, issues, playbook.ReasonDanglingReference)
	if issues[0].NodeID == nil || *issues[0].NodeID != orphan {
		t.Fatalf("expected the dangling root id to be flagged, got %+v", issues[0])
	}
}

func TestValidateRejectsDanglingEdgeTarget(t *testing.T) {
	b := newBuilder(t).decision("root").rootAt("root")
	g := b.graph()
	g.Edges = []playbook.Edge{{
		ID:         uuid.New(),
		FromNodeID: g.Nodes[0].ID,
		ToNodeID:   uuid.New(), // never declared
		Label:      "Yes",
	}}

	issues := g.Validate()
	// The edge is discarded as dangling, which leaves the decision node with
	// no usable outgoing choice — both are reported in one pass.
	assertReasons(t, issues,
		playbook.ReasonDanglingReference,
		playbook.ReasonDecisionNodeWithoutEdges,
	)
}

func TestValidateRejectsCycle(t *testing.T) {
	b := newBuilder(t).
		decision("a").
		decision("b").
		decision("c").
		edge("a", "next", "b").
		edge("b", "next", "c").
		edge("c", "back", "a").
		rootAt("a")

	issues := b.graph().Validate()
	// Every node on the loop is flagged so a designer can see the whole cycle.
	assertReasons(t, issues,
		playbook.ReasonCycleDetected,
		playbook.ReasonCycleDetected,
		playbook.ReasonCycleDetected,
	)
}

func TestValidateRejectsSelfLoop(t *testing.T) {
	b := newBuilder(t).
		decision("a").
		terminal("end", "close").
		edge("a", "again", "a").
		edge("a", "done", "end").
		rootAt("a")

	issues := b.graph().Validate()
	assertReasons(t, issues, playbook.ReasonCycleDetected)
}

func TestValidateReportsCycleInUnreachableComponent(t *testing.T) {
	// A cycle hiding in an orphaned component must still be reported: the
	// designer probably meant to connect it.
	b := newBuilder(t).
		decision("root").
		terminal("end", "close").
		decision("orphan_a").
		decision("orphan_b").
		edge("root", "done", "end").
		edge("orphan_a", "x", "orphan_b").
		edge("orphan_b", "y", "orphan_a").
		rootAt("root")

	issues := b.graph().Validate()
	assertReasons(t, issues,
		playbook.ReasonCycleDetected,
		playbook.ReasonCycleDetected,
		playbook.ReasonUnreachableFromRoot,
		playbook.ReasonUnreachableFromRoot,
	)
}

func TestValidateRejectsUnreachableNode(t *testing.T) {
	b := newBuilder(t).
		decision("root").
		terminal("end", "close").
		terminal("orphan", "never_used").
		edge("root", "done", "end").
		rootAt("root")

	issues := b.graph().Validate()
	assertReasons(t, issues, playbook.ReasonUnreachableFromRoot)
	if issues[0].NodeID == nil || *issues[0].NodeID != b.id("orphan") {
		t.Fatalf("expected the orphan node to be flagged, got %+v", issues[0])
	}
}

func TestValidateRejectsDecisionNodeWithoutEdges(t *testing.T) {
	b := newBuilder(t).decision("root").rootAt("root")
	assertReasons(t, b.graph().Validate(), playbook.ReasonDecisionNodeWithoutEdges)
}

func TestValidateRejectsTerminalNodeWithEdges(t *testing.T) {
	b := newBuilder(t).
		decision("root").
		terminal("end", "close").
		terminal("after", "close_again").
		edge("root", "done", "end").
		edge("end", "oops", "after").
		rootAt("root")

	assertReasons(t, b.graph().Validate(), playbook.ReasonTerminalNodeWithEdges)
}

func TestValidateRejectsTerminalWithoutResolution(t *testing.T) {
	b := newBuilder(t).decision("root").edge("root", "done", "end").rootAt("root")
	g := b.graph()
	g.Nodes = append(g.Nodes, playbook.Node{
		ID:    b.id("end"),
		Kind:  playbook.KindTerminal,
		Title: "end",
		// TerminalResolution deliberately nil.
	})

	assertReasons(t, g.Validate(), playbook.ReasonTerminalMissingResolution)
}

func TestValidateRejectsDecisionWithResolution(t *testing.T) {
	b := newBuilder(t).terminal("end", "close").rootAt("root")
	resolution := "should not be here"
	g := b.graph()
	g.Nodes = append(g.Nodes, playbook.Node{
		ID:                 b.id("root"),
		Kind:               playbook.KindDecision,
		Title:              "root",
		TerminalResolution: &resolution,
	})
	g.Edges = append(g.Edges, playbook.Edge{
		ID:         uuid.New(),
		FromNodeID: b.id("root"),
		ToNodeID:   b.id("end"),
		Label:      "done",
	})

	assertReasons(t, g.Validate(), playbook.ReasonDecisionWithResolution)
}

func TestValidateCollectsAllProblemsInOnePass(t *testing.T) {
	// A designer fixing a broken graph should not need one publish attempt per
	// mistake, so validation must not be fail-fast.
	b := newBuilder(t).
		decision("root").
		decision("dead_end").
		terminal("orphan", "never").
		edge("root", "go", "dead_end").
		rootAt("root")

	issues := b.graph().Validate()
	assertReasons(t, issues,
		playbook.ReasonDecisionNodeWithoutEdges, // dead_end has no choices
		playbook.ReasonUnreachableFromRoot,      // orphan is disconnected
	)
}

func TestValidationErrorCarriesIssuesAsDetails(t *testing.T) {
	b := newBuilder(t).decision("root").rootAt("root")
	issues := b.graph().Validate()

	err := playbook.ValidationError(issues)
	if err.Code != apperror.CodeInvalidPlaybookGraph {
		t.Fatalf("got code %q, want %q", err.Code, apperror.CodeInvalidPlaybookGraph)
	}
	// The HTTP status this maps to is asserted in the HTTP layer, which owns
	// that mapping; the domain only commits to the code.
	if len(err.Details) != len(issues) {
		t.Fatalf("got %d details, want %d", len(err.Details), len(issues))
	}
}

func TestOutgoingEdgesAreOrderedDeterministically(t *testing.T) {
	root := uuid.New()
	target := uuid.New()
	g := playbook.Graph{
		Nodes: []playbook.Node{{ID: root, Kind: playbook.KindDecision}, {ID: target, Kind: playbook.KindDecision}},
		Edges: []playbook.Edge{
			{ID: uuid.New(), FromNodeID: root, ToNodeID: target, Label: "No", SortOrder: 2},
			{ID: uuid.New(), FromNodeID: root, ToNodeID: target, Label: "Yes", SortOrder: 1},
			{ID: uuid.New(), FromNodeID: root, ToNodeID: target, Label: "Maybe", SortOrder: 1},
		},
	}

	got := g.OutgoingEdges(root)
	want := []string{"Maybe", "Yes", "No"} // sort_order first, then label
	for i, label := range want {
		if got[i].Label != label {
			t.Fatalf("position %d: got %q, want %q", i, got[i].Label, label)
		}
	}
}
