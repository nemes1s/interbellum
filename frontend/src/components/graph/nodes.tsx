"use client";

import { Handle, Position, type NodeProps } from "@xyflow/react";

import { humanizeToken } from "@/lib/format";
import type { PlaybookNodeKind } from "@/lib/types";

/**
 * Node data the graph builder attaches. Everything except `kind`, `title`,
 * `description` and `resolution` is overlay state — derived from the report
 * path, never from the graph itself.
 */
export interface GraphNodeData extends Record<string, unknown> {
  kind: PlaybookNodeKind;
  title: string;
  description: string;
  resolution: string | null;
  isRoot: boolean;
  /** On the investigation's path. */
  isVisited: boolean;
  /** Where the investigation stands / ended. */
  isFinal: boolean;
  /** 1-based step taken from this node, when it is on the path. */
  stepNumber: number | null;
  /** True when an overlay is active and this node is not on the path. */
  isGhost: boolean;
}

/** Shared frame. The variance between kinds is the left rule and the footer. */
function Frame({
  data,
  accent,
  children,
}: {
  data: GraphNodeData;
  accent: string;
  children?: React.ReactNode;
}) {
  const { isGhost, isVisited, isFinal, stepNumber, isRoot, kind } = data;

  const border = isFinal
    ? "border-signal"
    : isVisited
      ? "border-signal/55"
      : isGhost
        ? "border-rule"
        : "border-rule-strong";

  return (
    <div
      className={`relative w-[268px] border bg-panel ${border} ${isFinal ? "shadow-[0_0_0_3px_var(--color-signal-soft)]" : ""} ${
        isGhost ? "opacity-45" : ""
      }`}
      style={{ borderRadius: 2 }}
    >
      <Handle type="target" position={Position.Top} />

      {/* Kind is carried by a left rule, the way a schematic marks a bus. */}
      <div className="absolute inset-y-0 left-0 w-[3px]" style={{ background: accent }} aria-hidden />

      <div className="py-2 pl-3 pr-2.5">
        <div className="mb-1 flex items-center gap-1.5">
          <span
            className="font-mono text-[9.5px] font-medium uppercase leading-none tracking-[0.1em]"
            style={{ color: accent }}
          >
            {kind}
          </span>
          {isRoot && <span className="label !text-[9.5px]">root</span>}
          {stepNumber !== null && (
            <span className="ml-auto inline-flex h-[15px] min-w-[15px] items-center justify-center rounded-[2px] bg-signal px-1 font-mono text-[9.5px] font-medium leading-none text-white">
              {stepNumber}
            </span>
          )}
          {stepNumber === null && isFinal && (
            <span className="label ml-auto !text-signal">{kind === "terminal" ? "reached" : "current"}</span>
          )}
        </div>

        <p className="text-[12.5px] font-medium leading-[1.35] text-ink">{data.title}</p>
        {children}
      </div>

      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}

export function DecisionNode({ data }: NodeProps & { data: GraphNodeData }) {
  return (
    <Frame data={data} accent="var(--color-ink-muted)">
      {data.description && (
        <p className="mt-1 line-clamp-2 text-[11px] leading-snug text-ink-faint">{data.description}</p>
      )}
    </Frame>
  );
}

export function TerminalNode({ data }: NodeProps & { data: GraphNodeData }) {
  return (
    <Frame data={data} accent="var(--color-ink)">
      <p className="mt-1.5 border-t border-rule pt-1.5 font-mono text-[10.5px] leading-snug text-ink">
        {data.resolution ?? "—"}
      </p>
      {data.resolution && (
        <p className="mt-0.5 text-[10.5px] leading-snug text-ink-faint">{humanizeToken(data.resolution)}</p>
      )}
    </Frame>
  );
}

export const graphNodeTypes = { decision: DecisionNode, terminal: TerminalNode };
