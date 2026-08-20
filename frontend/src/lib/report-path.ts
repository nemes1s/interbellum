import type { PathStep, PlaybookEdge, PlaybookVersionDefinition, UUID } from "./types";

/**
 * Derives the visual overlay for an investigation report.
 *
 * The backend deliberately returns the canonical graph and the ordered `path`
 * as two separate fields, with no `was_visited` / `was_selected` flags on the
 * graph — the graph belongs to the playbook version, the path belongs to one
 * run. This module is the entire transform between them, and it is the only
 * place the console decides what is highlighted.
 *
 * It never mutates or reinterprets the graph. Highlighting is derived from
 * `path`: nodes come from `path[].node_id` plus the one node the last selected
 * edge leads to, and edges come from `path[].selected_edge_id`. Nothing is
 * inferred from the graph's shape.
 *
 * There is exactly one documented exception, and it is passed in explicitly
 * rather than guessed at — see `terminalRootNodeId` on `PathOverlayOptions`.
 */

export interface PathOverlay {
  /** Nodes the investigation actually stood at, in visit order. */
  visitedNodeIds: UUID[];
  /** Fast membership test over `visitedNodeIds`. */
  visited: Set<UUID>;
  /** Step number stamped on each visited node (1-based); the final node has none. */
  stepAtNode: Map<UUID, number>;
  /** Edges the investigation selected. */
  selectedEdgeIds: Set<UUID>;
  /** Step number stamped on each selected edge. */
  stepAtEdge: Map<UUID, number>;
  /**
   * The node the investigation ended at — the destination of the last selected
   * edge. Null when the path is empty (an investigation that has taken no
   * decision yet, or one whose root was itself terminal).
   */
  finalNodeId: UUID | null;
}

/** Empty overlay: nothing visited, nothing selected. */
function emptyOverlay(): PathOverlay {
  return {
    visitedNodeIds: [],
    visited: new Set(),
    stepAtNode: new Map(),
    selectedEdgeIds: new Set(),
    stepAtEdge: new Map(),
    finalNodeId: null,
  };
}

export interface PathOverlayOptions {
  /**
   * The node a **zero-step** investigation ended at.
   *
   * A playbook version whose root node is itself `terminal` is degenerate but
   * valid: starting an investigation against one creates it already
   * `completed`, with the resolution copied from the root and **no** steps
   * recorded — the backend deliberately invents no synthetic step just to
   * record arrival (docs/domain-model.md §4).
   *
   * Such a report has an empty `path`, so there is nothing to derive a final
   * node from. The caller supplies the published `root_node_id` here, and only
   * when the investigation is actually `completed`, so this stays an explicit
   * decision at the one call site that can know it is correct rather than an
   * inference made from the graph's shape.
   *
   * It is ignored whenever `path` is non-empty.
   */
  terminalRootNodeId?: UUID | null;
}

/**
 * Build the overlay from a report's ordered path and its canonical edge list.
 *
 * `edges` is read only to resolve where the last selected edge landed — the
 * path records the node a decision was made *from*, so the terminal node the
 * investigation reached is not in `path[].node_id` at all.
 */
export function buildPathOverlay(
  path: PathStep[],
  edges: PlaybookEdge[],
  options: PathOverlayOptions = {},
): PathOverlay {
  if (path.length === 0) {
    const overlay = emptyOverlay();
    if (options.terminalRootNodeId) {
      // Zero steps, but the investigation finished: it finished where it
      // started. Marking the root visited and final is what makes a
      // terminal-root report show its outcome at all.
      overlay.finalNodeId = options.terminalRootNodeId;
      overlay.visited.add(options.terminalRootNodeId);
      overlay.visitedNodeIds.push(options.terminalRootNodeId);
    }
    return overlay;
  }

  const ordered = [...path].sort((a, b) => a.step_number - b.step_number);
  const overlay = emptyOverlay();

  for (const step of ordered) {
    if (!overlay.visited.has(step.node_id)) {
      overlay.visited.add(step.node_id);
      overlay.visitedNodeIds.push(step.node_id);
    }
    // A node is stamped with the step taken *from* it. If a path somehow
    // revisits a node, the first visit is what the stamp refers to.
    if (!overlay.stepAtNode.has(step.node_id)) {
      overlay.stepAtNode.set(step.node_id, step.step_number);
    }
    overlay.selectedEdgeIds.add(step.selected_edge_id);
    overlay.stepAtEdge.set(step.selected_edge_id, step.step_number);
  }

  const last = ordered[ordered.length - 1];
  const lastEdge = edges.find((edge) => edge.id === last.selected_edge_id);
  if (lastEdge) {
    overlay.finalNodeId = lastEdge.to_node_id;
    if (!overlay.visited.has(lastEdge.to_node_id)) {
      overlay.visited.add(lastEdge.to_node_id);
      overlay.visitedNodeIds.push(lastEdge.to_node_id);
    }
  }

  return overlay;
}

/**
 * Overlay for an in-progress investigation, whose "path so far" comes from
 * `steps` rather than a report. Identical shape, so the graph component draws
 * the live runner and the finished report with the same code.
 */
export function buildOverlayFromSteps(
  steps: { sequence_number: number; node_id: UUID; selected_edge_id: UUID }[],
  edges: PlaybookEdge[],
  currentNodeId: UUID | null,
): PathOverlay {
  const overlay = buildPathOverlay(
    steps.map((step) => ({
      step_number: step.sequence_number,
      node_id: step.node_id,
      selected_edge_id: step.selected_edge_id,
      actor: { type: "human", id: null },
      rationale: null,
      evidence: [],
      created_at: "",
    })),
    edges,
  );

  if (currentNodeId) {
    overlay.finalNodeId = currentNodeId;
    if (!overlay.visited.has(currentNodeId)) {
      overlay.visited.add(currentNodeId);
      overlay.visitedNodeIds.push(currentNodeId);
    }
  }

  return overlay;
}

/** Convenience: index a version's nodes by id. */
export function indexNodes(version: PlaybookVersionDefinition) {
  return new Map(version.nodes.map((node) => [node.id, node] as const));
}

/** Convenience: index a version's edges by id. */
export function indexEdges(version: PlaybookVersionDefinition) {
  return new Map(version.edges.map((edge) => [edge.id, edge] as const));
}
