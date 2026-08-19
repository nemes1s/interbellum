// Package httpdto holds the request and response shapes of the HTTP API,
// together with their mapping to and from domain types.
//
// These structs exist separately from the domain so the wire format can evolve
// (field renames, added projections, compatibility shims) without those
// concerns leaking into business logic. They are the Go mirror of
// api/openapi.yaml; when one changes, so must the other.
package httpdto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/domain/playbook"
	"github.com/indurex/interbellum/internal/service/playbookservice"
)

// Playbook mirrors the Playbook schema.
type Playbook struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AlertType   string    `json:"alert_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PlaybookVersionSummary mirrors the PlaybookVersionSummary schema.
type PlaybookVersionSummary struct {
	ID          uuid.UUID  `json:"id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at"`
}

// PlaybookWithVersions mirrors the PlaybookWithVersions schema.
type PlaybookWithVersions struct {
	Playbook
	Versions []PlaybookVersionSummary `json:"versions"`
}

// PlaybookList mirrors the list response envelope.
type PlaybookList struct {
	Items []Playbook `json:"items"`
}

// Node mirrors the PlaybookNode schema.
type Node struct {
	ID                 uuid.UUID       `json:"id"`
	Kind               string          `json:"kind"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	TerminalResolution *string         `json:"terminal_resolution"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}

// Edge mirrors the PlaybookEdge schema.
type Edge struct {
	ID          uuid.UUID `json:"id"`
	FromNodeID  uuid.UUID `json:"from_node_id"`
	ToNodeID    uuid.UUID `json:"to_node_id"`
	Label       string    `json:"label"`
	Description *string   `json:"description"`
	SortOrder   int       `json:"sort_order"`
}

// GraphInput mirrors the PlaybookGraphInput schema. Nothing is required: a
// draft may be saved incomplete, and structural validity is only enforced at
// publish time (docs/domain-model.md §5).
type GraphInput struct {
	RootNodeID *uuid.UUID `json:"root_node_id"`
	Nodes      []Node     `json:"nodes"`
	Edges      []Edge     `json:"edges"`
}

// PlaybookVersionDefinition mirrors the PlaybookVersionDefinition schema:
// version metadata flattened together with the graph.
type PlaybookVersionDefinition struct {
	ID          uuid.UUID  `json:"id"`
	PlaybookID  uuid.UUID  `json:"playbook_id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at"`
	RootNodeID  *uuid.UUID `json:"root_node_id"`
	Nodes       []Node     `json:"nodes"`
	Edges       []Edge     `json:"edges"`
}

// CreatePlaybookRequest mirrors the CreatePlaybookRequest schema.
type CreatePlaybookRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	AlertType   string      `json:"alert_type"`
	Definition  *GraphInput `json:"definition"`
}

// CreatePlaybookVersionRequest mirrors the CreatePlaybookVersionRequest schema.
type CreatePlaybookVersionRequest struct {
	CloneFromVersionID *uuid.UUID `json:"clone_from_version_id"`
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

// ToPlaybook maps a domain playbook to its wire form.
func ToPlaybook(p playbook.Playbook) Playbook {
	return Playbook{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		AlertType:   p.AlertType,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// ToPlaybookList maps a slice of playbooks into the list envelope.
func ToPlaybookList(playbooks []playbook.Playbook) PlaybookList {
	items := make([]Playbook, 0, len(playbooks))
	for _, p := range playbooks {
		items = append(items, ToPlaybook(p))
	}
	return PlaybookList{Items: items}
}

// ToPlaybookWithVersions maps a playbook and its version summaries.
func ToPlaybookWithVersions(in playbookservice.PlaybookWithVersions) PlaybookWithVersions {
	versions := make([]PlaybookVersionSummary, 0, len(in.Versions))
	for _, v := range in.Versions {
		versions = append(versions, PlaybookVersionSummary{
			ID:          v.ID,
			Version:     v.Version,
			Status:      string(v.Status),
			CreatedAt:   v.CreatedAt,
			PublishedAt: v.PublishedAt,
		})
	}
	return PlaybookWithVersions{Playbook: ToPlaybook(in.Playbook), Versions: versions}
}

// ToNode maps a domain node to its wire form.
func ToNode(n playbook.Node) Node {
	return Node{
		ID:                 n.ID,
		Kind:               string(n.Kind),
		Title:              n.Title,
		Description:        n.Description,
		TerminalResolution: n.TerminalResolution,
		Metadata:           json.RawMessage(n.Metadata),
	}
}

// ToEdge maps a domain edge to its wire form.
func ToEdge(e playbook.Edge) Edge {
	return Edge{
		ID:          e.ID,
		FromNodeID:  e.FromNodeID,
		ToNodeID:    e.ToNodeID,
		Label:       e.Label,
		Description: e.Description,
		SortOrder:   e.SortOrder,
	}
}

// ToDefinition maps a version and its graph to the flattened wire form.
func ToDefinition(d playbook.Definition) PlaybookVersionDefinition {
	nodes := make([]Node, 0, len(d.Graph.Nodes))
	for _, n := range d.Graph.Nodes {
		nodes = append(nodes, ToNode(n))
	}
	edges := make([]Edge, 0, len(d.Graph.Edges))
	for _, e := range d.Graph.Edges {
		edges = append(edges, ToEdge(e))
	}

	return PlaybookVersionDefinition{
		ID:          d.Version.ID,
		PlaybookID:  d.Version.PlaybookID,
		Version:     d.Version.Version,
		Status:      string(d.Version.Status),
		CreatedAt:   d.Version.CreatedAt,
		PublishedAt: d.Version.PublishedAt,
		RootNodeID:  d.Graph.RootNodeID,
		Nodes:       nodes,
		Edges:       edges,
	}
}

// ToGraph maps a submitted graph payload into the domain type. IDs are
// client-supplied so a designer can author a whole graph, including its
// internal references, in one request — the database still enforces that every
// reference resolves within the same version.
func (g GraphInput) ToGraph() playbook.Graph {
	graph := playbook.Graph{
		RootNodeID: g.RootNodeID,
		Nodes:      make([]playbook.Node, 0, len(g.Nodes)),
		Edges:      make([]playbook.Edge, 0, len(g.Edges)),
	}
	for _, n := range g.Nodes {
		graph.Nodes = append(graph.Nodes, playbook.Node{
			ID:                 n.ID,
			Kind:               playbook.NodeKind(n.Kind),
			Title:              n.Title,
			Description:        n.Description,
			TerminalResolution: n.TerminalResolution,
			Metadata:           normalizeJSON(n.Metadata),
		})
	}
	for _, e := range g.Edges {
		graph.Edges = append(graph.Edges, playbook.Edge{
			ID:          e.ID,
			FromNodeID:  e.FromNodeID,
			ToNodeID:    e.ToNodeID,
			Label:       e.Label,
			Description: e.Description,
			SortOrder:   e.SortOrder,
		})
	}
	return graph
}
