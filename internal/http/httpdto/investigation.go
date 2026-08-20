package httpdto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nemes1s/interbellum/internal/apperror"
	"github.com/nemes1s/interbellum/internal/domain/alert"
	"github.com/nemes1s/interbellum/internal/domain/investigation"
	"github.com/nemes1s/interbellum/internal/service/investigationservice"
)

// Alert mirrors the Alert schema.
type Alert struct {
	ID          uuid.UUID       `json:"id"`
	ExternalID  *string         `json:"external_id"`
	AlertType   string          `json:"alert_type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Payload     json.RawMessage `json:"payload"`
	OccurredAt  time.Time       `json:"occurred_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

// CreateAlertRequest mirrors the CreateAlertRequest schema.
//
// Payload decodes into a map rather than json.RawMessage so that the object
// shape the OpenAPI document promises is enforced by the decoder itself: a
// scalar or an array fails with a 400 instead of being stored and later
// returned from an endpoint whose schema says "object". Nothing is lost by
// decoding through a map — JSONB normalizes key order and whitespace anyway,
// so the bytes were never preserved verbatim to begin with.
type CreateAlertRequest struct {
	ExternalID  *string                    `json:"external_id"`
	AlertType   string                     `json:"alert_type"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	OccurredAt  time.Time                  `json:"occurred_at"`
	Payload     map[string]json.RawMessage `json:"payload"`
}

// EvidenceItem mirrors the EvidenceItem schema. `type` and `summary` are
// required by the contract and validated in the domain; `data` is an arbitrary
// JSON object the engine never interprets.
type EvidenceItem struct {
	Type    string                     `json:"type"`
	Summary string                     `json:"summary"`
	Data    map[string]json.RawMessage `json:"data,omitempty"`
}

// Actor mirrors the Actor schema.
type Actor struct {
	Type string  `json:"type"`
	ID   *string `json:"id"`
}

// AvailableChoice mirrors the AvailableChoice schema: an edge the caller may
// select right now. Deliberately a projection of an edge rather than the edge
// itself — a client choosing its next action should not have to reason about
// which edges in the full graph happen to start here.
type AvailableChoice struct {
	EdgeID      uuid.UUID `json:"edge_id"`
	Label       string    `json:"label"`
	Description *string   `json:"description"`
	ToNodeID    uuid.UUID `json:"to_node_id"`
}

// Step mirrors the InvestigationStep schema.
type Step struct {
	ID             uuid.UUID      `json:"id"`
	SequenceNumber int            `json:"sequence_number"`
	NodeID         uuid.UUID      `json:"node_id"`
	SelectedEdgeID uuid.UUID      `json:"selected_edge_id"`
	Actor          Actor          `json:"actor"`
	Rationale      *string        `json:"rationale"`
	Evidence       []EvidenceItem `json:"evidence"`
	CreatedAt      time.Time      `json:"created_at"`
}

// InvestigationState mirrors the InvestigationState schema.
type InvestigationState struct {
	ID                uuid.UUID         `json:"id"`
	Alert             Alert             `json:"alert"`
	PlaybookVersionID uuid.UUID         `json:"playbook_version_id"`
	Status            string            `json:"status"`
	CurrentNode       Node              `json:"current_node"`
	AvailableChoices  []AvailableChoice `json:"available_choices"`
	FinalResolution   *string           `json:"final_resolution"`
	StartedAt         time.Time         `json:"started_at"`
	CompletedAt       *time.Time        `json:"completed_at"`
	Steps             []Step            `json:"steps"`
}

// StartInvestigationRequest mirrors the StartInvestigationRequest schema.
type StartInvestigationRequest struct {
	PlaybookVersionID uuid.UUID `json:"playbook_version_id"`
}

// SubmitDecisionRequest mirrors the SubmitDecisionRequest schema. Note the
// absence of any "next node" field: the server derives the destination from
// the selected edge, so a client cannot steer the investigation off-graph.
type SubmitDecisionRequest struct {
	EdgeID    uuid.UUID      `json:"edge_id"`
	Actor     Actor          `json:"actor"`
	Rationale *string        `json:"rationale"`
	Evidence  []EvidenceItem `json:"evidence"`
}

// PathStep mirrors the PathStep schema: one entry of the investigation-specific
// path, kept separate from the canonical graph.
type PathStep struct {
	StepNumber     int            `json:"step_number"`
	NodeID         uuid.UUID      `json:"node_id"`
	SelectedEdgeID uuid.UUID      `json:"selected_edge_id"`
	Actor          Actor          `json:"actor"`
	Rationale      *string        `json:"rationale"`
	Evidence       []EvidenceItem `json:"evidence"`
	CreatedAt      time.Time      `json:"created_at"`
}

// ReportInvestigation is the investigation summary embedded in a report.
type ReportInvestigation struct {
	ID              uuid.UUID  `json:"id"`
	Status          string     `json:"status"`
	FinalResolution *string    `json:"final_resolution"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

// InvestigationReport mirrors the InvestigationReport schema.
//
// The canonical graph (playbook_version) and the path taken are separate
// top-level fields rather than the graph carrying was_visited/was_selected
// flags — see investigationservice.Report for the reasoning.
type InvestigationReport struct {
	Investigation   ReportInvestigation       `json:"investigation"`
	Alert           Alert                     `json:"alert"`
	PlaybookVersion PlaybookVersionDefinition `json:"playbook_version"`
	Path            []PathStep                `json:"path"`
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

// ToAlert maps a domain alert to its wire form.
func ToAlert(a alert.Alert) Alert {
	payload := json.RawMessage(a.Payload)
	if len(payload) == 0 || string(payload) == "null" {
		payload = json.RawMessage(`{}`)
	}
	return Alert{
		ID:          a.ID,
		ExternalID:  a.ExternalID,
		AlertType:   a.AlertType,
		Title:       a.Title,
		Description: a.Description,
		Payload:     payload,
		OccurredAt:  a.OccurredAt,
		CreatedAt:   a.CreatedAt,
	}
}

// ToNewAlert maps an ingestion request to the domain input. The payload is
// re-marshalled from the decoded map; an absent or empty payload becomes nil,
// which the repository stores as the column default `{}`.
func (r CreateAlertRequest) ToNewAlert() (alert.New, error) {
	payload, err := marshalObject(r.Payload, "payload")
	if err != nil {
		return alert.New{}, err
	}
	return alert.New{
		ExternalID:  r.ExternalID,
		AlertType:   r.AlertType,
		Title:       r.Title,
		Description: r.Description,
		Payload:     payload,
		OccurredAt:  r.OccurredAt,
	}, nil
}

// marshalObject renders a decoded JSON object back to bytes, or nil when it is
// absent or empty.
func marshalObject(obj map[string]json.RawMessage, field string) ([]byte, error) {
	if len(obj) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, apperror.BadRequest("%s is not valid JSON", field)
	}
	return encoded, nil
}

func toEvidenceItems(items []EvidenceItem) ([]investigation.EvidenceItem, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]investigation.EvidenceItem, 0, len(items))
	for i, item := range items {
		data, err := marshalObject(item.Data, fmt.Sprintf("evidence[%d].data", i))
		if err != nil {
			return nil, err
		}
		out = append(out, investigation.EvidenceItem{
			Type:    item.Type,
			Summary: item.Summary,
			Data:    data,
		})
	}
	return out, nil
}

// fromEvidenceItems maps stored evidence back to the wire form. Absent
// evidence renders as `[]` rather than `null`, so clients can iterate the
// field unconditionally.
func fromEvidenceItems(items []investigation.EvidenceItem) []EvidenceItem {
	out := make([]EvidenceItem, 0, len(items))
	for _, item := range items {
		wire := EvidenceItem{Type: item.Type, Summary: item.Summary}
		if len(item.Data) > 0 && string(item.Data) != "null" {
			// Stored data was validated as an object on the way in.
			_ = json.Unmarshal(item.Data, &wire.Data)
		}
		out = append(out, wire)
	}
	return out
}

func toActor(a investigation.Actor) Actor {
	return Actor{Type: string(a.Type), ID: a.ID}
}

// normalizeJSON collapses an absent field and an explicit JSON `null` to the
// same thing: nothing. Without this, `"evidence": null` would be stored as a
// JSONB null rather than a SQL NULL, which reads back as `null` instead of
// `[]` and — worse — would make an idempotency retry compare unequal to an
// otherwise identical original request.
func normalizeJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}

// ToDecisionInput maps a decision request plus its idempotency key to the
// domain input. The key comes from a header rather than the body so that a
// retry is byte-identical to the original request.
func (r SubmitDecisionRequest) ToDecisionInput(idempotencyKey *string) (investigation.DecisionInput, error) {
	evidence, err := toEvidenceItems(r.Evidence)
	if err != nil {
		return investigation.DecisionInput{}, err
	}
	return investigation.DecisionInput{
		EdgeID: r.EdgeID,
		Actor: investigation.Actor{
			Type: investigation.ActorType(r.Actor.Type),
			ID:   r.Actor.ID,
		},
		Rationale:      r.Rationale,
		Evidence:       evidence,
		IdempotencyKey: idempotencyKey,
	}, nil
}

func toSteps(steps []investigation.Step) []Step {
	out := make([]Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, Step{
			ID:             s.ID,
			SequenceNumber: s.SequenceNumber,
			NodeID:         s.NodeID,
			SelectedEdgeID: s.SelectedEdgeID,
			Actor:          toActor(s.Actor),
			Rationale:      s.Rationale,
			Evidence:       fromEvidenceItems(s.Evidence),
			CreatedAt:      s.CreatedAt,
		})
	}
	return out
}

// ToInvestigationState maps the service view to the wire form.
func ToInvestigationState(s investigationservice.State) InvestigationState {
	choices := make([]AvailableChoice, 0, len(s.AvailableChoices))
	for _, e := range s.AvailableChoices {
		choices = append(choices, AvailableChoice{
			EdgeID:      e.ID,
			Label:       e.Label,
			Description: e.Description,
			ToNodeID:    e.ToNodeID,
		})
	}

	return InvestigationState{
		ID:                s.Investigation.ID,
		Alert:             ToAlert(s.Alert),
		PlaybookVersionID: s.Investigation.PlaybookVersionID,
		Status:            string(s.Investigation.Status),
		CurrentNode:       ToNode(s.CurrentNode),
		AvailableChoices:  choices,
		FinalResolution:   s.Investigation.FinalResolution,
		StartedAt:         s.Investigation.StartedAt,
		CompletedAt:       s.Investigation.CompletedAt,
		Steps:             toSteps(s.Steps),
	}
}

// ToInvestigationReport maps the service report to the wire form.
func ToInvestigationReport(r investigationservice.Report) InvestigationReport {
	path := make([]PathStep, 0, len(r.Steps))
	for _, s := range r.Steps {
		path = append(path, PathStep{
			StepNumber:     s.SequenceNumber,
			NodeID:         s.NodeID,
			SelectedEdgeID: s.SelectedEdgeID,
			Actor:          toActor(s.Actor),
			Rationale:      s.Rationale,
			Evidence:       fromEvidenceItems(s.Evidence),
			CreatedAt:      s.CreatedAt,
		})
	}

	return InvestigationReport{
		Investigation: ReportInvestigation{
			ID:              r.Investigation.ID,
			Status:          string(r.Investigation.Status),
			FinalResolution: r.Investigation.FinalResolution,
			StartedAt:       r.Investigation.StartedAt,
			CompletedAt:     r.Investigation.CompletedAt,
		},
		Alert:           ToAlert(r.Alert),
		PlaybookVersion: ToDefinition(r.Definition),
		Path:            path,
	}
}
