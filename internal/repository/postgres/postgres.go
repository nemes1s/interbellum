// Package postgres is the only package in the codebase that imports pgx. It
// implements the repository ports defined in internal/domain/* and translates
// database failures into application errors, so that no driver detail ever
// reaches an HTTP client.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nemes1s/interbellum/internal/apperror"
)

// PostgreSQL error codes we handle explicitly. Everything else becomes an
// opaque 500 — see translate.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

// PoolConfig holds the connection-pool tuning knobs exposed as configuration.
type PoolConfig struct {
	DatabaseURL     string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// NewPool opens a pgx connection pool and verifies connectivity before
// returning, so a misconfigured DATABASE_URL fails at startup rather than on
// the first request.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// translate converts a database error into an application error.
//
// Constraint violations are mapped by constraint *name*, not by guessing from
// the message: the schema's constraints encode real domain invariants (see
// docs/domain-model.md §7), and this is where the database gets to reject
// something the service layer failed to catch. Anything unrecognised becomes
// an opaque internal error, which is what keeps raw SQL text off the wire.
func translate(err error) error {
	if err == nil {
		return nil
	}
	// Application errors from inside a transaction function pass through
	// unchanged — they are already classified.
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return apperror.Internal(err)
	}

	switch pgErr.Code {
	case pgUniqueViolation:
		switch pgErr.ConstraintName {
		case "playbook_edges_from_node_id_label_key":
			return apperror.Validation(
				"two choices at the same node share a label; labels must be unique per node").
				WithCause(err)
		case "uq_alerts_external_id":
			return apperror.Conflict(apperror.CodeConflict,
				"an alert with this external_id already exists").WithCause(err)
		case "uq_investigation_steps_idempotency":
			return apperror.Conflict(apperror.CodeIdempotencyKeyReused,
				"this idempotency key was already used for this investigation").WithCause(err)
		case "investigation_steps_investigation_id_sequence_number_key":
			// Two decisions raced past the row lock — should be impossible.
			return apperror.Conflict(apperror.CodeConflict,
				"a concurrent decision was applied to this investigation; retry").WithCause(err)
		case "playbook_nodes_pkey", "playbook_edges_pkey":
			// Client-supplied graph IDs collided with rows that already exist,
			// almost always because a fixture with hard-coded UUIDs was posted
			// twice into the same database.
			return apperror.Validation(
				"a node or edge id in this graph already exists; graph ids are " +
					"client-supplied and must be unique across playbooks").WithCause(err)
		case "playbook_versions_playbook_id_version_key":
			return apperror.Conflict(apperror.CodeConflict,
				"a concurrent request created this playbook version; retry").WithCause(err)
		default:
			return apperror.Conflict(apperror.CodeConflict, "conflicting record already exists").WithCause(err)
		}
	case pgForeignKeyViolation:
		switch pgErr.ConstraintName {
		case "fk_playbook_versions_root_node":
			return apperror.Validation("root_node_id must reference a node in this version").WithCause(err)
		case "fk_playbook_edges_from_node_same_version", "fk_playbook_edges_to_node_same_version":
			return apperror.Validation("every edge must connect two nodes declared in this version").WithCause(err)
		case "investigations_alert_id_fkey":
			return apperror.NotFound("alert").WithCause(err)
		case "investigations_playbook_version_id_fkey":
			return apperror.NotFound("playbook version").WithCause(err)
		default:
			return apperror.Validation("request references a resource that does not exist").WithCause(err)
		}
	case pgCheckViolation:
		switch pgErr.ConstraintName {
		case "chk_playbook_nodes_terminal_resolution":
			return apperror.Validation(
				"terminal_resolution is required for terminal nodes and forbidden for decision nodes").
				WithCause(err)
		default:
			return apperror.Validation("request violates a data integrity rule").WithCause(err)
		}
	default:
		return apperror.Internal(err)
	}
}

// notFound maps pgx.ErrNoRows to a typed 404 and everything else through
// translate. Callers name the resource so the message reads naturally.
func notFound(err error, resource string) error {
	if isNoRows(err) {
		return apperror.NotFound(resource)
	}
	return translate(err)
}

// isNoRows reports whether a query returned nothing. Worth naming because
// "no row" is sometimes a 404 and sometimes an expected outcome (an
// ON CONFLICT DO NOTHING that did nothing).
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// inTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. Every multi-statement operation in this package goes through
// it, which is why no generic Transactor abstraction is needed at the service
// layer (see docs/package-structure.md).
func inTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return translate(err)
	}
	defer func() {
		// Rollback after a successful commit is a no-op, so this is safe as an
		// unconditional cleanup and covers the panic path too.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return translate(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return translate(err)
	}
	return nil
}
