import type {
  JsonObject,
  PlaybookEdge,
  PlaybookGraphInput,
  PlaybookNode,
  PlaybookNodeKind,
  UUID,
} from "./types";

/**
 * A playbook graph as the authoring form holds it, before it is a contract
 * object.
 *
 * Every field is a string, including the ones the API types as objects and
 * integers, because they are controls a designer is midway through typing into:
 * `{` is a legal intermediate state for a metadata textarea, and an empty
 * `sort_order` box is a legal intermediate state for a number input. Validation
 * happens once, on save.
 *
 * IDs are minted here rather than by the server — the contract makes them
 * client-supplied so a whole graph, including its internal references, can be
 * authored in one request.
 */
export interface NodeDraft {
  id: UUID;
  kind: PlaybookNodeKind;
  title: string;
  description: string;
  /**
   * Kept even while `kind` is `decision`, so flipping the kind back and forth
   * does not silently discard what was typed. Serialization is what enforces
   * the "set iff terminal" rule, by only ever sending it for a terminal node.
   */
  terminalResolution: string;
  metadata: string;
}

export interface EdgeDraft {
  id: UUID;
  /** Empty while the designer has not picked an endpoint yet. */
  fromNodeId: UUID | "";
  toNodeId: UUID | "";
  label: string;
  description: string;
  sortOrder: string;
}

export interface PlaybookDraft {
  /** Empty when no root has been chosen — a normal state for a draft. */
  rootNodeId: UUID | "";
  nodes: NodeDraft[];
  edges: EdgeDraft[];
}

export const EMPTY_DRAFT: PlaybookDraft = { rootNodeId: "", nodes: [], edges: [] };

type NewId = () => UUID;

const uuid: NewId = () => crypto.randomUUID();

export function newNodeDraft(
  seed: Partial<Omit<NodeDraft, "id">> = {},
  newId: NewId = uuid,
): NodeDraft {
  return {
    id: newId(),
    kind: seed.kind ?? "decision",
    title: seed.title ?? "",
    description: seed.description ?? "",
    terminalResolution: seed.terminalResolution ?? "",
    metadata: seed.metadata ?? "",
  };
}

export function newEdgeDraft(
  seed: Partial<Omit<EdgeDraft, "id">> = {},
  newId: NewId = uuid,
): EdgeDraft {
  return {
    id: newId(),
    fromNodeId: seed.fromNodeId ?? "",
    toNodeId: seed.toNodeId ?? "",
    label: seed.label ?? "",
    description: seed.description ?? "",
    sortOrder: seed.sortOrder ?? "0",
  };
}

export type DraftErrorField =
  | "id"
  | "title"
  | "terminalResolution"
  | "metadata"
  | "label"
  | "fromNodeId"
  | "toNodeId"
  | "rootNodeId";

export interface DraftError {
  scope: "node" | "edge" | "root";
  /** Position in the node or edge list; `-1` for a graph-level problem. */
  index: number;
  field: DraftErrorField;
  message: string;
}

export type DraftSerializationResult =
  | { ok: true; graph: PlaybookGraphInput }
  | { ok: false; errors: DraftError[] };

/**
 * Turn the form's drafts into the `PlaybookGraphInput` the API accepts, or
 * report every problem at once.
 *
 * The rules below are exactly docs/domain-model.md §5's *write-time* table —
 * the ones a draft `PUT` rejects with a `400`, whether from service-layer input
 * validation or from a database constraint. They are re-stated here only so the
 * designer gets an inline message instead of a round trip.
 *
 * Publish-time validation — root set, reachability, acyclicity, decision nodes
 * having choices — is deliberately **not** here. An unfinished draft is a
 * normal state to save, and the server's `422` enumerating every graph problem
 * is the thing this screen exists to show.
 */
export function serializePlaybookDraft(draft: PlaybookDraft): DraftSerializationResult {
  const errors: DraftError[] = [];
  const nodes: PlaybookNode[] = [];

  const seenNodeIds = new Set<string>();
  draft.nodes.forEach((node, index) => {
    if (seenNodeIds.has(node.id)) {
      errors.push({ scope: "node", index, field: "id", message: "This node's id is already used by another node." });
    }
    seenNodeIds.add(node.id);

    const title = node.title.trim();
    if (title === "") {
      errors.push({ scope: "node", index, field: "title", message: "Title is required." });
    }

    const resolution = node.terminalResolution.trim();
    if (node.kind === "terminal" && resolution === "") {
      errors.push({
        scope: "node",
        index,
        field: "terminalResolution",
        message: "A terminal node must carry a resolution — it is the investigation's outcome.",
      });
    }

    const metadata = parseMetadata(node.metadata);
    if (metadata.error !== null) {
      errors.push({ scope: "node", index, field: "metadata", message: metadata.error });
    }

    nodes.push({
      id: node.id,
      kind: node.kind,
      title,
      description: node.description.trim(),
      // The "iff terminal" CHECK constraint holds by construction: a decision
      // node cannot carry a resolution because nothing here can put one there.
      terminal_resolution: node.kind === "terminal" ? resolution : null,
      metadata: metadata.value,
    });
  });

  const edges: PlaybookEdge[] = [];
  const seenEdgeIds = new Set<string>();
  const labelsByFromNode = new Map<string, Set<string>>();

  draft.edges.forEach((edge, index) => {
    if (seenEdgeIds.has(edge.id)) {
      errors.push({ scope: "edge", index, field: "id", message: "This edge's id is already used by another edge." });
    }
    seenEdgeIds.add(edge.id);

    const label = edge.label.trim();
    if (label === "") {
      errors.push({ scope: "edge", index, field: "label", message: "Label is required — it is what an analyst picks." });
    }

    for (const [field, value] of [
      ["fromNodeId", edge.fromNodeId],
      ["toNodeId", edge.toNodeId],
    ] as const) {
      if (value === "") {
        errors.push({ scope: "edge", index, field, message: "Pick a node." });
      } else if (!seenNodeIds.has(value)) {
        // Reachable when the node an edge pointed at was deleted; the composite
        // foreign key would reject this write.
        errors.push({ scope: "edge", index, field, message: "That node is no longer part of this draft." });
      }
    }

    if (edge.fromNodeId !== "" && label !== "") {
      const labels = labelsByFromNode.get(edge.fromNodeId) ?? new Set<string>();
      if (labels.has(label)) {
        errors.push({
          scope: "edge",
          index,
          field: "label",
          message: `Another choice leaving this node is already labelled “${label}”. One label per node.`,
        });
      }
      labels.add(label);
      labelsByFromNode.set(edge.fromNodeId, labels);
    }

    edges.push({
      id: edge.id,
      from_node_id: edge.fromNodeId,
      to_node_id: edge.toNodeId,
      label,
      description: edge.description.trim() === "" ? null : edge.description.trim(),
      sort_order: parseSortOrder(edge.sortOrder),
    });
  });

  if (draft.rootNodeId !== "" && !seenNodeIds.has(draft.rootNodeId)) {
    errors.push({
      scope: "root",
      index: -1,
      field: "rootNodeId",
      message: "The chosen root is no longer part of this draft.",
    });
  }

  if (errors.length > 0) return { ok: false, errors };

  return {
    ok: true,
    graph: {
      root_node_id: draft.rootNodeId === "" ? null : draft.rootNodeId,
      nodes,
      edges,
    },
  };
}

function parseMetadata(raw: string): { value: JsonObject | null; error: string | null } {
  const text = raw.trim();
  if (text === "") return { value: null, error: null };

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { value: null, error: "Metadata is not valid JSON." };
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      value: null,
      error: 'Metadata must be a JSON object, for example {"suggested_evidence": ["asset_inventory_lookup"]}.',
    };
  }
  return { value: parsed as JsonObject, error: null };
}

/** A blank or unparseable box means "no ordering preference", which is the API's own default. */
function parseSortOrder(raw: string): number {
  const parsed = Number.parseInt(raw, 10);
  return Number.isNaN(parsed) ? 0 : parsed;
}

/** Load a stored version's graph back into the form. */
export function draftFromDefinition(definition: PlaybookGraphInput): PlaybookDraft {
  return {
    rootNodeId: definition.root_node_id ?? "",
    nodes: definition.nodes.map((node) => ({
      id: node.id,
      kind: node.kind,
      title: node.title,
      description: node.description ?? "",
      terminalResolution: node.terminal_resolution ?? "",
      metadata: node.metadata ? JSON.stringify(node.metadata, null, 2) : "",
    })),
    edges: definition.edges.map((edge) => ({
      id: edge.id,
      fromNodeId: edge.from_node_id,
      toNodeId: edge.to_node_id,
      label: edge.label,
      description: edge.description ?? "",
      sortOrder: String(edge.sort_order),
    })),
  };
}

/**
 * The draft as the live preview can draw it.
 *
 * Deliberately lenient where `serializePlaybookDraft` is strict: a graph being
 * built passes through states the API would reject, and the preview's job is to
 * show the shape anyway. Untitled nodes get a placeholder, and an edge with an
 * endpoint that is not (yet) a node is dropped rather than drawn to nowhere.
 */
export function previewGraph(draft: PlaybookDraft): { nodes: PlaybookNode[]; edges: PlaybookEdge[] } {
  const nodes: PlaybookNode[] = draft.nodes.map((node) => ({
    id: node.id,
    kind: node.kind,
    title: node.title.trim() === "" ? "Untitled node" : node.title.trim(),
    description: node.description.trim(),
    terminal_resolution: node.kind === "terminal" ? node.terminalResolution.trim() || null : null,
  }));

  const known = new Set(nodes.map((node) => node.id));
  const edges: PlaybookEdge[] = draft.edges
    .filter((edge) => known.has(edge.fromNodeId) && known.has(edge.toNodeId))
    .map((edge) => ({
      id: edge.id,
      from_node_id: edge.fromNodeId,
      to_node_id: edge.toNodeId,
      label: edge.label.trim() === "" ? "—" : edge.label.trim(),
      description: null,
      sort_order: parseSortOrder(edge.sortOrder),
    }));

  return { nodes, edges };
}
