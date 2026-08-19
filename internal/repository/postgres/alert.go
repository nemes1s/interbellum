package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indurex/interbellum/internal/domain/alert"
)

// AlertRepository implements alert.Repository against PostgreSQL.
type AlertRepository struct {
	pool *pgxpool.Pool
}

// NewAlertRepository constructs the repository.
func NewAlertRepository(pool *pgxpool.Pool) *AlertRepository {
	return &AlertRepository{pool: pool}
}

var _ alert.Repository = (*AlertRepository)(nil)

const alertColumns = `id, external_id, alert_type, title, description, payload, occurred_at, created_at`

// Create ingests an alert idempotently when external_id is supplied.
//
// ON CONFLICT DO NOTHING plus a follow-up read is used rather than DO UPDATE:
// the first write wins, so a retry carrying drifted field values returns what
// was originally ingested instead of silently rewriting history that an
// investigation may already reference. The `created` flag lets the handler
// answer 201 vs 200 without a second round trip in the common path.
func (r *AlertRepository) Create(ctx context.Context, in alert.New) (alert.Alert, bool, error) {
	payload := in.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO alerts (external_id, alert_type, title, description, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (external_id) WHERE external_id IS NOT NULL DO NOTHING
		RETURNING `+alertColumns,
		in.ExternalID, in.AlertType, in.Title, in.Description, payload, in.OccurredAt)

	a, err := scanAlert(row)
	if err == nil {
		return a, true, nil
	}

	// No row returned means the insert hit the external_id conflict; the
	// existing alert is the correct response.
	if in.ExternalID != nil && isNoRows(err) {
		existing, getErr := r.getByExternalID(ctx, *in.ExternalID)
		if getErr != nil {
			return alert.Alert{}, false, getErr
		}
		return existing, false, nil
	}
	return alert.Alert{}, false, translate(err)
}

// Get returns an alert by ID.
func (r *AlertRepository) Get(ctx context.Context, id uuid.UUID) (alert.Alert, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alerts WHERE id = $1`, id)
	a, err := scanAlert(row)
	if err != nil {
		return alert.Alert{}, notFound(err, "alert")
	}
	return a, nil
}

func (r *AlertRepository) getByExternalID(ctx context.Context, externalID string) (alert.Alert, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alerts WHERE external_id = $1`, externalID)
	a, err := scanAlert(row)
	if err != nil {
		return alert.Alert{}, notFound(err, "alert")
	}
	return a, nil
}

func scanAlert(s scanner) (alert.Alert, error) {
	var a alert.Alert
	err := s.Scan(&a.ID, &a.ExternalID, &a.AlertType, &a.Title, &a.Description,
		&a.Payload, &a.OccurredAt, &a.CreatedAt)
	return a, err
}
