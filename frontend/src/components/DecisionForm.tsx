"use client";

import { useCallback, useRef, useState } from "react";

import { ErrorNotice } from "@/components/ErrorNotice";
import { EvidenceEditor } from "@/components/EvidenceEditor";
import { Button, Chip, LabelledField, Select, TextArea, TextInput } from "@/components/primitives";
import * as api from "@/lib/api";
import { suggestedDecision } from "@/lib/demo";
import type { EvidenceDraft, EvidenceSerializationError } from "@/lib/evidence";
import { newEvidenceDraft, serializeEvidence } from "@/lib/evidence";
import type { ActorType, AvailableChoice, InvestigationState, PlaybookNode, SubmitDecisionRequest } from "@/lib/types";

/**
 * Choose an edge, say why, attach evidence, submit.
 *
 * Two things here are load-bearing rather than cosmetic:
 *
 * **The client selects an edge, never a node.** That is the server's safety
 * property, and the form is built so there is no way to express anything else.
 *
 * **The idempotency key identifies the decision, not the HTTP attempt.** A key
 * is minted when the *content* of the pending decision changes, and reused
 * across retries of that same content — so a resubmit after a timeout is a
 * server-side no-op rather than a duplicate step in the audit trail. Editing
 * the rationale, the evidence, the actor or the chosen edge makes it a
 * different decision, and mints a new key.
 */
export function DecisionForm({
  investigationId,
  currentNode,
  choices,
  onApplied,
}: {
  investigationId: string;
  currentNode: PlaybookNode;
  choices: AvailableChoice[];
  onApplied: (state: InvestigationState) => void;
}) {
  const [edgeId, setEdgeId] = useState<string | null>(null);
  const [actorType, setActorType] = useState<ActorType>("human");
  const [actorId, setActorId] = useState("analyst");
  const [rationale, setRationale] = useState("");
  const [evidence, setEvidence] = useState<EvidenceDraft[]>([]);
  const [evidenceErrors, setEvidenceErrors] = useState<EvidenceSerializationError[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<unknown>(null);

  // The key and the request content it was minted for.
  const keyRef = useRef<string | null>(null);
  const keyForRef = useRef<string | null>(null);

  const idempotencyKeyFor = useCallback((body: SubmitDecisionRequest) => {
    const signature = JSON.stringify(body);
    if (keyRef.current === null || keyForRef.current !== signature) {
      keyRef.current = crypto.randomUUID();
      keyForRef.current = signature;
    }
    return keyRef.current;
  }, []);

  const selected = choices.find((choice) => choice.edge_id === edgeId) ?? null;

  function chooseEdge(choice: AvailableChoice) {
    setEdgeId(choice.edge_id);
    setError(null);

    // Pre-fill from the assignment's worked example when this node and choice
    // are part of it, so the reviewer gets a realistic audit trail without
    // inventing OT detail. Never overwrites anything already typed.
    const suggestion = suggestedDecision(currentNode.title, choice.label);
    if (suggestion && rationale.trim() === "" && evidence.length === 0) {
      setRationale(suggestion.rationale);
      setEvidence([newEvidenceDraft(suggestion.evidence)]);
    }
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (submitting || !edgeId) return;

    const serialized = serializeEvidence(evidence);
    if (!serialized.ok) {
      setEvidenceErrors(serialized.errors);
      return;
    }
    setEvidenceErrors([]);

    const body: SubmitDecisionRequest = {
      edge_id: edgeId,
      actor: { type: actorType, id: actorId.trim() === "" ? null : actorId.trim() },
      rationale: rationale.trim() === "" ? null : rationale.trim(),
      evidence: serialized.evidence,
    };

    setSubmitting(true);
    setError(null);
    try {
      const next = await api.submitDecision(investigationId, body, idempotencyKeyFor(body));
      // The decision landed: the next one is a different intent.
      keyRef.current = null;
      keyForRef.current = null;
      setEdgeId(null);
      setRationale("");
      setEvidence([]);
      onApplied(next);
    } catch (err) {
      // The key is deliberately kept: pressing submit again retries the same
      // decision rather than recording a second one.
      setError(err);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <fieldset disabled={submitting} className="space-y-4">
        <div>
          <p className="label mb-1.5">Choices from this node</p>
          <ul className="grid gap-2 sm:grid-cols-2">
            {choices.map((choice) => {
              const active = choice.edge_id === edgeId;
              return (
                <li key={choice.edge_id}>
                  <button
                    type="button"
                    onClick={() => chooseEdge(choice)}
                    aria-pressed={active}
                    className={`w-full rounded-[2px] border px-3 py-2.5 text-left transition-colors ${
                      active
                        ? "border-signal bg-signal-soft"
                        : "border-rule-strong bg-panel hover:border-ink-faint hover:bg-sunken"
                    } disabled:opacity-60`}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className={`inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full border ${
                          active ? "border-signal bg-signal" : "border-rule-strong"
                        }`}
                        aria-hidden
                      >
                        {active && <span className="h-1.5 w-1.5 rounded-full bg-white" />}
                      </span>
                      <span className={`text-[14px] font-semibold ${active ? "text-signal" : "text-ink"}`}>
                        {choice.label}
                      </span>
                    </span>
                    {choice.description && (
                      <span className="mt-1 block pl-6 text-[12px] leading-snug text-ink-muted">
                        {choice.description}
                      </span>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>

        <div className="grid gap-3 sm:grid-cols-[160px_minmax(0,1fr)]">
          <LabelledField label="Actor type" htmlFor="actor-type">
            <Select
              id="actor-type"
              value={actorType}
              onChange={(e) => setActorType(e.target.value as ActorType)}
            >
              <option value="human">human</option>
              <option value="agent">agent</option>
            </Select>
          </LabelledField>
          <LabelledField
            label="Actor id"
            htmlFor="actor-id"
            hint="Self-reported here. Behind an authenticating gateway this would come from the principal, not the request."
          >
            <TextInput
              id="actor-id"
              value={actorId}
              onChange={(e) => setActorId(e.target.value)}
              className="font-mono"
              placeholder="analyst / investigation-agent-v1"
            />
          </LabelledField>
        </div>

        <LabelledField
          label="Rationale"
          htmlFor="rationale"
          hint="Free text. This is the sentence an auditor reads years from now."
        >
          <TextArea
            id="rationale"
            rows={3}
            value={rationale}
            onChange={(e) => setRationale(e.target.value)}
            placeholder="Why this choice, given the evidence gathered."
          />
        </LabelledField>

        <EvidenceEditor
          drafts={evidence}
          onChange={setEvidence}
          errors={evidenceErrors}
          disabled={submitting}
        />
      </fieldset>

      {error ? <ErrorNotice error={error} /> : null}

      <div className="flex flex-wrap items-center gap-3 border-t border-rule pt-3">
        <Button type="submit" variant="primary" disabled={submitting || !edgeId}>
          {submitting ? "Recording decision…" : "Record decision"}
        </Button>
        {selected ? (
          <p className="text-[12.5px] text-ink-muted">
            Selecting edge <Chip tone="signal">{selected.label}</Chip>{" "}
            <span className="font-mono text-[11px] text-ink-faint">{selected.edge_id}</span>. The server
            derives the next node.
          </p>
        ) : (
          <p className="text-[12.5px] text-ink-faint">Pick a choice above to enable the submission.</p>
        )}
        {submitting && (
          <p role="status" className="label ml-auto !text-signal">
            Submitting — controls locked
          </p>
        )}
      </div>
    </form>
  );
}
