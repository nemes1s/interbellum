import { describe, expect, it } from "vitest";

import { newEvidenceDraft, serializeEvidence } from "@/lib/evidence";

/**
 * The evidence editor holds strings; the API takes typed objects with
 * `additionalProperties: false`. This is the conversion, and getting it wrong
 * means a 400 the analyst cannot act on.
 */
describe("serializeEvidence", () => {
  it("produces the exact contract shape, omitting data when it was left empty", () => {
    const result = serializeEvidence([
      newEvidenceDraft({
        type: "asset_inventory_lookup",
        summary: "10.20.1.44 maps to ENG-WS-14",
        data: '{"asset": "ENG-WS-14", "trusted": true}',
      }),
      newEvidenceDraft({ type: "change_calendar_lookup", summary: "Approved window 10:00-12:00 UTC" }),
    ]);

    expect(result).toEqual({
      ok: true,
      evidence: [
        {
          type: "asset_inventory_lookup",
          summary: "10.20.1.44 maps to ENG-WS-14",
          data: { asset: "ENG-WS-14", trusted: true },
        },
        { type: "change_calendar_lookup", summary: "Approved window 10:00-12:00 UTC" },
      ],
    });
  });

  it("trims whitespace off type and summary", () => {
    const result = serializeEvidence([newEvidenceDraft({ type: "  historian_query  ", summary: " spike at 10:31  " })]);

    expect(result).toEqual({ ok: true, evidence: [{ type: "historian_query", summary: "spike at 10:31" }] });
  });

  it("drops a row the analyst added and left entirely blank", () => {
    const result = serializeEvidence([
      newEvidenceDraft({ type: "register_map_lookup", summary: "40021 is outside the SIS range" }),
      newEvidenceDraft(),
    ]);

    expect(result).toEqual({
      ok: true,
      evidence: [{ type: "register_map_lookup", summary: "40021 is outside the SIS range" }],
    });
  });

  it("serializes an empty editor to an empty array, not to undefined", () => {
    expect(serializeEvidence([])).toEqual({ ok: true, evidence: [] });
  });

  it("reports missing type and summary against the row they belong to", () => {
    const result = serializeEvidence([newEvidenceDraft({ summary: "no type given" }), newEvidenceDraft({ type: "x" })]);

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([
      { index: 0, field: "type", message: expect.stringContaining("Type is required") },
      { index: 1, field: "summary", message: expect.stringContaining("Summary is required") },
    ]);
  });

  it("rejects data that is not parseable JSON", () => {
    const result = serializeEvidence([newEvidenceDraft({ type: "t", summary: "s", data: "{not json" })]);

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors).toEqual([{ index: 0, field: "data", message: "Data is not valid JSON." }]);
  });

  it.each([
    ["an array", "[1, 2, 3]"],
    ["a scalar", "42"],
    ["a string", '"ENG-WS-14"'],
    ["null", "null"],
  ])("rejects data that parses to %s, matching the API's object-only rule", (_label, data) => {
    const result = serializeEvidence([newEvidenceDraft({ type: "t", summary: "s", data })]);

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors[0]).toMatchObject({ index: 0, field: "data" });
    expect(result.errors[0].message).toContain("JSON object");
  });

  it("returns no evidence at all when any row is invalid", () => {
    const result = serializeEvidence([
      newEvidenceDraft({ type: "good", summary: "fine" }),
      newEvidenceDraft({ type: "bad", summary: "also fine", data: "[]" }),
    ]);

    expect(result.ok).toBe(false);
  });
});
