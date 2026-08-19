// Package alert holds the Alert entity: a domain-agnostic trigger for an
// investigation. The engine never interprets Payload — that is the point.
// Playbook authors ask questions about it; evidence references it; the
// investigation engine itself stays free of OT-specific concepts.
package alert

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Alert is an ingested security alert.
type Alert struct {
	ID uuid.UUID
	// ExternalID is the upstream system's identifier. When present it is
	// unique, which is what makes ingestion idempotent.
	ExternalID  *string
	AlertType   string
	Title       string
	Description string
	// Payload is opaque alert-specific JSON.
	Payload []byte
	// OccurredAt is when the underlying event happened, which is generally not
	// when we ingested it.
	OccurredAt time.Time
	CreatedAt  time.Time
}

// New is the input for ingesting an alert.
type New struct {
	ExternalID  *string
	AlertType   string
	Title       string
	Description string
	Payload     []byte
	OccurredAt  time.Time
}

// Repository is the persistence port for alerts.
type Repository interface {
	// Create ingests an alert. When ExternalID is set and already exists, the
	// existing alert is returned unchanged with created=false — upstream
	// alerting systems retry after timeouts and cannot tell whether the
	// original request landed. First write wins: a retry carrying different
	// field values does not update the stored alert.
	Create(ctx context.Context, in New) (a Alert, created bool, err error)

	// Get returns an alert by ID, or apperror.CodeNotFound.
	Get(ctx context.Context, id uuid.UUID) (Alert, error)
}
