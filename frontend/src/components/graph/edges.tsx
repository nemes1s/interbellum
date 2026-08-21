"use client";

import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type EdgeProps } from "@xyflow/react";

export interface GraphEdgeData extends Record<string, unknown> {
  label: string;
  /** Selected during this investigation. */
  isSelected: boolean;
  /** 1-based step this selection was, when selected. */
  stepNumber: number | null;
  /** True when an overlay is active and this edge was not taken. */
  isGhost: boolean;
}

/**
 * A schematic conductor rather than a flowchart arrow: orthogonal segments,
 * zero corner radius, butt caps. A selected edge is drawn heavier and in the
 * signal colour with its step number stamped on the run — an untaken branch
 * drops to a hairline dash.
 */
export function GraphEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  data,
}: EdgeProps & { data?: GraphEdgeData }) {
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 0,
    offset: 14,
  });

  const selected = data?.isSelected ?? false;
  const ghost = data?.isGhost ?? false;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: selected ? "var(--color-signal)" : ghost ? "var(--color-rule)" : "var(--color-rule-strong)",
          strokeWidth: selected ? 2.25 : 1.25,
          strokeDasharray: ghost ? "3 4" : undefined,
        }}
      />
      <EdgeLabelRenderer>
        <div
          style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          className={`pointer-events-none absolute flex items-center gap-1 rounded-[2px] border px-1 py-[2px] font-mono text-[10px] font-medium leading-none ${
            selected
              ? "border-signal bg-signal text-white"
              : ghost
                ? "border-rule bg-panel text-ink-faint"
                : "border-rule-strong bg-panel text-ink-muted"
          }`}
        >
          {selected && data?.stepNumber !== null && data?.stepNumber !== undefined && (
            <span className="rounded-[1px] bg-white/30 px-[3px] py-[1px] tabular-nums">{data.stepNumber}</span>
          )}
          <span className="uppercase tracking-[0.06em]">{data?.label}</span>
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export const graphEdgeTypes = { schematic: GraphEdge };
