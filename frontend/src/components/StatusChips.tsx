import { dispositionOf } from "@/lib/format";
import type { InvestigationStatus, PlaybookVersionStatus } from "@/lib/types";

import { Chip, type Tone } from "./primitives";

const VERSION_TONE: Record<PlaybookVersionStatus, Tone> = {
  draft: "hold",
  published: "clear",
  archived: "neutral",
};

export function VersionStatusChip({ status }: { status: PlaybookVersionStatus }) {
  return <Chip tone={VERSION_TONE[status]}>{status}</Chip>;
}

const INVESTIGATION_TONE: Record<InvestigationStatus, Tone> = {
  in_progress: "hold",
  completed: "clear",
};

export function InvestigationStatusChip({ status }: { status: InvestigationStatus }) {
  return <Chip tone={INVESTIGATION_TONE[status]}>{status.replace("_", " ")}</Chip>;
}

const DISPOSITION_TONE: Record<ReturnType<typeof dispositionOf>, Tone> = {
  close: "clear",
  escalate: "hold",
  contain: "alarm",
  neutral: "neutral",
};

/** Tone for a terminal resolution, read from the token the playbook author chose. */
export function resolutionTone(resolution: string | null | undefined): Tone {
  return DISPOSITION_TONE[dispositionOf(resolution)];
}
