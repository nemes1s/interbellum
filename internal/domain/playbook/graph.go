package playbook

import (
	"sort"

	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/apperror"
)

// IssueReason enumerates the semantic problems publish-time validation can
// report. The values are part of the API contract (see the
// GraphValidationIssue schema in api/openapi.yaml).
//
// Duplicate edge labels at a node are deliberately absent: they are rejected
// earlier, when the draft graph is written, by a database unique constraint on
// (from_node_id, label). A draft carrying that problem cannot be persisted, so
// publish never has to check for it.
type IssueReason string

const (
	ReasonMissingRoot               IssueReason = "missing_root"
	ReasonDanglingReference         IssueReason = "dangling_reference"
	ReasonUnreachableFromRoot       IssueReason = "unreachable_from_root"
	ReasonCycleDetected             IssueReason = "cycle_detected"
	ReasonDecisionNodeWithoutEdges  IssueReason = "decision_node_without_edges"
	ReasonTerminalNodeWithEdges     IssueReason = "terminal_node_with_edges"
	ReasonTerminalMissingResolution IssueReason = "terminal_node_missing_resolution"
	ReasonDecisionWithResolution    IssueReason = "decision_node_with_resolution"
)

// ValidationIssue is one problem found in a graph. Exactly one of NodeID/EdgeID
// is set for most reasons; ReasonMissingRoot sets neither.
type ValidationIssue struct {
	NodeID *uuid.UUID  `json:"node_id,omitempty"`
	EdgeID *uuid.UUID  `json:"edge_id,omitempty"`
	Reason IssueReason `json:"reason"`
}

// Validate checks every publish-time rule from docs/domain-model.md §5 and
// returns *all* problems found, not just the first. Designers get to fix a
// broken graph in one pass instead of one publish attempt per mistake.
//
// Chosen graph model: strict DAG — cycles are rejected. Re-convergence
// (two branches leading to the same terminal node) is allowed, since it is
// both natural for playbooks and harmless for traversal. Because the graph is
// acyclic, decision-time traversal needs no depth guard.
//
// Returns nil when the graph is publishable.
func (g Graph) Validate() []ValidationIssue {
	var issues []ValidationIssue

	nodesByID := make(map[uuid.UUID]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodesByID[n.ID] = n
	}

	// 1. Root must be set and must reference a node in this graph.
	if g.RootNodeID == nil {
		issues = append(issues, ValidationIssue{Reason: ReasonMissingRoot})
	} else if _, ok := nodesByID[*g.RootNodeID]; !ok {
		root := *g.RootNodeID
		issues = append(issues, ValidationIssue{NodeID: &root, Reason: ReasonDanglingReference})
	}

	// 2. Every edge endpoint must exist. Edges failing this are excluded from
	//    the adjacency map so reachability/cycle checks operate on a sane graph
	//    rather than reporting cascading noise.
	adjacency := make(map[uuid.UUID][]uuid.UUID, len(g.Nodes))
	outDegree := make(map[uuid.UUID]int, len(g.Nodes))
	for _, e := range g.Edges {
		_, fromOK := nodesByID[e.FromNodeID]
		_, toOK := nodesByID[e.ToNodeID]
		if !fromOK || !toOK {
			id := e.ID
			issues = append(issues, ValidationIssue{EdgeID: &id, Reason: ReasonDanglingReference})
			continue
		}
		adjacency[e.FromNodeID] = append(adjacency[e.FromNodeID], e.ToNodeID)
		outDegree[e.FromNodeID]++
	}

	// 3/4. Node kind rules: decisions need choices and no resolution;
	//      terminals need a resolution and no choices.
	for _, n := range g.Nodes {
		id := n.ID
		switch n.Kind {
		case KindDecision:
			if outDegree[id] == 0 {
				issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonDecisionNodeWithoutEdges})
			}
			if n.TerminalResolution != nil {
				issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonDecisionWithResolution})
			}
		case KindTerminal:
			if outDegree[id] > 0 {
				issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonTerminalNodeWithEdges})
			}
			if n.TerminalResolution == nil || *n.TerminalResolution == "" {
				issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonTerminalMissingResolution})
			}
		}
	}

	// 5. Acyclicity, over the whole node set (not only the reachable part), so
	//    a cycle sitting in an orphaned component is still reported.
	issues = append(issues, detectCycles(g.Nodes, adjacency)...)

	// 6. Reachability from the root. Skipped when there is no usable root —
	//    reporting every node as unreachable on top of missing_root would be
	//    noise, not information.
	if g.RootNodeID != nil {
		if _, ok := nodesByID[*g.RootNodeID]; ok {
			issues = append(issues, detectUnreachable(g.Nodes, adjacency, *g.RootNodeID)...)
		}
	}

	if len(issues) == 0 {
		return nil
	}
	sortIssues(issues)
	return issues
}

// detectCycles runs an iterative colored DFS from every node. Nodes that
// participate in a cycle are reported individually so a designer can see
// exactly which part of the graph loops.
func detectCycles(nodes []Node, adjacency map[uuid.UUID][]uuid.UUID) []ValidationIssue {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[uuid.UUID]int, len(nodes))
	inCycle := make(map[uuid.UUID]bool)

	// Iterative DFS keeps a deep or malformed graph from blowing the stack.
	type frame struct {
		node uuid.UUID
		next int
	}
	for _, start := range nodes {
		if color[start.ID] != white {
			continue
		}
		stack := []frame{{node: start.ID}}
		color[start.ID] = grey
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			neighbours := adjacency[top.node]
			if top.next >= len(neighbours) {
				color[top.node] = black
				stack = stack[:len(stack)-1]
				continue
			}
			next := neighbours[top.next]
			top.next++
			switch color[next] {
			case white:
				color[next] = grey
				stack = append(stack, frame{node: next})
			case grey:
				// Back edge: everything on the stack from `next` upward is on
				// the cycle.
				inCycle[next] = true
				for i := len(stack) - 1; i >= 0; i-- {
					inCycle[stack[i].node] = true
					if stack[i].node == next {
						break
					}
				}
			}
		}
	}

	var issues []ValidationIssue
	for _, n := range nodes {
		if inCycle[n.ID] {
			id := n.ID
			issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonCycleDetected})
		}
	}
	return issues
}

// detectUnreachable reports nodes not reachable from the root by BFS.
func detectUnreachable(nodes []Node, adjacency map[uuid.UUID][]uuid.UUID, root uuid.UUID) []ValidationIssue {
	seen := map[uuid.UUID]bool{root: true}
	queue := []uuid.UUID{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[cur] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}

	var issues []ValidationIssue
	for _, n := range nodes {
		if !seen[n.ID] {
			id := n.ID
			issues = append(issues, ValidationIssue{NodeID: &id, Reason: ReasonUnreachableFromRoot})
		}
	}
	return issues
}

// sortIssues gives the response a deterministic order (by reason, then by the
// referenced ID) so tests and API consumers see stable output.
func sortIssues(issues []ValidationIssue) {
	key := func(i ValidationIssue) string {
		switch {
		case i.NodeID != nil:
			return i.NodeID.String()
		case i.EdgeID != nil:
			return i.EdgeID.String()
		default:
			return ""
		}
	}
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].Reason != issues[b].Reason {
			return issues[a].Reason < issues[b].Reason
		}
		return key(issues[a]) < key(issues[b])
	})
}

// ValidationError converts issues into the application error the API returns
// as 422 with a structured `details` array.
func ValidationError(issues []ValidationIssue) *apperror.Error {
	err := apperror.New(apperror.CodeInvalidPlaybookGraph,
		"playbook graph is not valid for publishing (%d problem(s) found)", len(issues))
	for _, issue := range issues {
		err.Details = append(err.Details, issue)
	}
	return err
}
