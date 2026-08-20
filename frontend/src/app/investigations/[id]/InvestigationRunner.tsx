"use client";

import Link from "next/link";
import { useCallback, useMemo, useRef } from "react";

import { DecisionForm } from "@/components/DecisionForm";
import { ErrorNotice } from "@/components/ErrorNotice";
import { EvidenceList } from "@/components/EvidenceList";
import { PlaybookGraph } from "@/components/graph/PlaybookGraph";
import { InvestigationOutcome } from "@/components/InvestigationOutcome";
import { Chip, Field, Id, JsonBlock, Loading, Panel } from "@/components/primitives";
import { InvestigationStatusChip } from "@/components/StatusChips";
import * as api from "@/lib/api";
import { timestamp } from "@/lib/format";
import { buildOverlayFromSteps } from "@/lib/report-path";
import { useResource } from "@/lib/use-resource";
import type { InvestigationState } from "@/lib/types";

/**
 * The investigation runner.
 *
 * State comes from `GET /investigations/{id}` — one response carrying the
 * alert, the current question, the choices and the history, which is the same
 * response an automated agent reads before deciding. The investigation id is in
 * the URL, so the page is refreshable and shareable; nothing correctness-bearing
 * is held in the browser.
 */
export function InvestigationRunner({ investigationId }: { investigationId: string }) {
  const investigation = useResource((signal) => api.getInvestigation(investigationId, signal), [investigationId]);

  // Fetched only so the runner can draw the graph with progress on it. The
  // decision loop itself needs nothing from here.
  const version = useResource(
    (signal) => api.getPlaybookVersion(investigation.data!.playbook_version_id, signal),
    [investigation.data?.playbook_version_id],
    { enabled: Boolean(investigation.data?.playbook_version_id) },
  );

  const state = investigation.data;

  // Recording a decision replaces the question in place, and the analyst is
  // usually scrolled down into the evidence form when it happens. Bring the new
  // question back into view rather than leaving them looking at a blank form.
  const questionRef = useRef<HTMLDivElement>(null);
  const applyDecision = useCallback(
    (next: InvestigationState) => {
      investigation.set(next);
      questionRef.current?.scrollIntoView({ block: "start", behavior: "smooth" });
    },
    [investigation],
  );

  const overlay = useMemo(() => {
    if (!state || !version.data) return undefined;
    return buildOverlayFromSteps(state.steps, version.data.edges, state.current_node.id);
  }, [state, version.data]);

  if (investigation.loading && !state) return <Loading what="the investigation" />;
  if (investigation.error && !state) {
    return <ErrorNotice error={investigation.error} onRetry={investigation.reload} />;
  }
  if (!state) return null;

  const completed = state.status === "completed";
  const terminalNode = completed ? state.current_node : null;

  return (
    <div className="space-y-4">
      <Header state={state} />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        {/* ---------------- primary column: the question ------------------ */}
        <div ref={questionRef} className="min-w-0 space-y-4 scroll-mt-16">
          {completed ? (
            <InvestigationOutcome
              investigationId={state.id}
              finalResolution={state.final_resolution}
              terminalNode={terminalNode}
              startedAt={state.started_at}
              completedAt={state.completed_at}
            />
          ) : (
            <Panel
              eyebrow={`Current decision node · ${state.current_node.id.slice(0, 8)}…`}
              title={null}
              bodyClassName="p-4"
            >
              <h2 className="text-[18px] font-semibold leading-snug text-ink sm:text-[20px]">
                {state.current_node.title}
              </h2>
              {state.current_node.description && (
                <p className="mt-1.5 max-w-2xl text-[13.5px] leading-relaxed text-ink-muted">
                  {state.current_node.description}
                </p>
              )}
              {state.current_node.metadata && Object.keys(state.current_node.metadata).length > 0 && (
                <details className="mt-2.5">
                  <summary className="label cursor-pointer select-none hover:text-ink-muted">
                    Designer hints for this node
                  </summary>
                  <JsonBlock value={state.current_node.metadata} maxHeight="10rem" className="mt-1.5" />
                </details>
              )}

              <div className="mt-4 border-t border-rule pt-4">
                <DecisionForm
                  investigationId={state.id}
                  currentNode={state.current_node}
                  choices={state.available_choices}
                  onApplied={applyDecision}
                />
              </div>
            </Panel>
          )}

          <History state={state} />
        </div>

        {/* ---------------- context column -------------------------------- */}
        <div className="min-w-0 space-y-4">
          <Panel eyebrow="Alert under investigation" title={state.alert.title}>
            <dl className="grid grid-cols-2 gap-x-3 gap-y-3">
              <Field label="Type" className="col-span-2">
                <Chip tone="neutral">{state.alert.alert_type}</Chip>
              </Field>
              <Field label="External id">
                <span className="font-mono text-[11.5px]">{state.alert.external_id ?? "—"}</span>
              </Field>
              <Field label="Occurred at">
                <span className="font-mono text-[11.5px]">{timestamp(state.alert.occurred_at)}</span>
              </Field>
              {state.alert.description && (
                <Field label="Description" className="col-span-2">
                  {state.alert.description}
                </Field>
              )}
            </dl>
            <div className="mt-3">
              <p className="label mb-1">Payload</p>
              <JsonBlock value={state.alert.payload} maxHeight="16rem" />
            </div>
          </Panel>

          <Panel
            eyebrow="Playbook version"
            title={version.data ? `Version ${version.data.version}` : "Loading…"}
            bodyClassName=""
          >
            {version.error ? <ErrorNotice error={version.error} onRetry={version.reload} className="m-3" /> : null}
            {version.data && (
              <>
                <PlaybookGraph
                  className="h-[360px] w-full"
                  nodes={version.data.nodes}
                  edges={version.data.edges}
                  rootNodeId={version.data.root_node_id}
                  overlay={overlay}
                />
                <p className="border-t border-rule px-3 py-2 text-[11.5px] leading-snug text-ink-faint">
                  The immutable graph, with the route taken so far drawn on it. The console derives the
                  highlight from the recorded steps and changes nothing about the graph.
                </p>
              </>
            )}
          </Panel>
        </div>
      </div>
    </div>
  );
}

function Header({ state }: { state: InvestigationState }) {
  return (
    <div className="panel px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="min-w-0">
          <p className="label mb-0.5">Investigation</p>
          <p className="break-all font-mono text-[13px] text-ink">{state.id}</p>
        </div>
        <InvestigationStatusChip status={state.status} />
        {state.status === "completed" && (
          <Link
            href={`/investigations/${state.id}/report`}
            className="ml-auto text-[12.5px] text-signal underline decoration-signal/40 underline-offset-2 hover:decoration-signal"
          >
            Open the audit report →
          </Link>
        )}
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-rule pt-3 sm:grid-cols-4">
        <Field label="Started">
          <span className="font-mono text-[11.5px]">{timestamp(state.started_at)}</span>
        </Field>
        <Field label="Completed">
          <span className="font-mono text-[11.5px]">{timestamp(state.completed_at)}</span>
        </Field>
        <Field label="Decisions recorded">{state.steps.length}</Field>
        <Field label="Playbook version">
          <Id value={state.playbook_version_id} />
        </Field>
      </dl>
    </div>
  );
}

/**
 * Decisions already recorded, newest last.
 *
 * This list is the append-only trail: there is no route that edits or deletes a
 * step, so there is nothing here to edit or delete either.
 */
function History({ state }: { state: InvestigationState }) {
  if (state.steps.length === 0) {
    return (
      <Panel eyebrow="History" title="No decisions yet">
        <p className="text-[13px] leading-relaxed text-ink-muted">
          Every decision recorded here is appended, never rewritten. The first one will appear as soon
          as you record a choice above.
        </p>
      </Panel>
    );
  }

  return (
    <Panel
      eyebrow="Append-only history"
      title={`${state.steps.length} decision${state.steps.length === 1 ? "" : "s"} recorded`}
      bodyClassName=""
    >
      <ol className="divide-y divide-rule">
        {state.steps.map((step) => (
          <li key={step.id} className="grid gap-x-4 px-4 py-3 sm:grid-cols-[44px_minmax(0,1fr)]">
            <span className="font-mono text-[13px] tabular-nums text-ink-faint">
              {String(step.sequence_number).padStart(2, "0")}
            </span>
            <div className="min-w-0 space-y-2">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <Chip tone="signal">{step.actor.type}</Chip>
                <span className="font-mono text-[11.5px] text-ink-muted">{step.actor.id ?? "unattributed"}</span>
                <span className="ml-auto font-mono text-[10.5px] text-ink-faint">
                  {timestamp(step.created_at)}
                </span>
              </div>
              {step.rationale && <p className="text-[13px] leading-relaxed text-ink">{step.rationale}</p>}
              <EvidenceList evidence={step.evidence} />
              <p className="font-mono text-[10px] text-ink-faint">
                node {step.node_id.slice(0, 8)}… · edge {step.selected_edge_id.slice(0, 8)}…
              </p>
            </div>
          </li>
        ))}
      </ol>
    </Panel>
  );
}
