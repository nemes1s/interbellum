import dagre from "@dagrejs/dagre";

import type { PlaybookEdge, PlaybookNode, UUID } from "./types";

/**
 * The stored graph carries no coordinates — a playbook is a procedure, not a
 * drawing, and the backend is right not to persist layout. Positions are
 * therefore computed here, on every render, with dagre.
 *
 * Determinism matters: the same version must always draw identically, or two
 * reviewers comparing screenshots see different pictures. dagre itself is
 * deterministic given a fixed insertion order, so nodes and edges are sorted
 * before they are added — API ordering is not part of the contract.
 */

export const NODE_WIDTH = 268;
export const DECISION_NODE_HEIGHT = 92;
export const TERMINAL_NODE_HEIGHT = 104;

export interface PositionedNode {
  id: UUID;
  /** Top-left corner, which is the origin React Flow positions against. */
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface GraphLayout {
  nodes: Map<UUID, PositionedNode>;
  width: number;
  height: number;
}

function heightFor(node: PlaybookNode): number {
  return node.kind === "terminal" ? TERMINAL_NODE_HEIGHT : DECISION_NODE_HEIGHT;
}

/**
 * Lay a playbook graph out top-to-bottom.
 *
 * Edges are ordered by `sort_order` then `label`, which is what puts a node's
 * "Yes" branch consistently left of its "No" branch — the designer's declared
 * choice ordering becomes the drawing's left-to-right ordering.
 */
export function layoutGraph(nodes: PlaybookNode[], edges: PlaybookEdge[]): GraphLayout {
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({
    rankdir: "TB",
    ranksep: 84,
    nodesep: 44,
    marginx: 24,
    marginy: 24,
  });

  const known = new Set(nodes.map((node) => node.id));

  for (const node of [...nodes].sort((a, b) => a.id.localeCompare(b.id))) {
    graph.setNode(node.id, { width: NODE_WIDTH, height: heightFor(node) });
  }

  const ordered = [...edges].sort(
    (a, b) =>
      a.from_node_id.localeCompare(b.from_node_id) ||
      a.sort_order - b.sort_order ||
      a.label.localeCompare(b.label),
  );
  for (const edge of ordered) {
    // A draft may legitimately reference a node that is not in the payload;
    // dagre would invent a phantom node for it, so skip instead.
    if (known.has(edge.from_node_id) && known.has(edge.to_node_id)) {
      graph.setEdge(edge.from_node_id, edge.to_node_id);
    }
  }

  dagre.layout(graph);

  const positioned = new Map<UUID, PositionedNode>();
  let width = 0;
  let height = 0;
  for (const node of nodes) {
    const laid = graph.node(node.id) as { x: number; y: number; width: number; height: number } | undefined;
    if (!laid) continue;
    // dagre centres nodes; React Flow positions by top-left.
    const box: PositionedNode = {
      id: node.id,
      x: laid.x - laid.width / 2,
      y: laid.y - laid.height / 2,
      width: laid.width,
      height: laid.height,
    };
    positioned.set(node.id, box);
    width = Math.max(width, box.x + box.width);
    height = Math.max(height, box.y + box.height);
  }

  return { nodes: positioned, width, height };
}
