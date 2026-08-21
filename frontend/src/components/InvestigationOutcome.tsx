import Link from "next/link";

import { resolutionTone } from "@/components/StatusChips";
import { duration, humanizeToken, timestamp } from "@/lib/format";
import type { PlaybookNode } from "@/lib/types";

import { Chip } from "./primitives";

/**
 * The end of an investigation.
 *
 * A resolution is the whole point of the run, so it is set larger than anything
 * else on the screen and carries the raw `terminal_resolution` token verbatim —
 * that string is what downstream systems key on, and paraphrasing it in the one
 * place it is displayed would be a small lie.
 */
export function InvestigationOutcome({
  investigationId,
  finalResolution,
  terminalNode,
  startedAt,
  completedAt,
  showReportLink = true,
}: {
  investigationId: string;
  finalResolution: string | null;
  terminalNode: PlaybookNode | null;
  startedAt: string;
  completedAt: string | null;
  showReportLink?: boolean;
}) {
  const tone = resolutionTone(finalResolution);
  const elapsed = completedAt ? duration(startedAt, completedAt) : null;

  return (
    <div className="rounded-[3px] border border-rule bg-panel">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-rule px-4 py-2.5">
        <span className="label">Terminal resolution reached</span>
        <Chip tone={tone}>completed</Chip>
        {elapsed && <span className="ml-auto font-mono text-[10.5px] text-ink-faint">{elapsed} elapsed</span>}
      </div>

      <div className="px-4 py-4">
        <p className="font-mono text-[20px] font-medium leading-tight tracking-[-0.01em] text-ink sm:text-[24px]">
          {finalResolution ?? "—"}
        </p>
        {finalResolution && (
          <p className="mt-1 text-[14px] text-ink-muted">{humanizeToken(finalResolution)}</p>
        )}

        {terminalNode && (
          <div className="mt-3 border-t border-rule pt-3">
            <p className="label mb-1">Outcome node</p>
            <p className="text-[13px] font-medium text-ink">{terminalNode.title}</p>
            {terminalNode.description && (
              <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-ink-muted">
                {terminalNode.description}
              </p>
            )}
          </div>
        )}

        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-rule pt-3">
          <span className="font-mono text-[10.5px] text-ink-faint">
            completed {timestamp(completedAt)}
          </span>
          {showReportLink && (
            <Link
              href={`/investigations/${investigationId}/report`}
              className="ml-auto inline-flex items-center rounded-[2px] border border-signal bg-signal px-3 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-[#1a44bb]"
            >
              View investigation report
            </Link>
          )}
        </div>
      </div>
    </div>
  );
}
