"use client";

import { Button, Chip, Empty, Id, LabelledField, Select, TextArea, TextInput } from "@/components/primitives";
import type { DraftError, NodeDraft } from "@/lib/playbook-draft";
import type { PlaybookNodeKind, UUID } from "@/lib/types";

import { FieldError, errorFor } from "./FieldError";

/**
 * The node list, as a form.
 *
 * `terminal_resolution` appears only on a terminal node rather than being
 * rendered and disabled, because the rule it encodes is not "this field is
 * unavailable" but "a decision node does not have one" — the database CHECK
 * constraint rejects a decision node that carries a resolution just as firmly
 * as a terminal node that lacks one.
 */
export function NodeListEditor({
  nodes,
  rootNodeId,
  errors,
  disabled,
  onAdd,
  onUpdate,
  onRemove,
  onAddChoice,
}: {
  nodes: NodeDraft[];
  rootNodeId: string;
  errors: DraftError[];
  disabled?: boolean;
  onAdd: (kind: PlaybookNodeKind) => void;
  onUpdate: (index: number, patch: Partial<NodeDraft>) => void;
  onRemove: (index: number) => void;
  onAddChoice: (nodeId: UUID) => void;
}) {
  return (
    <div className="space-y-3">
      {nodes.length === 0 ? (
        <Empty title="No nodes yet">
          <p>
            Start with the first question an analyst should answer. A draft with no nodes is still a
            valid draft — the API only insists on a complete graph at publish time.
          </p>
        </Empty>
      ) : (
        <ul className="space-y-3">
          {nodes.map((node, index) => (
            <li key={node.id} className="rounded-[2px] border border-rule bg-sunken p-3">
              <div className="mb-2.5 flex flex-wrap items-center gap-2">
                <span className="label">Node {index + 1}</span>
                {node.id === rootNodeId && <Chip tone="signal">root</Chip>}
                <Id value={node.id} />
                <div className="ml-auto flex items-center gap-1">
                  {node.kind === "decision" && (
                    <Button
                      type="button"
                      variant="quiet"
                      className="!py-0.5 !text-[12px]"
                      disabled={disabled}
                      onClick={() => onAddChoice(node.id)}
                    >
                      + Choice from here
                    </Button>
                  )}
                  <Button
                    type="button"
                    variant="quiet"
                    className="!py-0.5 !text-[12px]"
                    disabled={disabled}
                    onClick={() => onRemove(index)}
                  >
                    Remove
                  </Button>
                </div>
              </div>

              <div className="grid gap-2.5 sm:grid-cols-[150px_minmax(0,1fr)]">
                <LabelledField label="Kind" htmlFor={`${node.id}-kind`}>
                  <Select
                    id={`${node.id}-kind`}
                    value={node.kind}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { kind: e.target.value as PlaybookNodeKind })}
                  >
                    <option value="decision">decision</option>
                    <option value="terminal">terminal</option>
                  </Select>
                </LabelledField>

                <LabelledField label="Title" htmlFor={`${node.id}-title`}>
                  <TextInput
                    id={`${node.id}-title`}
                    value={node.title}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { title: e.target.value })}
                    placeholder={
                      node.kind === "decision"
                        ? "Write came from a known engineering workstation?"
                        : "Close: authorized maintenance"
                    }
                    aria-invalid={Boolean(errorFor(errors, "node", index, "title"))}
                  />
                  <FieldError message={errorFor(errors, "node", index, "title")} />
                </LabelledField>
              </div>

              <LabelledField label="Description" htmlFor={`${node.id}-description`} className="mt-2.5">
                <TextInput
                  id={`${node.id}-description`}
                  value={node.description}
                  disabled={disabled}
                  onChange={(e) => onUpdate(index, { description: e.target.value })}
                  placeholder="What the analyst should actually check here."
                />
              </LabelledField>

              {node.kind === "terminal" && (
                <LabelledField
                  label="Terminal resolution"
                  htmlFor={`${node.id}-resolution`}
                  className="mt-2.5"
                  hint="The investigation's recorded outcome. A decision node must not have one; a terminal node must."
                >
                  <TextInput
                    id={`${node.id}-resolution`}
                    value={node.terminalResolution}
                    disabled={disabled}
                    onChange={(e) => onUpdate(index, { terminalResolution: e.target.value })}
                    className="font-mono !text-[12px]"
                    placeholder="close_authorized_maintenance"
                    aria-invalid={Boolean(errorFor(errors, "node", index, "terminalResolution"))}
                  />
                  <FieldError message={errorFor(errors, "node", index, "terminalResolution")} />
                </LabelledField>
              )}

              <LabelledField
                label="Metadata (JSON object, optional)"
                htmlFor={`${node.id}-metadata`}
                className="mt-2.5"
                hint="Designer hints for an agent. The engine stores it and hands it back; it never reads it."
              >
                <TextArea
                  id={`${node.id}-metadata`}
                  rows={3}
                  value={node.metadata}
                  disabled={disabled}
                  onChange={(e) => onUpdate(index, { metadata: e.target.value })}
                  className="font-mono !text-[11.5px]"
                  spellCheck={false}
                  placeholder={'{ "suggested_evidence": ["asset_inventory_lookup"] }'}
                  aria-invalid={Boolean(errorFor(errors, "node", index, "metadata"))}
                />
                <FieldError message={errorFor(errors, "node", index, "metadata")} />
              </LabelledField>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap gap-2">
        <Button type="button" disabled={disabled} onClick={() => onAdd("decision")}>
          + Decision node
        </Button>
        <Button type="button" disabled={disabled} onClick={() => onAdd("terminal")}>
          + Terminal node
        </Button>
      </div>
    </div>
  );
}
