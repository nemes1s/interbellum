// Package playbook contains the playbook design domain: the logical playbook,
// its versions, and the decision graph (nodes and edges) each version owns.
//
// This package has no framework dependencies — no pgx, no chi, no net/http —
// so its rules (graph validation in particular) are unit-testable in
// isolation. See docs/package-structure.md for the boundary rules.
package playbook

import (
	"time"

	"github.com/google/uuid"
)

// VersionStatus is the lifecycle state of a playbook version:
// draft -> published -> archived. See docs/domain-model.md §4.
type VersionStatus string

const (
	// StatusDraft versions are mutable; their graph can be replaced wholesale.
	StatusDraft VersionStatus = "draft"
	// StatusPublished versions are frozen forever and may back investigations.
	StatusPublished VersionStatus = "published"
	// StatusArchived versions stay readable (historical investigations still
	// resolve against them) but should not back new investigations.
	StatusArchived VersionStatus = "archived"
)

// NodeKind distinguishes a question from an outcome.
type NodeKind string

const (
	// KindDecision nodes pose a question and must have at least one outgoing edge.
	KindDecision NodeKind = "decision"
	// KindTerminal nodes end an investigation and must have no outgoing edges.
	KindTerminal NodeKind = "terminal"
)

// Valid reports whether k is a known node kind.
func (k NodeKind) Valid() bool {
	return k == KindDecision || k == KindTerminal
}

// Playbook is the logical container for a family of versions. Its own fields
// are metadata only: investigations bind to a Version, never to a Playbook, so
// editing name/description cannot change the meaning of past investigations.
type Playbook struct {
	ID          uuid.UUID
	Name        string
	Description string
	// AlertType is immutable after creation — it classifies which alerts this
	// playbook is for, and changing it would silently reclassify every
	// existing version including published ones. See docs/domain-model.md §2.
	AlertType string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Version is one snapshot of a decision graph. Once published it is immutable,
// which is what makes historical investigations auditable independently of
// later playbook edits.
type Version struct {
	ID          uuid.UUID
	PlaybookID  uuid.UUID
	Version     int
	Status      VersionStatus
	RootNodeID  *uuid.UUID
	CreatedAt   time.Time
	PublishedAt *time.Time
}

// IsDraft reports whether the version's graph may still be modified.
func (v Version) IsDraft() bool { return v.Status == StatusDraft }

// Node is a single point in a version's decision graph.
type Node struct {
	ID          uuid.UUID
	Kind        NodeKind
	Title       string
	Description string
	// TerminalResolution is set iff Kind is KindTerminal. It is copied onto
	// the investigation as final_resolution when reached.
	TerminalResolution *string
	// Metadata is designer-defined and opaque to the engine (e.g. suggested
	// evidence queries an agent might run at this node).
	Metadata []byte
}

// Edge is a directed, labeled choice between two nodes of the same version.
type Edge struct {
	ID          uuid.UUID
	FromNodeID  uuid.UUID
	ToNodeID    uuid.UUID
	Label       string
	Description *string
	SortOrder   int
}

// Graph is a version's complete node/edge set plus its chosen root. It is the
// unit read and written at the API boundary, and the unit Validate operates on.
type Graph struct {
	RootNodeID *uuid.UUID
	Nodes      []Node
	Edges      []Edge
}

// Definition is a version together with its graph — what GET/PUT
// /playbook-versions/{id} returns.
type Definition struct {
	Version Version
	Graph   Graph
}

// NodeByID returns the node with the given ID, if present.
func (g Graph) NodeByID(id uuid.UUID) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// EdgeByID returns the edge with the given ID, if present.
func (g Graph) EdgeByID(id uuid.UUID) (Edge, bool) {
	for _, e := range g.Edges {
		if e.ID == id {
			return e, true
		}
	}
	return Edge{}, false
}

// OutgoingEdges returns the edges leaving a node, ordered by sort_order then
// label so that a UI and an agent always see choices in the same order.
func (g Graph) OutgoingEdges(nodeID uuid.UUID) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.FromNodeID == nodeID {
			out = append(out, e)
		}
	}
	sortEdges(out)
	return out
}

func sortEdges(edges []Edge) {
	// Insertion sort: edge fan-out per node is tiny (2-3 in practice), and
	// this keeps the domain package free of extra imports for no real cost.
	for i := 1; i < len(edges); i++ {
		for j := i; j > 0; j-- {
			a, b := edges[j-1], edges[j]
			if a.SortOrder < b.SortOrder || (a.SortOrder == b.SortOrder && a.Label <= b.Label) {
				break
			}
			edges[j-1], edges[j] = b, a
		}
	}
}
