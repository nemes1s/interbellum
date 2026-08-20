"use client";

import Link from "next/link";
import { useMemo } from "react";

import { ErrorNotice } from "@/components/ErrorNotice";
import { EvidenceList } from "@/components/EvidenceList";
import { PlaybookGraph } from "@/components/graph/PlaybookGraph";
import { Chip, Field, Id, JsonBlock, Loading, Panel } from "@/components/primitives";
import { InvestigationStatusChip, resolutionTone } from "@/components/StatusChips";
import * as api from "@/lib/api";
import { duration, humanizeToken, timestamp } from "@/lib/format";
import { buildPathOverlay, indexEdges, indexNodes } from "@/lib/report-path";
import { useResource } from "@/lib/use-resource";
import type { InvestigationReport } from "@/lib/types";

/**
 * The audit report.
 *
 * This page is the backend's central design claim made visible:
 *
 *   canonical immutable graph  +  append-only investigation path  =  auditable report
 *
 * The API returns those first two as separate fields — the graph carries no
 * per-investigation flags — and this screen is the join. The graph drawn is the
 * playbook version's own graph, unmodified; the highlights come from
 * `buildPathOverlay(report.path, …)`.
 *
 * The single exception is a terminal-root investigation, which completes with
 * zero steps and so has no path to read an outcome from; this screen supplies
 * the root explicitly for that case. See `PathOverlayOptions`.
 */
export function InvestigationReportScreen({ investigationId }: { investigationId: string }) {
  const report = useResource((signal) => api.getInvestigationReport(investigationId, signal), [investigationId]);

  const overlay = useMemo(() => {
    if (!report.data) return undefined;
    const { path, playbook_version: version, investigation } = report.data;
    return buildPathOverlay(path, version.edges, {
      // Only meaningful for the zero-step case: an investigation whose playbook
      // root was itself terminal completed at that root without recording a
      // step, so the root is where it ended. Ignored when `path` has entries.
      terminalRootNodeId: investigation.status === "completed" ? version.root_node_id : null,
    });
  }, [report.data]);

  if (report.loading && !report.data) return <Loading what="the report" />;
  if (report.error) return <ErrorNotice error={report.error} onRetry={report.reload} />;
  if (!report.data || !overlay) return null;

  const { investigation, alert, playbook_version: version, path } = report.data;
  const nodes = indexNodes(version);
  const edges = indexEdges(version);
  const elapsed = duration(investigation.started_at, investigation.completed_at);
  const finalNode = overlay.finalNodeId ? nodes.get(overlay.finalNodeId) : undefined;

  return (
    <div className="space-y-4">
      <Summary report={report.data} elapsed={elapsed} />

      <Panel
        eyebrow="GET /api/v1/investigations/{id}/report"
        title={`Playbook version ${version.version} — with the path taken`}
        actions={
          <Link
            href={`/playbooks?playbook=${version.playbook_id}&version=${version.id}`}
            className="text-[12.5px] text-ink-muted underline decoration-rule-strong underline-offset-2 hover:text-ink"
          >
            View the canonical graph on its own
          </Link>
        }
        bodyClassName=""
      >
        <PlaybookGraph
          className="h-[min(64vh,660px)] w-full"
          nodes={version.nodes}
          edges={version.edges}
          rootNodeId={version.root_node_id}
          overlay={overlay}
        />
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-rule px-4 py-2.5">
          <span className="label">Overlay</span>
          <Legend kind="taken" label={`${plural(overlay.selectedEdgeIds.size, "edge")} selected, numbered in order`} />
          <Legend kind="visited" label={`${plural(overlay.visitedNodeIds.length, "node")} on the path`} />
          <Legend kind="final" label="Where it ended" />
          <Legend
            kind="ghost"
            label={`${plural(version.edges.length - overlay.selectedEdgeIds.size, "branch", "branches")} not taken`}
          />
        </div>
      </Panel>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <Panel
          eyebrow="Audit timeline"
          title={
            path.length === 0
              ? "No decisions were recorded"
              : `${path.length} decision${path.length === 1 ? "" : "s"}, in the order they were made`
          }
          bodyClassName=""
        >
          {path.length === 0 ? (
            <p className="px-4 py-6 text-[13px] leading-relaxed text-ink-muted">
              This investigation recorded no steps. That happens when the playbook version&apos;s root
              node is itself terminal — the investigation is created already completed rather than
              inventing a synthetic step to record arrival.
            </p>
          ) : (
            <ol className="divide-y divide-rule">
              {[...path]
                .sort((a, b) => a.step_number - b.step_number)
                .map((step) => {
                  const node = nodes.get(step.node_id);
                  const edge = edges.get(step.selected_edge_id);
                  const destination = edge ? nodes.get(edge.to_node_id) : undefined;
                  return (
                    <li key={step.step_number} className="grid gap-x-4 px-4 py-4 sm:grid-cols-[52px_minmax(0,1fr)]">
                      <div>
                        <span className="inline-flex h-6 min-w-6 items-center justify-center rounded-[2px] bg-signal px-1.5 font-mono text-[12px] font-medium tabular-nums leading-none text-white">
                          {step.step_number}
                        </span>
                      </div>

                      <div className="min-w-0 space-y-2.5">
                        <div>
                          <p className="label mb-0.5">Decision node</p>
                          <p className="text-[14px] font-medium leading-snug text-ink">
                            {node?.title ?? <Id value={step.node_id} />}
                          </p>
                        </div>

                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                          <span className="label">Selected</span>
                          <Chip tone="signal">{edge?.label ?? "edge"}</Chip>
                          {destination && (
                            <span className="text-[12.5px] text-ink-muted">→ {destination.title}</span>
                          )}
                        </div>

                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                          <span className="label">Actor</span>
                          <Chip tone="neutral">{step.actor.type}</Chip>
                          <span className="font-mono text-[11.5px] text-ink-muted">
                            {step.actor.id ?? "unattributed"}
                          </span>
                          <span className="ml-auto font-mono text-[10.5px] text-ink-faint">
                            {timestamp(step.created_at)}
                          </span>
                        </div>

                        <div>
                          <p className="label mb-0.5">Rationale</p>
                          {step.rationale ? (
                            <p className="max-w-3xl text-[13px] leading-relaxed text-ink">{step.rationale}</p>
                          ) : (
                            <p className="text-[12.5px] text-ink-faint">None recorded.</p>
                          )}
                        </div>

                        <div>
                          <p className="label mb-1">Evidence</p>
                          <EvidenceList evidence={step.evidence} />
                        </div>

                        <p className="font-mono text-[10px] text-ink-faint">
                          node {step.node_id} · edge {step.selected_edge_id}
                        </p>
                      </div>
                    </li>
                  );
                })}
            </ol>
          )}
        </Panel>

        <div className="space-y-4">
          {finalNode && (
            <Panel eyebrow="Final node" title={finalNode.title}>
              {finalNode.terminal_resolution && (
                <>
                  <p className="font-mono text-[13px] text-ink">{finalNode.terminal_resolution}</p>
                  <p className="mt-0.5 text-[12.5px] text-ink-muted">
                    {humanizeToken(finalNode.terminal_resolution)}
                  </p>
                </>
              )}
              {finalNode.description && (
                <p className="mt-2 border-t border-rule pt-2 text-[13px] leading-relaxed text-ink-muted">
                  {finalNode.description}
                </p>
              )}
            </Panel>
          )}

          <Panel eyebrow="Alert" title={alert.title}>
            <dl className="grid grid-cols-2 gap-x-3 gap-y-3">
              <Field label="Type" className="col-span-2">
                <Chip tone="neutral">{alert.alert_type}</Chip>
              </Field>
              <Field label="External id">
                <span className="font-mono text-[11.5px]">{alert.external_id ?? "—"}</span>
              </Field>
              <Field label="Occurred at">
                <span className="font-mono text-[11.5px]">{timestamp(alert.occurred_at)}</span>
              </Field>
            </dl>
            <div className="mt-3">
              <p className="label mb-1">Payload</p>
              <JsonBlock value={alert.payload} maxHeight="16rem" />
            </div>
          </Panel>

          <Panel eyebrow="Provenance" title="What this report is bound to">
            <dl className="space-y-3">
              <Field label="Investigation id">
                <span className="break-all font-mono text-[11.5px]">{investigation.id}</span>
              </Field>
              <Field label="Playbook version id">
                <span className="break-all font-mono text-[11.5px]">{version.id}</span>
              </Field>
              <Field label="Version status">
                <Chip tone="clear">{version.status}</Chip>
              </Field>
              <Field label="Published at">
                <span className="font-mono text-[11.5px]">{timestamp(version.published_at)}</span>
              </Field>
            </dl>
            <p className="mt-3 border-t border-rule pt-3 text-[12px] leading-relaxed text-ink-muted">
              Published versions are frozen. This report resolves against exactly the graph the
              investigation ran on, no matter how many times the playbook has been revised since.
            </p>
          </Panel>
        </div>
      </div>
    </div>
  );
}

function Summary({ report, elapsed }: { report: InvestigationReport; elapsed: string | null }) {
  const { investigation, alert } = report;
  const tone = resolutionTone(investigation.final_resolution);

  return (
    <div className="panel">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-rule px-4 py-2.5">
        <p className="label">Investigation report</p>
        <InvestigationStatusChip status={investigation.status} />
        <span className="break-all font-mono text-[11.5px] text-ink-muted">{investigation.id}</span>
        <Link
          href={`/investigations/${investigation.id}`}
          className="ml-auto text-[12.5px] text-ink-muted underline decoration-rule-strong underline-offset-2 hover:text-ink"
        >
          Back to the runner
        </Link>
      </div>

      <div className="grid gap-4 px-4 py-4 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div>
          <h1 className="text-[19px] font-semibold leading-snug text-ink">{alert.title}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-2">
            <Chip tone="neutral">{alert.alert_type}</Chip>
            {alert.external_id && (
              <span className="font-mono text-[11.5px] text-ink-muted">{alert.external_id}</span>
            )}
          </p>
          {alert.description && (
            <p className="mt-2 max-w-2xl text-[13px] leading-relaxed text-ink-muted">{alert.description}</p>
          )}

          <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-rule pt-3 sm:grid-cols-4">
            <Field label="Started at">
              <span className="font-mono text-[11.5px]">{timestamp(investigation.started_at)}</span>
            </Field>
            <Field label="Completed at">
              <span className="font-mono text-[11.5px]">{timestamp(investigation.completed_at)}</span>
            </Field>
            <Field label="Elapsed">{elapsed ?? "—"}</Field>
            <Field label="Steps">{report.path.length}</Field>
          </dl>
        </div>

        <div className={`rounded-[2px] border p-3 ${tone === "neutral" ? "border-rule bg-sunken" : ""}`}
          style={
            tone === "neutral"
              ? undefined
              : {
                  borderColor: `color-mix(in srgb, var(--color-${tone}) 35%, transparent)`,
                  background: `var(--color-${tone}-soft)`,
                }
          }
        >
          <p className="label mb-1.5">Final resolution</p>
          {investigation.final_resolution ? (
            <>
              <p className="break-words font-mono text-[16px] font-medium leading-tight text-ink">
                {investigation.final_resolution}
              </p>
              <p className="mt-1 text-[13px] text-ink-muted">
                {humanizeToken(investigation.final_resolution)}
              </p>
            </>
          ) : (
            <p className="text-[13px] text-ink-muted">
              Still in progress — no terminal node has been reached yet.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

/** "1 node" / "3 nodes" — a terminal-root report legitimately reports one of each. */
function plural(count: number, singular: string, plural = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

const LEGEND = {
  taken: "h-[3px] w-6 bg-signal",
  visited: "h-3.5 w-[3px] bg-signal/55",
  final: "h-3.5 w-3.5 border border-signal bg-signal-soft",
  ghost: "h-[1px] w-6 bg-rule-strong",
} as const;

function Legend({ kind, label }: { kind: keyof typeof LEGEND; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[11.5px] text-ink-muted">
      <span className={LEGEND[kind]} aria-hidden />
      {label}
    </span>
  );
}
