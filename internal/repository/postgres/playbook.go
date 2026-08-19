package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/domain/playbook"
)

// PlaybookRepository implements playbook.Repository against PostgreSQL.
type PlaybookRepository struct {
	pool *pgxpool.Pool
}

// NewPlaybookRepository constructs the repository.
func NewPlaybookRepository(pool *pgxpool.Pool) *PlaybookRepository {
	return &PlaybookRepository{pool: pool}
}

var _ playbook.Repository = (*PlaybookRepository)(nil)

// Create inserts the playbook, its first draft version, and the initial graph
// as one unit — a playbook that exists without a version would be a state no
// other code path knows how to handle.
func (r *PlaybookRepository) Create(ctx context.Context, in playbook.NewPlaybook) (playbook.Playbook, playbook.Definition, error) {
	var (
		pb  playbook.Playbook
		def playbook.Definition
	)

	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO playbooks (name, description, alert_type)
			VALUES ($1, $2, $3)
			RETURNING id, name, description, alert_type, created_at, updated_at`,
			in.Name, in.Description, in.AlertType)
		if err := row.Scan(&pb.ID, &pb.Name, &pb.Description, &pb.AlertType, &pb.CreatedAt, &pb.UpdatedAt); err != nil {
			return err
		}

		version, err := insertVersion(ctx, tx, pb.ID, 1)
		if err != nil {
			return err
		}
		if err := writeGraph(ctx, tx, version.ID, in.Graph); err != nil {
			return err
		}

		def, err = loadDefinition(ctx, tx, version.ID)
		return err
	})
	if err != nil {
		return playbook.Playbook{}, playbook.Definition{}, err
	}
	return pb, def, nil
}

// List returns playbooks, newest first, optionally filtered by alert type.
func (r *PlaybookRepository) List(ctx context.Context, alertType string) ([]playbook.Playbook, error) {
	// A single query with an "unset means match everything" predicate keeps
	// this to one code path; the alert_type index still applies when filtering.
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, alert_type, created_at, updated_at
		FROM playbooks
		WHERE ($1 = '' OR alert_type = $1)
		ORDER BY created_at DESC, id`, alertType)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	playbooks := []playbook.Playbook{}
	for rows.Next() {
		var pb playbook.Playbook
		if err := rows.Scan(&pb.ID, &pb.Name, &pb.Description, &pb.AlertType, &pb.CreatedAt, &pb.UpdatedAt); err != nil {
			return nil, translate(err)
		}
		playbooks = append(playbooks, pb)
	}
	return playbooks, translate(rows.Err())
}

// Get returns a playbook with its version summaries, newest version first.
func (r *PlaybookRepository) Get(ctx context.Context, id uuid.UUID) (playbook.Playbook, []playbook.Version, error) {
	var pb playbook.Playbook
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, alert_type, created_at, updated_at
		FROM playbooks WHERE id = $1`, id).
		Scan(&pb.ID, &pb.Name, &pb.Description, &pb.AlertType, &pb.CreatedAt, &pb.UpdatedAt)
	if err != nil {
		return playbook.Playbook{}, nil, notFound(err, "playbook")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, playbook_id, version, status, root_node_id, created_at, published_at
		FROM playbook_versions
		WHERE playbook_id = $1
		ORDER BY version DESC`, id)
	if err != nil {
		return playbook.Playbook{}, nil, translate(err)
	}
	defer rows.Close()

	versions := []playbook.Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return playbook.Playbook{}, nil, translate(err)
		}
		versions = append(versions, v)
	}
	return pb, versions, translate(rows.Err())
}

// CreateVersion adds the next draft version, optionally seeded from an
// existing version's graph.
func (r *PlaybookRepository) CreateVersion(ctx context.Context, playbookID uuid.UUID, cloneFrom *uuid.UUID) (playbook.Definition, error) {
	var def playbook.Definition

	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		// Lock the parent playbook row so two concurrent version creations
		// cannot compute the same next version number. The unique constraint
		// on (playbook_id, version) would catch it anyway, but a lock turns a
		// retryable conflict into a wait.
		var exists bool
		err := tx.QueryRow(ctx, `SELECT true FROM playbooks WHERE id = $1 FOR UPDATE`, playbookID).Scan(&exists)
		if err != nil {
			return notFound(err, "playbook")
		}

		var nextVersion int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(version), 0) + 1 FROM playbook_versions WHERE playbook_id = $1`,
			playbookID).Scan(&nextVersion); err != nil {
			return err
		}

		version, err := insertVersion(ctx, tx, playbookID, nextVersion)
		if err != nil {
			return err
		}

		if cloneFrom != nil {
			source, err := loadDefinition(ctx, tx, *cloneFrom)
			if err != nil {
				return err
			}
			if source.Version.PlaybookID != playbookID {
				return apperror.Validation(
					"clone_from_version_id %s belongs to a different playbook", *cloneFrom)
			}
			// Clone structurally with fresh IDs: the two versions must never
			// share node/edge rows, or editing the draft would mutate the
			// published version's graph.
			if err := writeGraph(ctx, tx, version.ID, cloneGraph(source.Graph)); err != nil {
				return err
			}
		}

		def, err = loadDefinition(ctx, tx, version.ID)
		return err
	})
	if err != nil {
		return playbook.Definition{}, err
	}
	return def, nil
}

// GetVersion returns a version with its full graph.
func (r *PlaybookRepository) GetVersion(ctx context.Context, versionID uuid.UUID) (playbook.Definition, error) {
	return loadDefinition(ctx, r.pool, versionID)
}

// ReplaceGraph replaces a draft version's whole graph.
//
// The delete/insert ordering is forced by the schema's foreign keys: the root
// pointer must be released before its node can be deleted, and edges must go
// before the nodes they reference. See the comment block in
// migrations/000001_init_schema.up.sql.
func (r *PlaybookRepository) ReplaceGraph(ctx context.Context, versionID uuid.UUID, g playbook.Graph) (playbook.Definition, error) {
	var def playbook.Definition

	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		version, err := lockVersion(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if !version.IsDraft() {
			return apperror.Conflict(apperror.CodeVersionNotDraft,
				"playbook version %s is %s; published definitions are immutable — create a new version instead",
				versionID, version.Status)
		}

		if err := clearGraph(ctx, tx, versionID); err != nil {
			return err
		}
		if err := writeGraph(ctx, tx, versionID, g); err != nil {
			return err
		}

		def, err = loadDefinition(ctx, tx, versionID)
		return err
	})
	if err != nil {
		return playbook.Definition{}, err
	}
	return def, nil
}

// Publish validates and publishes a draft version.
//
// Validation and the status flip happen under the same row lock as
// ReplaceGraph takes, so a concurrent edit cannot substitute a different graph
// between "this graph is valid" and "this version is published". That
// ordering requirement is why publishing is a single atomic repository
// operation rather than a load-validate-save sequence in the service layer;
// the rule being applied, playbook.Graph.Validate, is still a pure domain
// function unit-tested without a database.
func (r *PlaybookRepository) Publish(ctx context.Context, versionID uuid.UUID) (playbook.Definition, error) {
	var def playbook.Definition

	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		version, err := lockVersion(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if !version.IsDraft() {
			return apperror.Conflict(apperror.CodeVersionNotDraft,
				"playbook version %s is already %s", versionID, version.Status)
		}

		graph, err := loadGraph(ctx, tx, versionID, version.RootNodeID)
		if err != nil {
			return err
		}
		if issues := graph.Validate(); issues != nil {
			return playbook.ValidationError(issues)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE playbook_versions
			SET status = 'published', published_at = now()
			WHERE id = $1`, versionID); err != nil {
			return err
		}

		def, err = loadDefinition(ctx, tx, versionID)
		return err
	})
	if err != nil {
		return playbook.Definition{}, err
	}
	return def, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// querier is the subset of pgx shared by *pgxpool.Pool and pgx.Tx, so the load
// helpers work both standalone and inside a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// scanner covers pgx.Row and pgx.Rows for the shared version scan.
type scanner interface {
	Scan(dest ...any) error
}

func scanVersion(s scanner) (playbook.Version, error) {
	var v playbook.Version
	err := s.Scan(&v.ID, &v.PlaybookID, &v.Version, &v.Status, &v.RootNodeID, &v.CreatedAt, &v.PublishedAt)
	return v, err
}

func insertVersion(ctx context.Context, tx pgx.Tx, playbookID uuid.UUID, number int) (playbook.Version, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO playbook_versions (playbook_id, version, status)
		VALUES ($1, $2, 'draft')
		RETURNING id, playbook_id, version, status, root_node_id, created_at, published_at`,
		playbookID, number)
	return scanVersion(row)
}

// lockVersion reads a version FOR UPDATE, serializing edits and publishes of
// the same version against each other.
func lockVersion(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) (playbook.Version, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, playbook_id, version, status, root_node_id, created_at, published_at
		FROM playbook_versions
		WHERE id = $1
		FOR UPDATE`, versionID)

	v, err := scanVersion(row)
	if err != nil {
		return playbook.Version{}, notFound(err, "playbook version")
	}
	return v, nil
}

// clearGraph removes a draft's existing graph, releasing the root pointer
// first so the node it references becomes deletable.
func clearGraph(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`UPDATE playbook_versions SET root_node_id = NULL WHERE id = $1`, versionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM playbook_edges WHERE playbook_version_id = $1`, versionID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM playbook_nodes WHERE playbook_version_id = $1`, versionID)
	return err
}

// writeGraph inserts nodes, then edges, then sets the root — the order the
// foreign keys require.
func writeGraph(ctx context.Context, tx pgx.Tx, versionID uuid.UUID, g playbook.Graph) error {
	for _, n := range g.Nodes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO playbook_nodes
				(id, playbook_version_id, kind, title, description, terminal_resolution, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			n.ID, versionID, string(n.Kind), n.Title, n.Description, n.TerminalResolution, n.Metadata,
		); err != nil {
			return err
		}
	}

	for _, e := range g.Edges {
		if _, err := tx.Exec(ctx, `
			INSERT INTO playbook_edges
				(id, playbook_version_id, from_node_id, to_node_id, label, description, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			e.ID, versionID, e.FromNodeID, e.ToNodeID, e.Label, e.Description, e.SortOrder,
		); err != nil {
			return err
		}
	}

	if g.RootNodeID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE playbook_versions SET root_node_id = $2 WHERE id = $1`,
			versionID, *g.RootNodeID); err != nil {
			return err
		}
	}
	return nil
}

func loadDefinition(ctx context.Context, q querier, versionID uuid.UUID) (playbook.Definition, error) {
	row := q.QueryRow(ctx, `
		SELECT id, playbook_id, version, status, root_node_id, created_at, published_at
		FROM playbook_versions WHERE id = $1`, versionID)

	version, err := scanVersion(row)
	if err != nil {
		return playbook.Definition{}, notFound(err, "playbook version")
	}

	graph, err := loadGraph(ctx, q, versionID, version.RootNodeID)
	if err != nil {
		return playbook.Definition{}, err
	}
	return playbook.Definition{Version: version, Graph: graph}, nil
}

func loadGraph(ctx context.Context, q querier, versionID uuid.UUID, rootNodeID *uuid.UUID) (playbook.Graph, error) {
	graph := playbook.Graph{RootNodeID: rootNodeID, Nodes: []playbook.Node{}, Edges: []playbook.Edge{}}

	nodeRows, err := q.Query(ctx, `
		SELECT id, kind, title, description, terminal_resolution, metadata
		FROM playbook_nodes
		WHERE playbook_version_id = $1
		ORDER BY title, id`, versionID)
	if err != nil {
		return playbook.Graph{}, translate(err)
	}
	defer nodeRows.Close()

	for nodeRows.Next() {
		var n playbook.Node
		if err := nodeRows.Scan(&n.ID, &n.Kind, &n.Title, &n.Description, &n.TerminalResolution, &n.Metadata); err != nil {
			return playbook.Graph{}, translate(err)
		}
		graph.Nodes = append(graph.Nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return playbook.Graph{}, translate(err)
	}
	nodeRows.Close()

	edgeRows, err := q.Query(ctx, `
		SELECT id, from_node_id, to_node_id, label, description, sort_order
		FROM playbook_edges
		WHERE playbook_version_id = $1
		ORDER BY sort_order, label, id`, versionID)
	if err != nil {
		return playbook.Graph{}, translate(err)
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var e playbook.Edge
		if err := edgeRows.Scan(&e.ID, &e.FromNodeID, &e.ToNodeID, &e.Label, &e.Description, &e.SortOrder); err != nil {
			return playbook.Graph{}, translate(err)
		}
		graph.Edges = append(graph.Edges, e)
	}
	return graph, translate(edgeRows.Err())
}

// cloneGraph produces a structural copy with fresh node/edge IDs, remapping
// every reference (root and edge endpoints) to the new IDs.
func cloneGraph(src playbook.Graph) playbook.Graph {
	idMap := make(map[uuid.UUID]uuid.UUID, len(src.Nodes))
	out := playbook.Graph{
		Nodes: make([]playbook.Node, 0, len(src.Nodes)),
		Edges: make([]playbook.Edge, 0, len(src.Edges)),
	}

	for _, n := range src.Nodes {
		fresh := uuid.New()
		idMap[n.ID] = fresh
		n.ID = fresh
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range src.Edges {
		e.ID = uuid.New()
		e.FromNodeID = idMap[e.FromNodeID]
		e.ToNodeID = idMap[e.ToNodeID]
		out.Edges = append(out.Edges, e)
	}
	if src.RootNodeID != nil {
		if mapped, ok := idMap[*src.RootNodeID]; ok {
			out.RootNodeID = &mapped
		}
	}
	return out
}
