// Package alertservice orchestrates alert ingestion and retrieval.
package alertservice

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/apperror"
	"github.com/indurex/interbellum/internal/domain/alert"
)

// Service exposes the alert use cases to the HTTP layer.
type Service struct {
	repo alert.Repository
	log  *slog.Logger
}

// New constructs the service.
func New(repo alert.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// Create ingests an alert. The boolean reports whether a new alert was
// actually created, which the handler turns into 201 vs 200 — see
// alert.Repository.Create for why re-ingestion is idempotent rather than a
// conflict.
func (s *Service) Create(ctx context.Context, in alert.New) (alert.Alert, bool, error) {
	if in.AlertType == "" {
		return alert.Alert{}, false, apperror.Validation("alert_type is required")
	}
	if in.Title == "" {
		return alert.Alert{}, false, apperror.Validation("title is required")
	}
	if in.OccurredAt.IsZero() {
		return alert.Alert{}, false, apperror.Validation("occurred_at is required")
	}
	if in.ExternalID != nil && *in.ExternalID == "" {
		// An empty string would defeat the unique index (which only covers
		// non-null values) and silently disable idempotency for that caller.
		return alert.Alert{}, false, apperror.Validation("external_id must not be empty when provided")
	}
	if len(in.Payload) > 0 && !json.Valid(in.Payload) {
		return alert.Alert{}, false, apperror.BadRequest("payload must be valid JSON")
	}

	a, created, err := s.repo.Create(ctx, in)
	if err != nil {
		return alert.Alert{}, false, err
	}

	// Payload is deliberately not logged: alert payloads carry operational
	// detail about customer sites (addresses, device identifiers) and belong
	// in the database, not in log aggregation.
	s.log.InfoContext(ctx, "alert ingested",
		slog.String("alert_id", a.ID.String()),
		slog.String("alert_type", a.AlertType),
		slog.Bool("created", created))
	return a, created, nil
}

// Get returns an alert by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (alert.Alert, error) {
	return s.repo.Get(ctx, id)
}
