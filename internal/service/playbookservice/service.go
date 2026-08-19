// Package playbookservice orchestrates playbook design use cases.
//
// Most methods here are thin — validate the request shape, then call one
// repository operation. That is deliberate: the interesting rules live either
// in the domain (graph validation) or in the database (constraints), and
// inserting a layer that merely forwards would add indirection without adding
// safety. What this layer does own is input validation that the domain cannot
// express on its own, such as "a node kind must be one the schema knows".
package playbookservice

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/domain/playbook"
)

// Service exposes the playbook use cases to the HTTP layer.
type Service struct {
	repo playbook.Repository
	log  *slog.Logger
}

// New constructs the service.
func New(repo playbook.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// PlaybookWithVersions is a playbook plus the summaries of its versions.
type PlaybookWithVersions struct {
	Playbook playbook.Playbook
	Versions []playbook.Version
}

// Create creates a playbook and its first draft version.
func (s *Service) Create(ctx context.Context, in playbook.NewPlaybook) (playbook.Playbook, playbook.Definition, error) {
	if in.Name == "" {
		return playbook.Playbook{}, playbook.Definition{}, apperror.Validation("name is required")
	}
	if in.AlertType == "" {
		return playbook.Playbook{}, playbook.Definition{}, apperror.Validation("alert_type is required")
	}
	if err := validateGraphInput(in.Graph); err != nil {
		return playbook.Playbook{}, playbook.Definition{}, err
	}

	pb, def, err := s.repo.Create(ctx, in)
	if err != nil {
		return playbook.Playbook{}, playbook.Definition{}, err
	}

	s.log.InfoContext(ctx, "playbook created",
		slog.String("playbook_id", pb.ID.String()),
		slog.String("playbook_version_id", def.Version.ID.String()),
		slog.String("alert_type", pb.AlertType))
	return pb, def, nil
}

// List returns playbooks, optionally filtered by alert type.
func (s *Service) List(ctx context.Context, alertType string) ([]playbook.Playbook, error) {
	return s.repo.List(ctx, alertType)
}

// Get returns a playbook with its version summaries.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (PlaybookWithVersions, error) {
	pb, versions, err := s.repo.Get(ctx, id)
	if err != nil {
		return PlaybookWithVersions{}, err
	}
	return PlaybookWithVersions{Playbook: pb, Versions: versions}, nil
}

// CreateVersion adds a new draft version, optionally cloning an existing one.
func (s *Service) CreateVersion(ctx context.Context, playbookID uuid.UUID, cloneFrom *uuid.UUID) (playbook.Definition, error) {
	def, err := s.repo.CreateVersion(ctx, playbookID, cloneFrom)
	if err != nil {
		return playbook.Definition{}, err
	}

	s.log.InfoContext(ctx, "playbook version created",
		slog.String("playbook_id", playbookID.String()),
		slog.String("playbook_version_id", def.Version.ID.String()),
		slog.Int("version", def.Version.Version))
	return def, nil
}

// GetVersion returns a version with its full graph.
func (s *Service) GetVersion(ctx context.Context, versionID uuid.UUID) (playbook.Definition, error) {
	return s.repo.GetVersion(ctx, versionID)
}

// ReplaceGraph replaces a draft version's graph wholesale.
func (s *Service) ReplaceGraph(ctx context.Context, versionID uuid.UUID, g playbook.Graph) (playbook.Definition, error) {
	if err := validateGraphInput(g); err != nil {
		return playbook.Definition{}, err
	}
	return s.repo.ReplaceGraph(ctx, versionID, g)
}

// Publish validates and publishes a draft version.
func (s *Service) Publish(ctx context.Context, versionID uuid.UUID) (playbook.Definition, error) {
	def, err := s.repo.Publish(ctx, versionID)
	if err != nil {
		// Graph rejections are the designer's normal feedback loop, not
		// operational incidents, so they log at info with the problem count
		// rather than as errors.
		if apperror.IsCode(err, apperror.CodeInvalidPlaybookGraph) {
			s.log.InfoContext(ctx, "playbook version failed validation",
				slog.String("playbook_version_id", versionID.String()),
				slog.Int("issues", len(apperror.From(err).Details)))
		}
		return playbook.Definition{}, err
	}

	s.log.InfoContext(ctx, "playbook version published",
		slog.String("playbook_id", def.Version.PlaybookID.String()),
		slog.String("playbook_version_id", def.Version.ID.String()),
		slog.Int("version", def.Version.Version),
		slog.Int("nodes", len(def.Graph.Nodes)),
		slog.Int("edges", len(def.Graph.Edges)))
	return def, nil
}

// validateGraphInput enforces the structural rules that must hold for a draft
// to be *storable*, as opposed to publishable.
//
// The distinction matters: a draft is allowed to be incomplete (no root, no
// nodes, decision nodes without choices yet) because designers save work in
// progress. What it is never allowed to be is incoherent — an unknown node
// kind, an edge with no label, or two nodes sharing an ID would corrupt the
// stored graph rather than merely leave it unfinished. Publish-time rules live
// in playbook.Graph.Validate; see docs/domain-model.md §5.
func validateGraphInput(g playbook.Graph) error {
	seenNodes := make(map[uuid.UUID]bool, len(g.Nodes))
	for i, n := range g.Nodes {
		if n.ID == uuid.Nil {
			return apperror.Validation("nodes[%d].id is required", i)
		}
		if seenNodes[n.ID] {
			return apperror.Validation("nodes[%d].id %s is duplicated", i, n.ID)
		}
		seenNodes[n.ID] = true

		if !n.Kind.Valid() {
			return apperror.Validation("nodes[%d].kind %q must be %q or %q",
				i, n.Kind, playbook.KindDecision, playbook.KindTerminal)
		}
		if n.Title == "" {
			return apperror.Validation("nodes[%d].title is required", i)
		}
	}

	seenEdges := make(map[uuid.UUID]bool, len(g.Edges))
	for i, e := range g.Edges {
		if e.ID == uuid.Nil {
			return apperror.Validation("edges[%d].id is required", i)
		}
		if seenEdges[e.ID] {
			return apperror.Validation("edges[%d].id %s is duplicated", i, e.ID)
		}
		seenEdges[e.ID] = true

		if e.Label == "" {
			return apperror.Validation("edges[%d].label is required", i)
		}
		if e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
			return apperror.Validation("edges[%d] requires both from_node_id and to_node_id", i)
		}
	}
	return nil
}
