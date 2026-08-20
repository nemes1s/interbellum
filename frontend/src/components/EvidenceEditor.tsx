"use client";

import { Button, LabelledField, TextArea, TextInput } from "@/components/primitives";
import type { EvidenceDraft, EvidenceSerializationError } from "@/lib/evidence";
import { newEvidenceDraft } from "@/lib/evidence";

/**
 * Add/remove evidence rows. Each row is exactly the published `EvidenceItem`
 * shape — `type`, `summary`, and an optional `data` JSON object — and nothing
 * more, because `additionalProperties: false` means anything else is a 400.
 */
export function EvidenceEditor({
  drafts,
  onChange,
  errors,
  disabled,
}: {
  drafts: EvidenceDraft[];
  onChange: (next: EvidenceDraft[]) => void;
  errors: EvidenceSerializationError[];
  disabled?: boolean;
}) {
  function update(index: number, patch: Partial<EvidenceDraft>) {
    onChange(drafts.map((draft, i) => (i === index ? { ...draft, ...patch } : draft)));
  }

  function errorFor(index: number, field: EvidenceSerializationError["field"]) {
    return errors.find((error) => error.index === index && error.field === field)?.message;
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="label">Evidence</span>
        <span className="text-[11.5px] text-ink-faint">
          {drafts.length === 0
            ? "Optional, but it is what makes the step reviewable later."
            : `${drafts.length} item${drafts.length === 1 ? "" : "s"}`}
        </span>
        <Button
          type="button"
          variant="quiet"
          className="ml-auto"
          disabled={disabled}
          onClick={() => onChange([...drafts, newEvidenceDraft()])}
        >
          + Add evidence
        </Button>
      </div>

      {drafts.length > 0 && (
        <ul className="space-y-2">
          {drafts.map((draft, index) => (
            <li key={draft.key} className="rounded-[2px] border border-rule bg-sunken p-2.5">
              <div className="mb-2 flex items-center gap-2">
                <span className="label">Item {index + 1}</span>
                <Button
                  type="button"
                  variant="quiet"
                  className="ml-auto !py-0.5 !text-[12px]"
                  disabled={disabled}
                  onClick={() => onChange(drafts.filter((_, i) => i !== index))}
                >
                  Remove
                </Button>
              </div>

              <div className="grid gap-2 sm:grid-cols-[200px_minmax(0,1fr)]">
                <LabelledField label="Type" htmlFor={`${draft.key}-type`}>
                  <TextInput
                    id={`${draft.key}-type`}
                    value={draft.type}
                    disabled={disabled}
                    onChange={(e) => update(index, { type: e.target.value })}
                    className="font-mono !text-[12px]"
                    placeholder="asset_inventory_lookup"
                    aria-invalid={Boolean(errorFor(index, "type"))}
                  />
                  <FieldError message={errorFor(index, "type")} />
                </LabelledField>

                <LabelledField label="Summary" htmlFor={`${draft.key}-summary`}>
                  <TextInput
                    id={`${draft.key}-summary`}
                    value={draft.summary}
                    disabled={disabled}
                    onChange={(e) => update(index, { summary: e.target.value })}
                    placeholder="10.20.1.44 maps to ENG-WS-14"
                    aria-invalid={Boolean(errorFor(index, "summary"))}
                  />
                  <FieldError message={errorFor(index, "summary")} />
                </LabelledField>
              </div>

              <LabelledField
                label="Data (JSON object, optional)"
                htmlFor={`${draft.key}-data`}
                className="mt-2"
              >
                <TextArea
                  id={`${draft.key}-data`}
                  rows={3}
                  value={draft.data}
                  disabled={disabled}
                  onChange={(e) => update(index, { data: e.target.value })}
                  className="font-mono !text-[11.5px]"
                  spellCheck={false}
                  placeholder={'{ "asset": "ENG-WS-14", "trusted": true }'}
                  aria-invalid={Boolean(errorFor(index, "data"))}
                />
                <FieldError message={errorFor(index, "data")} />
              </LabelledField>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className="mt-1 text-[11.5px] leading-snug text-alarm">
      {message}
    </p>
  );
}
