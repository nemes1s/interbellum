"use client";

import { ApiError, asGraphIssue, explainErrorCode, explainGraphReason, isApiError } from "@/lib/api-error";
import { shortId } from "@/lib/format";

import { Button } from "./primitives";

/**
 * Renders the backend's structured error envelope.
 *
 * Three layers, in the order an analyst needs them:
 *  1. the machine-readable `code`, because that is what the contract promises
 *     and what a bug report should quote;
 *  2. what that code means for the person looking at the screen;
 *  3. the server's own `message`, which carries the specifics (which edge,
 *     which node), plus `details` when the failure is graph validation.
 *
 * Nothing here says "Something went wrong".
 */
export function ErrorNotice({
  error,
  onRetry,
  className = "",
}: {
  error: unknown;
  onRetry?: () => void;
  className?: string;
}) {
  if (!error) return null;

  const api: ApiError | null = isApiError(error) ? error : null;
  const code = api?.code ?? "UNEXPECTED_ERROR";
  const message = api?.detail ?? (error instanceof Error ? error.message : String(error));
  const explanation = explainErrorCode(code);
  const issues = (api?.details ?? []).map(asGraphIssue).filter((issue) => issue !== null);

  return (
    <div
      role="alert"
      className={`rounded-[3px] border border-alarm/40 bg-alarm-soft ${className}`}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-alarm/20 px-3 py-2">
        <span className="font-mono text-[11px] font-medium uppercase tracking-[0.08em] text-alarm">{code}</span>
        {api && api.status > 0 && (
          <span className="font-mono text-[11px] text-alarm/70">HTTP {api.status}</span>
        )}
        {onRetry && (
          <Button variant="quiet" onClick={onRetry} className="ml-auto !text-alarm hover:!bg-alarm/10">
            Try again
          </Button>
        )}
      </div>

      <div className="space-y-2 px-3 py-2.5">
        {explanation && <p className="text-[13px] font-medium leading-snug text-ink">{explanation}</p>}
        <p className="font-mono text-[11.5px] leading-relaxed text-ink-muted">{message}</p>

        {issues.length > 0 && (
          <div className="rounded-[2px] border border-alarm/25 bg-panel">
            <p className="label border-b border-alarm/20 px-2.5 py-1.5 !text-alarm">
              {issues.length} graph problem{issues.length === 1 ? "" : "s"}
            </p>
            <ul className="divide-y divide-rule">
              {issues.map((issue, index) => (
                <li key={index} className="flex flex-wrap items-baseline gap-x-2 px-2.5 py-1.5">
                  <span className="text-[12.5px] text-ink">{explainGraphReason(issue.reason)}</span>
                  {issue.node_id && (
                    <span title={issue.node_id} className="font-mono text-[11px] text-ink-faint">
                      node {shortId(issue.node_id)}
                    </span>
                  )}
                  {issue.edge_id && (
                    <span title={issue.edge_id} className="font-mono text-[11px] text-ink-faint">
                      edge {shortId(issue.edge_id)}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  );
}
