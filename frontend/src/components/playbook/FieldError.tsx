import type { DraftError } from "@/lib/playbook-draft";

/** Inline message for one control. Same shape the evidence editor uses. */
export function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className="mt-1 text-[11.5px] leading-snug text-alarm">
      {message}
    </p>
  );
}

export function errorFor(
  errors: DraftError[],
  scope: DraftError["scope"],
  index: number,
  field: DraftError["field"],
): string | undefined {
  return errors.find(
    (error) => error.scope === scope && error.index === index && error.field === field,
  )?.message;
}
