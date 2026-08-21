import * as React from "react";

import { prettyJson, shortId } from "@/lib/format";
import type { JsonObject } from "@/lib/types";

/** A titled panel — the console's only container. */
export function Panel({
  title,
  eyebrow,
  actions,
  children,
  className = "",
  bodyClassName = "p-4",
}: {
  title?: React.ReactNode;
  eyebrow?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      {(title || eyebrow || actions) && (
        <header className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-rule px-4 py-2.5">
          <div className="min-w-0">
            {eyebrow && <p className="label mb-0.5">{eyebrow}</p>}
            {title && <h2 className="truncate text-[13px] font-semibold text-ink">{title}</h2>}
          </div>
          {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={bodyClassName}>{children}</div>
    </section>
  );
}

/** A definition row: mono micro-label above a value. Used for every metadata grid. */
export function Field({
  label,
  children,
  className = "",
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    // min-w-0 so a long monospace value (an alert type, a UUID) wraps inside
    // its own grid column instead of pushing into the next one.
    <div className={`min-w-0 ${className}`}>
      <dt className="label mb-1">{label}</dt>
      <dd className="break-words text-[13px] leading-snug text-ink">{children}</dd>
    </div>
  );
}

const TONES = {
  neutral: "border-rule-strong bg-sunken text-ink-muted",
  signal: "border-signal/35 bg-signal-soft text-signal",
  hold: "border-hold/35 bg-hold-soft text-hold",
  clear: "border-clear/35 bg-clear-soft text-clear",
  alarm: "border-alarm/35 bg-alarm-soft text-alarm",
} as const;

export type Tone = keyof typeof TONES;

/** A status chip. Always mono, always uppercase — it reads as a channel state. */
export function Chip({
  tone = "neutral",
  children,
  className = "",
}: {
  tone?: Tone;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-[2px] border px-1.5 py-[3px] font-mono text-[10px] font-medium uppercase leading-none tracking-[0.08em] ${TONES[tone]} ${className}`}
    >
      {children}
    </span>
  );
}

/** A UUID, rendered short but selectable in full via the title attribute. */
export function Id({ value, className = "" }: { value: string | null | undefined; className?: string }) {
  return (
    <span title={value ?? undefined} className={`font-mono text-[11px] text-ink-muted ${className}`}>
      {shortId(value)}
    </span>
  );
}

/** A readable JSON block. Scrolls inside itself; the page never scrolls sideways. */
export function JsonBlock({
  value,
  className = "",
  maxHeight = "18rem",
}: {
  value: unknown;
  className?: string;
  maxHeight?: string;
}) {
  return (
    <pre
      style={{ maxHeight }}
      className={`overflow-auto rounded-[2px] border border-rule bg-sunken p-2.5 font-mono text-[11.5px] leading-[1.55] text-ink ${className}`}
    >
      {prettyJson(value)}
    </pre>
  );
}

/** JSON that is known to be an object, e.g. an alert payload or evidence data. */
export function ObjectBlock(props: { value: JsonObject | undefined | null; className?: string; maxHeight?: string }) {
  return <JsonBlock {...props} value={props.value ?? {}} />;
}

const BUTTON_VARIANTS = {
  primary:
    "bg-signal text-white border-signal hover:bg-[#1a44bb] disabled:bg-ink-faint disabled:border-ink-faint",
  secondary: "bg-panel text-ink border-rule-strong hover:bg-sunken disabled:text-ink-faint",
  quiet: "bg-transparent text-ink-muted border-transparent hover:bg-sunken hover:text-ink",
} as const;

export function Button({
  variant = "secondary",
  className = "",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: keyof typeof BUTTON_VARIANTS }) {
  return (
    <button
      {...props}
      className={`inline-flex items-center justify-center gap-1.5 rounded-[2px] border px-3 py-1.5 text-[13px] font-medium transition-colors disabled:cursor-not-allowed ${BUTTON_VARIANTS[variant]} ${className}`}
    />
  );
}

/** Text and textarea inputs share one look: hairline box, mono placeholder-free. */
const CONTROL =
  "w-full rounded-[2px] border border-rule bg-panel px-2.5 py-1.5 text-[13px] text-ink placeholder:text-ink-faint focus:border-signal disabled:bg-sunken disabled:text-ink-muted";

export function TextInput({ className = "", ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${CONTROL} ${className}`} />;
}

export function TextArea({ className = "", ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={`${CONTROL} leading-snug ${className}`} />;
}

export function Select({ className = "", ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={`${CONTROL} ${className}`} />;
}

/** A labelled form control. */
export function LabelledField({
  label,
  hint,
  htmlFor,
  children,
  className = "",
}: {
  label: string;
  hint?: React.ReactNode;
  htmlFor?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      <label htmlFor={htmlFor} className="label mb-1 block">
        {label}
      </label>
      {children}
      {hint && <p className="mt-1 text-[11.5px] leading-snug text-ink-faint">{hint}</p>}
    </div>
  );
}

/** Loading placeholder. Deliberately text, not a shimmer. */
export function Loading({ what }: { what: string }) {
  return (
    <p role="status" className="label py-8 text-center">
      Loading {what}…
    </p>
  );
}

/** Empty state — an invitation to act, never just "no data". */
export function Empty({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div className="px-4 py-10 text-center">
      <p className="text-[13px] font-medium text-ink">{title}</p>
      {children && <div className="mx-auto mt-2 max-w-md text-[13px] leading-relaxed text-ink-muted">{children}</div>}
    </div>
  );
}
