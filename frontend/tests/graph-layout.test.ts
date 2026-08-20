import { describe, expect, it } from "vitest";

import { layoutGraph, NODE_WIDTH } from "@/lib/graph-layout";
import type { PlaybookEdge, PlaybookNode } from "@/lib/types";

const nodes: PlaybookNode[] = [
  { id: "n1", kind: "decision", title: "Root question", description: "", terminal_resolution: null },
  { id: "n2", kind: "decision", title: "Follow-up", description: "", terminal_resolution: null },
  { id: "n3", kind: "terminal", title: "Close", description: "", terminal_resolution: "close_it" },
  { id: "n4", kind: "terminal", title: "Escalate", description: "", terminal_resolution: "escalate_it" },
];

const edges: PlaybookEdge[] = [
  { id: "e1", from_node_id: "n1", to_node_id: "n2", label: "Yes", description: null, sort_order: 1 },
  { id: "e2", from_node_id: "n1", to_node_id: "n4", label: "No", description: null, sort_order: 2 },
  { id: "e3", from_node_id: "n2", to_node_id: "n3", label: "Yes", description: null, sort_order: 1 },
];

/**
 * The stored graph carries no coordinates, so the console computes them. Two
 * things must hold: every node gets a box, and the same graph always draws the
 * same way regardless of the order the API happened to return rows in.
 */
describe("layoutGraph", () => {
  it("positions every node and reports the overall extent", () => {
    const layout = layoutGraph(nodes, edges);

    expect(layout.nodes.size).toBe(4);
    for (const node of nodes) {
      const box = layout.nodes.get(node.id);
      expect(box).toBeDefined();
      expect(box!.width).toBe(NODE_WIDTH);
      expect(Number.isFinite(box!.x)).toBe(true);
      expect(Number.isFinite(box!.y)).toBe(true);
    }
    expect(layout.width).toBeGreaterThan(0);
    expect(layout.height).toBeGreaterThan(0);
  });

  it("ranks children below their parent, top to bottom", () => {
    const layout = layoutGraph(nodes, edges);

    expect(layout.nodes.get("n2")!.y).toBeGreaterThan(layout.nodes.get("n1")!.y);
    expect(layout.nodes.get("n3")!.y).toBeGreaterThan(layout.nodes.get("n2")!.y);
  });

  it("is deterministic: API ordering does not change the drawing", () => {
    const forwards = layoutGraph(nodes, edges);
    const backwards = layoutGraph([...nodes].reverse(), [...edges].reverse());

    for (const node of nodes) {
      expect(backwards.nodes.get(node.id)).toEqual(forwards.nodes.get(node.id));
    }
  });

  it("ignores an edge pointing at a node the payload does not contain", () => {
    // Legal in a draft, and dagre would otherwise invent a phantom node for it.
    const dangling: PlaybookEdge = {
      id: "e9",
      from_node_id: "n1",
      to_node_id: "missing",
      label: "Maybe",
      description: null,
      sort_order: 3,
    };

    const layout = layoutGraph(nodes, [...edges, dangling]);

    expect(layout.nodes.size).toBe(4);
    expect(layout.nodes.has("missing")).toBe(false);
  });

  it("returns an empty layout for a draft with no nodes", () => {
    const layout = layoutGraph([], []);

    expect(layout.nodes.size).toBe(0);
    expect(layout.width).toBe(0);
  });
});
