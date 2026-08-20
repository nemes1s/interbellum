/** Display helpers. Nothing here is business logic. */

/** Short form of a UUID for dense tables: first and last group. */
export function shortId(id: string | null | undefined): string {
  if (!id) return "—";
  return id.length <= 13 ? id : `${id.slice(0, 8)}…${id.slice(-4)}`;
}

const TIMESTAMP = new Intl.DateTimeFormat("en-GB", {
  year: "numeric",
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  timeZone: "UTC",
  hour12: false,
});

/** Absolute UTC timestamp. An audit record has no business rendering "3 hours ago". */
export function timestamp(iso: string | null | undefined): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return `${TIMESTAMP.format(date).replace(",", "")} UTC`;
}

/** Duration between two ISO timestamps, e.g. "4m 12s". */
export function duration(from: string, to: string | null): string | null {
  if (!to) return null;
  const ms = new Date(to).getTime() - new Date(from).getTime();
  if (!Number.isFinite(ms) || ms < 0) return null;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** `close_authorized_maintenance` → `Close authorized maintenance`. */
export function humanizeToken(token: string): string {
  const spaced = token.replace(/[_-]+/g, " ").trim();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/**
 * A resolution's disposition, read from the leading verb of the resolution
 * token the *playbook author* chose. Used only to tint the report banner, never
 * to decide anything — an unrecognised prefix simply gets the neutral tint.
 */
export type Disposition = "close" | "escalate" | "contain" | "neutral";

export function dispositionOf(resolution: string | null | undefined): Disposition {
  if (!resolution) return "neutral";
  const head = resolution.split(/[_-]/, 1)[0].toLowerCase();
  if (head === "close") return "close";
  if (head === "escalate") return "escalate";
  if (head === "contain" || head === "isolate") return "contain";
  return "neutral";
}

/** Stable JSON rendering for the console's JSON blocks. */
export function prettyJson(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? "null";
}
