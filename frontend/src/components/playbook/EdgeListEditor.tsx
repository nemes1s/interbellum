"use client";

import { Button, Empty, LabelledField, Select, TextInput } from "@/components/primitives";
import type { DraftError, EdgeDraft, NodeDraft } from "@/lib/playbook-draft";

import { FieldError, errorFor } from "./FieldError";

/**
 * The edge list, as a form.
 *
 * Endpoints are selects over the nodes that exist right now, so the only way to
 * express an edge is between two real nodes — which is also why there is no
 * drawing surface here. `label` is what an analyst is shown at decision time,
 * and no two edges leaving one node may share it.
 */
export function EdgeListEditor({
  edges,
  nodes,
  errors,
  disabled,
  onAdd,
  onUpdate,
  onRemove,
}: {
  edges: EdgeDraft[];
  nodes: NodeDraft[];
  errors: DraftError[];
  disabled?: boolean;
  onAdd: () => void;
  onUpdate: (index: number, patch: Partial<EdgeDraft>) => void;
  onRemove: (index: number) => void;
}) {
  // A terminal node has no outgoing choices, so it is never a source. It is
  // still offered as a destination: reaching one is how an investigation ends.
  const sources = nodes.filter((node) => node.kind === "decision");

  return (
    <div className="space-y-3">
      {edges.length === 0 ? (
        <Empty title="No choices yet">
          <p>
            An edge is one answer to a decision node&apos;s question. Every decision node needs at
            least one before the version can be published.
          </p>
        </Empty>
      ) : (
        <ul className="space-y-3">
          {edges.map((edge, index) => (
            <li key={edge.id} className="rounded-[2px] border border-rule bg-sunken p-3">
              <div className="mb-2.5 flex items-center gap-2">
                <span className="label">Choice {index + 1}</span>
                <Button
                  type="button"
                  variant="quiet"
                  className="ml-auto !py-0.5 !text-[12px]"
                  disabled={disabled}
                  onClick={() => onRemove(index)}
                >
                  Remove
                </Button>
              </div>

              <div className="grid gap-2.5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_88px]">
                <LabelledField label="From node" htmlFor={`${edge.id}-from`}>
                  <Select
                    id={`${edge.id}-from`}
                    value={edge.fromNodeId}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { fromNodeId: e.target.value })}
                    aria-invalid={Boolean(errorFor(errors, "edge", index, "fromNodeId"))}
                  >
                    <NodeOptions nodes={sources} selectedId={edge.fromNodeId} allNodes={nodes} />
                  </Select>
                  <FieldError message={errorFor(errors, "edge", index, "fromNodeId")} />
                </LabelledField>

                <LabelledField label="Label" htmlFor={`${edge.id}-label`}>
                  <TextInput
                    id={`${edge.id}-label`}
                    value={edge.label}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { label: e.target.value })}
                    placeholder="Yes"
                    aria-invalid={Boolean(errorFor(errors, "edge", index, "label"))}
                  />
                  <FieldError message={errorFor(errors, "edge", index, "label")} />
                </LabelledField>

                <LabelledField label="To node" htmlFor={`${edge.id}-to`}>
                  <Select
                    id={`${edge.id}-to`}
                    value={edge.toNodeId}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { toNodeId: e.target.value })}
                    aria-invalid={Boolean(errorFor(errors, "edge", index, "toNodeId"))}
                  >
                    <NodeOptions nodes={nodes} selectedId={edge.toNodeId} allNodes={nodes} />
                  </Select>
                  <FieldError message={errorFor(errors, "edge", index, "toNodeId")} />
                </LabelledField>

                <LabelledField label="Order" htmlFor={`${edge.id}-sort`}>
                  <TextInput
                    id={`${edge.id}-sort`}
                    type="number"
                    step={1}
                    value={edge.sortOrder}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { sortOrder: e.target.value })}
                    className="font-mono"
                  />
                </LabelledField>
              </div>

              <LabelledField label="Description" htmlFor={`${edge.id}-description`} className="mt-2.5">
                <TextInput
                  id={`${edge.id}-description`}
                  value={edge.description}
                  disabled={disabled}
                  onChange={(e) => onUpdate(index, { description: e.target.value })}
                  placeholder="Shown beneath the choice when an analyst picks it."
                />
              </LabelledField>
            </li>
          ))}
        </ul>
      )}

      <Button type="button" disabled={disabled || nodes.length === 0} onClick={onAdd}>
        + Choice
      </Button>
    </div>
  );
}

/**
 * Node options for an endpoint select.
 *
 * A node that was selected and has since become ineligible — a decision node
 * flipped to terminal while an edge still leaves it — is still listed, so the
 * select shows what the draft actually holds rather than silently reading as
 * unset. Saving that draft is allowed; `terminal_node_with_edges` is one of the
 * publish-time checks, so publishing is what surfaces the problem.
 */
function NodeOptions({
  nodes,
  selectedId,
  allNodes,
}: {
  nodes: NodeDraft[];
  selectedId: string;
  allNodes: NodeDraft[];
}) {
  const stale =
    selectedId !== "" && !nodes.some((node) => node.id === selectedId)
      ? allNodes.find((node) => node.id === selectedId)
      : undefined;

  return (
    <>
      <option value="">— select a node —</option>
      {nodes.map((node) => (
        <option key={node.id} value={node.id}>
          {optionLabel(node, allNodes)}
        </option>
      ))}
      {stale && (
        <option key={stale.id} value={stale.id}>
          {optionLabel(stale, allNodes)}
        </option>
      )}
    </>
  );
}

function optionLabel(node: NodeDraft, allNodes: NodeDraft[]): string {
  const position = allNodes.indexOf(node) + 1;
  const title = node.title.trim() === "" ? "Untitled node" : node.title.trim();
  return `${position}. ${title}`;
}
