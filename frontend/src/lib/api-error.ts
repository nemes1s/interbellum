import type { ApiErrorBody, GraphValidationIssue, JsonObject } from "./types";

/**
 * A failed API call, carrying the backend's structured envelope.
 *
 * The console branches on `code`, never on `message`. Messages are still shown,
 * because the backend writes good ones ("edge 5b1f… originates at node a000…03,
 * but the investigation is at node a000…01") and an analyst needs that detail.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;
  readonly details: ApiErrorBody["details"];

  constructor(status: number, body: ApiErrorBody) {
    super(body.message || `request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code || "UNKNOWN";
    this.detail = body.message ?? "";
    this.details = body.details;
  }
}

export function isApiError(err: unknown): err is ApiError {
  return err instanceof ApiError;
}

/**
 * A short, human title for an error code. The backend's own `message` is always
 * shown underneath, so this line explains *what kind* of problem it is and what
 * the analyst can do about it — never a restatement of the message.
 */
const CODE_TITLES: Record<string, string> = {
  PLAYBOOK_VERSION_NOT_PUBLISHED:
    "That playbook version is still a draft. Publish it before starting an investigation against it.",
  PLAYBOOK_VERSION_NOT_DRAFT:
    "That playbook version is published, so its graph is frozen. Create a new version to change it.",
  ALERT_TYPE_MISMATCH:
    "This playbook is written for a different alert type. Pick a playbook registered for this alert's type.",
  INVALID_TRANSITION:
    "That choice is no longer available. The investigation has moved on — reload to see the current question.",
  INVESTIGATION_ALREADY_COMPLETED:
    "This investigation reached a terminal resolution and cannot be advanced. Open its report instead.",
  IDEMPOTENCY_KEY_REUSED:
    "This submission reused an idempotency key with different content, so it was rejected rather than recorded. Reload and submit again.",
  INVALID_PLAYBOOK_GRAPH:
    "The playbook graph cannot be published yet. Every problem found is listed below.",
  RESOURCE_NOT_FOUND: "That record does not exist. It may have been created against a different database.",
  VALIDATION_FAILED: "The request was rejected as invalid.",
  BAD_REQUEST: "The request was malformed.",
  PAYLOAD_TOO_LARGE: "The request body exceeded the server's size limit.",
  METHOD_NOT_ALLOWED: "That path exists, but not for this operation.",
  NOT_READY: "The API is not ready to serve traffic — its database is unreachable.",
  INTERNAL_ERROR: "The API failed unexpectedly. The cause is in its logs.",
  NETWORK_ERROR: "The console could not reach the API. Check that it is running.",
};

export function explainErrorCode(code: string): string | null {
  return CODE_TITLES[code] ?? null;
}

/** Narrows a `details` entry to a publish-time graph validation issue. */
export function asGraphIssue(detail: GraphValidationIssue | JsonObject): GraphValidationIssue | null {
  if (detail && typeof detail === "object" && typeof (detail as GraphValidationIssue).reason === "string") {
    return detail as GraphValidationIssue;
  }
  return null;
}

/** Human wording for each publish-time graph validation reason. */
const GRAPH_REASONS: Record<string, string> = {
  missing_root: "No root node is set",
  dangling_reference: "References a node that does not exist in this version",
  unreachable_from_root: "Not reachable from the root node",
  cycle_detected: "Part of a cycle — playbooks must be acyclic",
  decision_node_without_edges: "Decision node offers no choices",
  terminal_node_with_edges: "Terminal node has outgoing choices",
  terminal_node_missing_resolution: "Terminal node has no resolution",
  decision_node_with_resolution: "Decision node carries a terminal resolution",
};

export function explainGraphReason(reason: string): string {
  return GRAPH_REASONS[reason] ?? reason;
}
