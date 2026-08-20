import { JsonBlock } from "@/components/primitives";
import type { EvidenceItem } from "@/lib/types";

/** Recorded evidence, read-only. Used by both the runner history and the report ledger. */
export function EvidenceList({ evidence }: { evidence: EvidenceItem[] }) {
  if (evidence.length === 0) {
    return <p className="text-[12px] text-ink-faint">No evidence recorded for this step.</p>;
  }

  return (
    <ul className="space-y-2">
      {evidence.map((item, index) => (
        <li key={index} className="rounded-[2px] border border-rule bg-sunken px-2.5 py-2">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="font-mono text-[10.5px] font-medium uppercase tracking-[0.07em] text-ink-muted">
              {item.type}
            </span>
            <span className="text-[12.5px] leading-snug text-ink">{item.summary}</span>
          </div>
          {item.data && Object.keys(item.data).length > 0 && (
            <JsonBlock value={item.data} maxHeight="10rem" className="mt-1.5 !bg-panel" />
          )}
        </li>
      ))}
    </ul>
  );
}
