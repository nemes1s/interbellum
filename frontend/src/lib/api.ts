import { ApiError } from "./api-error";
import type {
  Alert,
  ApiErrorBody,
  CreateAlertRequest,
  CreatePlaybookRequest,
  InvestigationReport,
  InvestigationState,
  PlaybookList,
  PlaybookVersionDefinition,
  PlaybookWithVersions,
  StartInvestigationRequest,
  SubmitDecisionRequest,
  UUID,
} from "./types";

/**
 * The browser always talks to this origin. `/api/v1/*` is served by the Next.js
 * route handler in `src/app/api/[...path]/route.ts`, which forwards to
 * `BACKEND_URL` server-side — so no CORS is needed on the Go API and the API's
 * address is never baked into client JavaScript.
 */
const BASE = "/api/v1";

/** A response plus the status code, for the few places the code is meaningful. */
export interface ApiResult<T> {
  data: T;
  status: number;
}

async function request<T>(
  path: string,
  init: RequestInit & { idempotencyKey?: string } = {},
): Promise<ApiResult<T>> {
  const { idempotencyKey, ...rest } = init;
  const headers = new Headers(rest.headers);
  if (rest.body !== undefined) headers.set("Content-Type", "application/json");
  if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);

  let response: Response;
  try {
    response = await fetch(`${BASE}${path}`, { ...rest, headers });
  } catch (cause) {
    // An abort is the caller withdrawing interest, not a failure. Rethrow it
    // unchanged so it is never rendered as an error to the analyst.
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError(0, {
      code: "NETWORK_ERROR",
      message: cause instanceof Error ? cause.message : "the request could not be sent",
    });
  }

  const text = await response.text();
  let body: unknown = null;
  if (text.length > 0) {
    try {
      body = JSON.parse(text);
    } catch {
      // A non-JSON body from a proxy or gateway. Surface it rather than
      // pretending the server said nothing.
      if (!response.ok) {
        throw new ApiError(response.status, {
          code: "BAD_GATEWAY_RESPONSE",
          message: text.slice(0, 300),
        });
      }
    }
  }

  if (!response.ok) {
    throw new ApiError(response.status, (body ?? {
      code: "INTERNAL_ERROR",
      message: `request failed with status ${response.status}`,
    }) as ApiErrorBody);
  }

  return { data: body as T, status: response.status };
}

async function json<T>(path: string, init?: RequestInit & { idempotencyKey?: string }): Promise<T> {
  return (await request<T>(path, init)).data;
}

// ---------------------------------------------------------------------------
// Playbooks
// ---------------------------------------------------------------------------
//
// Reads take an optional AbortSignal so that navigating away actually cancels
// the request in flight, not merely the state update that would follow it.
//
// Writes deliberately do not: a decision, an ingestion or a publish must not be
// cut short because a component unmounted. The server would have applied it
// anyway, and the client would be left not knowing whether it had.

export function listPlaybooks(alertType?: string, signal?: AbortSignal): Promise<PlaybookList> {
  const query = alertType ? `?alert_type=${encodeURIComponent(alertType)}` : "";
  return json<PlaybookList>(`/playbooks${query}`, { signal });
}

export function getPlaybook(playbookId: UUID, signal?: AbortSignal): Promise<PlaybookWithVersions> {
  return json<PlaybookWithVersions>(`/playbooks/${playbookId}`, { signal });
}

export function createPlaybook(body: CreatePlaybookRequest): Promise<PlaybookWithVersions> {
  return json<PlaybookWithVersions>("/playbooks", { method: "POST", body: JSON.stringify(body) });
}

export function getPlaybookVersion(
  versionId: UUID,
  signal?: AbortSignal,
): Promise<PlaybookVersionDefinition> {
  return json<PlaybookVersionDefinition>(`/playbook-versions/${versionId}`, { signal });
}

export function publishPlaybookVersion(versionId: UUID): Promise<PlaybookVersionDefinition> {
  return json<PlaybookVersionDefinition>(`/playbook-versions/${versionId}/publish`, { method: "POST" });
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

/**
 * Ingest an alert. The status is returned alongside the alert because the
 * contract distinguishes 201 (created) from 200 (an alert with this
 * `external_id` already existed and was returned unchanged) — worth showing.
 */
export function createAlert(body: CreateAlertRequest): Promise<ApiResult<Alert>> {
  return request<Alert>("/alerts", { method: "POST", body: JSON.stringify(body) });
}

export function getAlert(alertId: UUID, signal?: AbortSignal): Promise<Alert> {
  return json<Alert>(`/alerts/${alertId}`, { signal });
}

// ---------------------------------------------------------------------------
// Investigations
// ---------------------------------------------------------------------------

export function startInvestigation(
  alertId: UUID,
  body: StartInvestigationRequest,
): Promise<InvestigationState> {
  return json<InvestigationState>(`/alerts/${alertId}/investigations`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function getInvestigation(
  investigationId: UUID,
  signal?: AbortSignal,
): Promise<InvestigationState> {
  return json<InvestigationState>(`/investigations/${investigationId}`, { signal });
}

/**
 * Submit a decision.
 *
 * `idempotencyKey` is supplied by the caller and must identify the *intent*,
 * not the HTTP attempt: a network retry of the same decision reuses the key so
 * the server treats it as a no-op, while a new decision always gets a fresh
 * one. See `useDecisionSubmission`.
 */
export function submitDecision(
  investigationId: UUID,
  body: SubmitDecisionRequest,
  idempotencyKey: string,
): Promise<InvestigationState> {
  return json<InvestigationState>(`/investigations/${investigationId}/decisions`, {
    method: "POST",
    body: JSON.stringify(body),
    idempotencyKey,
  });
}

export function getInvestigationReport(
  investigationId: UUID,
  signal?: AbortSignal,
): Promise<InvestigationReport> {
  return json<InvestigationReport>(`/investigations/${investigationId}/report`, { signal });
}
