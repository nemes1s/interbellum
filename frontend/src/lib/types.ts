/**
 * Typed mirror of `api/openapi.yaml`. Hand-written rather than generated so it
 * stays small and readable, but it invents nothing: every field below appears
 * in the published contract, with the same name, and nullable fields are
 * nullable here too.
 *
 * When the API contract changes, this file changes. It never leads.
 */

export type UUID = string;

/** JSON that the engine treats as opaque: alert payloads, node metadata, evidence data. */
export type JsonValue =
  | string
  | number
  | boolean
  | null
  | JsonValue[]
  | { [key: string]: JsonValue };

export type JsonObject = { [key: string]: JsonValue };

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/** Stable error codes from the backend. Clients branch on these, never on message text. */
export type ErrorCode =
  | "BAD_REQUEST"
  | "VALIDATION_FAILED"
  | "RESOURCE_NOT_FOUND"
  | "CONFLICT"
  | "PLAYBOOK_VERSION_NOT_DRAFT"
  | "PLAYBOOK_VERSION_NOT_PUBLISHED"
  | "INVALID_PLAYBOOK_GRAPH"
  | "INVESTIGATION_ALREADY_COMPLETED"
  | "INVALID_TRANSITION"
  | "IDEMPOTENCY_KEY_REUSED"
  | "ALERT_TYPE_MISMATCH"
  | "METHOD_NOT_ALLOWED"
  | "PAYLOAD_TOO_LARGE"
  | "NOT_READY"
  | "INTERNAL_ERROR";

/** The `reason` vocabulary of a publish-time graph validation issue. */
export type GraphValidationReason =
  | "missing_root"
  | "dangling_reference"
  | "unreachable_from_root"
  | "cycle_detected"
  | "decision_node_without_edges"
  | "terminal_node_with_edges"
  | "terminal_node_missing_resolution"
  | "decision_node_with_resolution";

export interface GraphValidationIssue {
  node_id?: UUID;
  edge_id?: UUID;
  reason: GraphValidationReason;
}

/** The error envelope every failing endpoint returns. */
export interface ApiErrorBody {
  code: ErrorCode | string;
  message: string;
  details?: (GraphValidationIssue | JsonObject)[];
}

// ---------------------------------------------------------------------------
// Playbook design
// ---------------------------------------------------------------------------

export type PlaybookVersionStatus = "draft" | "published" | "archived";
export type PlaybookNodeKind = "decision" | "terminal";

export interface Playbook {
  id: UUID;
  name: string;
  description: string;
  alert_type: string;
  created_at: string;
  updated_at: string;
}

export interface PlaybookVersionSummary {
  id: UUID;
  version: number;
  status: PlaybookVersionStatus;
  created_at: string;
  published_at: string | null;
}

export interface PlaybookWithVersions extends Playbook {
  versions: PlaybookVersionSummary[];
}

export interface PlaybookList {
  items: Playbook[];
}

export interface PlaybookNode {
  id: UUID;
  kind: PlaybookNodeKind;
  title: string;
  description: string;
  /** Set iff `kind === "terminal"`. */
  terminal_resolution: string | null;
  /** Designer hints, opaque to the engine. */
  metadata?: JsonObject | null;
}

export interface PlaybookEdge {
  id: UUID;
  from_node_id: UUID;
  to_node_id: UUID;
  label: string;
  description: string | null;
  sort_order: number;
}

export interface PlaybookGraphInput {
  root_node_id: UUID | null;
  nodes: PlaybookNode[];
  edges: PlaybookEdge[];
}

export interface PlaybookVersionDefinition extends PlaybookGraphInput {
  id: UUID;
  playbook_id: UUID;
  version: number;
  status: PlaybookVersionStatus;
  created_at: string;
  published_at: string | null;
}

export interface CreatePlaybookRequest {
  name: string;
  description?: string;
  alert_type: string;
  definition?: PlaybookGraphInput | null;
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

export interface Alert {
  id: UUID;
  external_id: string | null;
  alert_type: string;
  title: string;
  description: string;
  payload: JsonObject;
  occurred_at: string;
  created_at: string;
}

export interface CreateAlertRequest {
  external_id?: string | null;
  alert_type: string;
  title: string;
  description?: string;
  occurred_at: string;
  payload?: JsonObject;
}

// ---------------------------------------------------------------------------
// Investigations
// ---------------------------------------------------------------------------

export type InvestigationStatus = "in_progress" | "completed";
export type ActorType = "human" | "agent";

export interface Actor {
  type: ActorType;
  id: string | null;
}

export interface EvidenceItem {
  type: string;
  summary: string;
  data?: JsonObject;
}

export interface AvailableChoice {
  edge_id: UUID;
  label: string;
  description: string | null;
  to_node_id: UUID;
}

export interface InvestigationStep {
  id: UUID;
  sequence_number: number;
  node_id: UUID;
  selected_edge_id: UUID;
  actor: Actor;
  rationale: string | null;
  evidence: EvidenceItem[];
  created_at: string;
}

export interface InvestigationState {
  id: UUID;
  alert: Alert;
  playbook_version_id: UUID;
  status: InvestigationStatus;
  current_node: PlaybookNode;
  available_choices: AvailableChoice[];
  final_resolution: string | null;
  started_at: string;
  completed_at: string | null;
  steps: InvestigationStep[];
}

export interface StartInvestigationRequest {
  playbook_version_id: UUID;
}

export interface SubmitDecisionRequest {
  edge_id: UUID;
  actor: Actor;
  rationale?: string | null;
  evidence?: EvidenceItem[];
}

/** One entry of the ordered, investigation-specific path — distinct from the canonical graph. */
export interface PathStep {
  step_number: number;
  node_id: UUID;
  selected_edge_id: UUID;
  actor: Actor;
  rationale: string | null;
  evidence: EvidenceItem[];
  created_at: string;
}

export interface InvestigationReport {
  investigation: {
    id: UUID;
    status: InvestigationStatus;
    final_resolution: string | null;
    started_at: string;
    completed_at: string | null;
  };
  alert: Alert;
  /** The canonical, immutable graph. Never annotated per investigation. */
  playbook_version: PlaybookVersionDefinition;
  /** The append-only route taken through that graph. */
  path: PathStep[];
}
