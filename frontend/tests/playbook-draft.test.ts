import { describe, expect, it } from "vitest";

import {
  draftFromDefinition,
  EMPTY_DRAFT,
  newEdgeDraft,
  newNodeDraft,
  previewGraph,
  serializePlaybookDraft,
  type PlaybookDraft,
} from "@/lib/playbook-draft";

/** Deterministic ids, so a failure names the node it is about rather than a fresh UUID. */
let counter = 0;
const nextId = () => `id-${(counter += 1)}`;

const node = (seed: Parameters<typeof newNodeDraft>[0]) => newNodeDraft(seed, nextId);
const edge = (seed: Parameters<typeof newEdgeDraft>[0]) => newEdgeDraft(seed, nextId);

function draft(partial: Partial<PlaybookDraft>): PlaybookDraft {
  return { ...EMPTY_DRAFT, ...partial };
}

/**
 * The authoring form holds strings; the API takes a typed `PlaybookGraphInput`.
 * This is the conversion, and every rule it enforces is one the draft `PUT`
 * would otherwise reject with a 400 the designer cannot act on.
 */
describe("serializePlaybookDraft", () => {
  it("produces the exact contract shape for a complete two-node graph", () => {
    const decision = node({ kind: "decision", title: "  Known workstation?  ", description: " Check inventory. " });
    const terminal = node({
      kind: "terminal",
      title: "Close: authorized maintenance",
      terminalResolution: " close_authorized_maintenance ",
      metadata: '{"suggested_evidence": ["asset_inventory_lookup"]}',
    });

    const result = serializePlaybookDraft(
      draft({
        rootNodeId: decision.id,
        nodes: [decision, terminal],
        edges: [edge({ fromNodeId: decision.id, toNodeId: terminal.id, label: " Yes ", sortOrder: "2" })],
      }),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.graph).toEqual({
      root_node_id: decision.id,
      nodes: [
        {
          id: decision.id,
          kind: "decision",
          title: "Known workstation?",
          description: "Check inventory.",
          terminal_resolution: null,
          metadata: null,
        },
        {
          id: terminal.id,
          kind: "terminal",
          title: "Close: authorized maintenance",
          description: "",
          terminal_resolution: "close_authorized_maintenance",
          metadata: { suggested_evidence: ["asset_inventory_lookup"] },
        },
      ],
      edges: [
        {
          id: result.graph.edges[0].id,
          from_node_id: decision.id,
          to_node_id: terminal.id,
          label: "Yes",
          description: null,
          sort_order: 2,
        },
      ],
    });
  });

  it("allows an incomplete draft with no root and no nodes — that is a normal state to save", () => {
    expect(serializePlaybookDraft(EMPTY_DRAFT)).toEqual({
      ok: true,
      graph: { root_node_id: null, nodes: [], edges: [] },
    });
  });

  it("allows nodes with no root chosen yet, sending root_node_id as null", () => {
    const orphan = node({ title: "First question" });

    const result = serializePlaybookDraft(draft({ nodes: [orphan] }));

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.graph.root_node_id).toBeNull();
  });

  it("does not re-implement publish-time validation: an unreachable, edgeless decision node still saves", () => {
    const root = node({ kind: "decision", title: "Root" });
    const stranded = node({ kind: "decision", title: "Nothing points here and nothing leaves" });

    const result = serializePlaybookDraft(draft({ rootNodeId: root.id, nodes: [root, stranded] }));

    expect(result.ok).toBe(true);
  });

  describe("the terminal_resolution iff terminal rule", () => {
    it("drops a resolution left over from a node that is now a decision", () => {
      const flipped = node({ kind: "decision", title: "Was terminal", terminalResolution: "close_it" });

      const result = serializePlaybookDraft(draft({ nodes: [flipped] }));

      expect(result.ok).toBe(true);
      if (!result.ok) return;
      expect(result.graph.nodes[0].terminal_resolution).toBeNull();
    });

    it("rejects a terminal node with no resolution", () => {
      const bare = node({ kind: "terminal", title: "Close" });

      const result = serializePlaybookDraft(draft({ nodes: [bare] }));

      expect(result.ok).toBe(false);
      if (result.ok) return;
      expect(result.errors).toEqual([
        { scope: "node", index: 0, field: "terminalResolution", message: expect.stringContaining("must carry a resolution") },
      ]);
    });
  });

  it("reports two edges leaving the same node with the same label, against the second one", () => {
    const from = node({ title: "Known workstation?" });
    const a = node({ kind: "terminal", title: "A", terminalResolution: "a" });
    const b = node({ kind: "terminal", title: "B", terminalResolution: "b" });

    const result = serializePlaybookDraft(
      draft({
        nodes: [from, a, b],
        edges: [
          edge({ fromNodeId: from.id, toNodeId: a.id, label: "Yes" }),
          edge({ fromNodeId: from.id, toNodeId: b.id, label: "Yes" }),
        ],
      }),
    );

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([
      { scope: "edge", index: 1, field: "label", message: expect.stringContaining("already labelled") },
    ]);
  });

  it("allows the same label on edges leaving different nodes", () => {
    const first = node({ title: "First?" });
    const second = node({ title: "Second?" });
    const end = node({ kind: "terminal", title: "End", terminalResolution: "done" });

    const result = serializePlaybookDraft(
      draft({
        nodes: [first, second, end],
        edges: [
          edge({ fromNodeId: first.id, toNodeId: second.id, label: "Yes" }),
          edge({ fromNodeId: second.id, toNodeId: end.id, label: "Yes" }),
        ],
      }),
    );

    expect(result.ok).toBe(true);
  });

  it("collects every write-time problem in one pass rather than stopping at the first", () => {
    const untitled = node({ kind: "decision", title: "   " });
    const badMetadata = node({ kind: "terminal", title: "End", terminalResolution: "done", metadata: "[1, 2]" });

    const result = serializePlaybookDraft(
      draft({
        rootNodeId: "a-node-that-was-deleted",
        nodes: [untitled, badMetadata],
        edges: [edge({ fromNodeId: untitled.id, toNodeId: "", label: "" })],
      }),
    );

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors.map((error) => [error.scope, error.index, error.field])).toEqual([
      ["node", 0, "title"],
      ["node", 1, "metadata"],
      ["edge", 0, "label"],
      ["edge", 0, "toNodeId"],
      ["root", -1, "rootNodeId"],
    ]);
  });

  it("reports an edge endpoint whose node has since been deleted", () => {
    const from = node({ title: "Still here" });

    const result = serializePlaybookDraft(
      draft({ nodes: [from], edges: [edge({ fromNodeId: from.id, toNodeId: "gone", label: "Yes" })] }),
    );

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([
      { scope: "edge", index: 0, field: "toNodeId", message: expect.stringContaining("no longer part of this draft") },
    ]);
  });

  it.each([
    ["an array", "[1, 2, 3]"],
    ["a scalar", "42"],
    ["not JSON at all", "{oops"],
  ])("rejects node metadata that is %s, matching the API's object-only rule", (_label, metadata) => {
    const result = serializePlaybookDraft(draft({ nodes: [node({ title: "Q", metadata })] }));

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ scope: "node", index: 0, field: "metadata" });
  });

  it("treats a blank sort order as the API's own default of 0", () => {
    const from = node({ title: "Q" });
    const to = node({ kind: "terminal", title: "End", terminalResolution: "done" });

    const result = serializePlaybookDraft(
      draft({ nodes: [from, to], edges: [edge({ fromNodeId: from.id, toNodeId: to.id, label: "Yes", sortOrder: "" })] }),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.graph.edges[0].sort_order).toBe(0);
  });
});

describe("draftFromDefinition", () => {
  it("round-trips a stored version back through serialization unchanged", () => {
    const definition = {
      root_node_id: "n1",
      nodes: [
        {
          id: "n1",
          kind: "decision" as const,
          title: "Known workstation?",
          description: "Check inventory.",
          terminal_resolution: null,
          metadata: { suggested_evidence: ["asset_inventory_lookup"] },
        },
        {
          id: "n2",
          kind: "terminal" as const,
          title: "Close",
          description: "",
          terminal_resolution: "close_authorized_maintenance",
          metadata: null,
        },
      ],
      edges: [
        {
          id: "e1",
          from_node_id: "n1",
          to_node_id: "n2",
          label: "Yes",
          description: "Source is trusted.",
          sort_order: 1,
        },
      ],
    };

    const result = serializePlaybookDraft(draftFromDefinition(definition));

    expect(result).toEqual({ ok: true, graph: definition });
  });
});

describe("previewGraph", () => {
  it("draws a half-built graph the API would reject, so the shape is visible while authoring", () => {
    const titled = node({ title: "Known workstation?" });
    const untitled = node({ kind: "terminal", title: "" });

    const preview = previewGraph(
      draft({
        nodes: [titled, untitled],
        edges: [
          edge({ fromNodeId: titled.id, toNodeId: untitled.id, label: "" }),
          edge({ fromNodeId: titled.id, toNodeId: "", label: "No" }),
        ],
      }),
    );

    expect(preview.nodes.map((n) => n.title)).toEqual(["Known workstation?", "Untitled node"]);
    // The edge with no destination is dropped rather than drawn to nowhere.
    expect(preview.edges).toHaveLength(1);
  });
});
