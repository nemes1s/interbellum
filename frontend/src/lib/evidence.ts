import type { EvidenceItem, JsonObject } from "./types";

/**
 * Evidence as the form holds it, before it is a contract object.
 *
 * `data` is a string here because it is a textarea the analyst types JSON into:
 * "{" is a legal intermediate state to be in while typing, and it must not
 * blow up the editor. Validation happens once, on submit.
 */
export interface EvidenceDraft {
  /** Stable key for React list rendering; never sent. */
  key: string;
  type: string;
  summary: string;
  data: string;
}

let counter = 0;

export function newEvidenceDraft(seed: Partial<Omit<EvidenceDraft, "key">> = {}): EvidenceDraft {
  counter += 1;
  return {
    key: `ev-${counter}`,
    type: seed.type ?? "",
    summary: seed.summary ?? "",
    data: seed.data ?? "",
  };
}

export interface EvidenceSerializationError {
  index: number;
  field: "type" | "summary" | "data";
  message: string;
}

export type EvidenceSerializationResult =
  | { ok: true; evidence: EvidenceItem[] }
  | { ok: false; errors: EvidenceSerializationError[] };

/**
 * Turn evidence drafts into the array the API accepts, or report every problem.
 *
 * The rules mirror the published `EvidenceItem` schema exactly, and exist here
 * only so the analyst gets an inline message instead of a round trip to a 400:
 * `type` and `summary` are required and non-blank, `data` must be a JSON
 * *object* when present. Nothing beyond that is re-implemented — the server
 * remains the authority.
 *
 * Fully blank rows are dropped rather than reported, because "click add, change
 * your mind" is a normal thing to do and should not need a delete.
 */
export function serializeEvidence(drafts: EvidenceDraft[]): EvidenceSerializationResult {
  const errors: EvidenceSerializationError[] = [];
  const evidence: EvidenceItem[] = [];

  drafts.forEach((draft, index) => {
    const type = draft.type.trim();
    const summary = draft.summary.trim();
    const data = draft.data.trim();

    if (type === "" && summary === "" && data === "") return;

    if (type === "") {
      errors.push({ index, field: "type", message: "Type is required." });
    }
    if (summary === "") {
      errors.push({ index, field: "summary", message: "Summary is required." });
    }

    let parsed: JsonObject | undefined;
    if (data !== "") {
      let value: unknown;
      try {
        value = JSON.parse(data);
      } catch {
        errors.push({ index, field: "data", message: "Data is not valid JSON." });
        return;
      }
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        errors.push({ index, field: "data", message: "Data must be a JSON object, for example {\"asset\": \"ENG-WS-14\"}." });
        return;
      }
      parsed = value as JsonObject;
    }

    if (type === "" || summary === "") return;

    // `data` is omitted rather than sent as {} — the contract marks it
    // optional, and an empty object is a claim that there was detail.
    evidence.push(parsed === undefined ? { type, summary } : { type, summary, data: parsed });
  });

  return errors.length > 0 ? { ok: false, errors } : { ok: true, evidence };
}

/** Pretty-print a JSON object for the evidence editor's textarea. */
export function formatEvidenceData(data: JsonObject | undefined): string {
  return data === undefined ? "" : JSON.stringify(data, null, 2);
}
