import { describe, expect, it } from "vitest";

import { buildOverlayFromSteps, buildPathOverlay } from "@/lib/report-path";
import type { PathStep, PlaybookEdge } from "@/lib/types";

/**
 * Mapping a report's `path` onto the canonical graph is the console's one piece
 * of genuinely load-bearing logic, so it is tested directly rather than through
 * the graph component.
 */

const N = {
  q1: "a0000000-0000-4000-8000-000000000001",
  q2: "a0000000-0000-4000-8000-000000000002",
  q3: "a0000000-0000-4000-8000-000000000003",
  q4: "a0000000-0000-4000-8000-000000000004",
  close: "b0000000-0000-4000-8000-000000000001",
  escalate: "b0000000-0000-4000-8000-000000000003",
} as const;

const E = {
  q1yes: "e0000000-0000-4000-8000-000000000001",
  q1no: "e0000000-0000-4000-8000-000000000002",
  q2yes: "e0000000-0000-4000-8000-000000000003",
  q2no: "e0000000-0000-4000-8000-000000000004",
  q3no: "e0000000-0000-4000-8000-000000000006",
} as const;

const edges: PlaybookEdge[] = [
  { id: E.q1yes, from_node_id: N.q1, to_node_id: N.q2, label: "Yes", description: null, sort_order: 1 },
  { id: E.q1no, from_node_id: N.q1, to_node_id: N.q4, label: "No", description: null, sort_order: 2 },
  { id: E.q2yes, from_node_id: N.q2, to_node_id: N.q3, label: "Yes", description: null, sort_order: 1 },
  { id: E.q2no, from_node_id: N.q2, to_node_id: N.escalate, label: "No", description: null, sort_order: 2 },
  { id: E.q3no, from_node_id: N.q3, to_node_id: N.close, label: "No", description: null, sort_order: 2 },
];

function step(step_number: number, node_id: string, selected_edge_id: string): PathStep {
  return {
    step_number,
    node_id,
    selected_edge_id,
    actor: { type: "agent", id: "investigation-agent-v1" },
    rationale: null,
    evidence: [],
    created_at: "2026-08-19T10:31:00Z",
  };
}

describe("buildPathOverlay", () => {
  const path = [step(1, N.q1, E.q1yes), step(2, N.q2, E.q2yes), step(3, N.q3, E.q3no)];

  it("marks every node the path stood at, plus the node the last edge led to", () => {
    const overlay = buildPathOverlay(path, edges);

    expect(overlay.visitedNodeIds).toEqual([N.q1, N.q2, N.q3, N.close]);
    expect(overlay.visited.has(N.close)).toBe(true);
    // Nodes on branches the investigation did not take stay unvisited.
    expect(overlay.visited.has(N.q4)).toBe(false);
    expect(overlay.visited.has(N.escalate)).toBe(false);
  });

  it("marks exactly the selected edges and leaves every sibling branch untaken", () => {
    const overlay = buildPathOverlay(path, edges);

    expect([...overlay.selectedEdgeIds].sort()).toEqual([E.q1yes, E.q2yes, E.q3no].sort());
    expect(overlay.selectedEdgeIds.has(E.q1no)).toBe(false);
    expect(overlay.selectedEdgeIds.has(E.q2no)).toBe(false);
  });

  it("stamps step numbers on the node a decision was made from and on the edge chosen", () => {
    const overlay = buildPathOverlay(path, edges);

    expect(overlay.stepAtNode.get(N.q1)).toBe(1);
    expect(overlay.stepAtNode.get(N.q2)).toBe(2);
    expect(overlay.stepAtNode.get(N.q3)).toBe(3);
    // The terminal node was arrived at, not decided from — it carries no step.
    expect(overlay.stepAtNode.has(N.close)).toBe(false);

    expect(overlay.stepAtEdge.get(E.q1yes)).toBe(1);
    expect(overlay.stepAtEdge.get(E.q3no)).toBe(3);
  });

  it("resolves the final node from the last selected edge's destination", () => {
    expect(buildPathOverlay(path, edges).finalNodeId).toBe(N.close);
  });

  it("orders by step_number rather than trusting array order", () => {
    const shuffled = [path[2], path[0], path[1]];
    const overlay = buildPathOverlay(shuffled, edges);

    expect(overlay.visitedNodeIds).toEqual([N.q1, N.q2, N.q3, N.close]);
    expect(overlay.finalNodeId).toBe(N.close);
  });

  it("highlights nothing for an empty path when no terminal root is supplied", () => {
    const overlay = buildPathOverlay([], edges);

    expect(overlay.visitedNodeIds).toEqual([]);
    expect(overlay.selectedEdgeIds.size).toBe(0);
    expect(overlay.finalNodeId).toBeNull();
  });

  // A playbook version whose root is itself terminal completes the
  // investigation at creation with zero steps (docs/domain-model.md §4). Such
  // a report has nothing in `path` to derive an outcome from, so the caller
  // passes the root explicitly.
  describe("terminal-root investigations (zero steps)", () => {
    const soleNode = "c0000000-0000-4000-8000-000000000001";

    it("marks the root visited and final when the investigation completed there", () => {
      const overlay = buildPathOverlay([], [], { terminalRootNodeId: soleNode });

      expect(overlay.finalNodeId).toBe(soleNode);
      expect(overlay.visitedNodeIds).toEqual([soleNode]);
      expect(overlay.visited.has(soleNode)).toBe(true);
      // Still no decisions: nothing was selected and nothing carries a step.
      expect(overlay.selectedEdgeIds.size).toBe(0);
      expect(overlay.stepAtNode.size).toBe(0);
    });

    it("ignores the option for an in-progress investigation (caller passes null)", () => {
      const overlay = buildPathOverlay([], [], { terminalRootNodeId: null });

      expect(overlay.finalNodeId).toBeNull();
      expect(overlay.visitedNodeIds).toEqual([]);
    });

    it("ignores the option entirely once the path has steps", () => {
      const overlay = buildPathOverlay(path, edges, { terminalRootNodeId: soleNode });

      // The real path wins: the option is only a zero-step fallback.
      expect(overlay.finalNodeId).toBe(N.close);
      expect(overlay.visited.has(soleNode)).toBe(false);
      expect(overlay.visitedNodeIds).toEqual([N.q1, N.q2, N.q3, N.close]);
    });
  });

  it("does not invent a final node when the last edge is absent from the graph", () => {
    const overlay = buildPathOverlay([step(1, N.q1, "e0000000-0000-4000-8000-0000000000ff")], edges);

    expect(overlay.finalNodeId).toBeNull();
    expect(overlay.visitedNodeIds).toEqual([N.q1]);
  });
});

describe("buildOverlayFromSteps", () => {
  it("treats the live current node as the end of the path so far", () => {
    const overlay = buildOverlayFromSteps(
      [{ sequence_number: 1, node_id: N.q1, selected_edge_id: E.q1yes }],
      edges,
      N.q2,
    );

    expect(overlay.visitedNodeIds).toEqual([N.q1, N.q2]);
    expect(overlay.finalNodeId).toBe(N.q2);
    expect(overlay.stepAtEdge.get(E.q1yes)).toBe(1);
  });

  it("highlights the root alone before any decision is recorded", () => {
    const overlay = buildOverlayFromSteps([], edges, N.q1);

    expect(overlay.visitedNodeIds).toEqual([N.q1]);
    expect(overlay.selectedEdgeIds.size).toBe(0);
  });
});
