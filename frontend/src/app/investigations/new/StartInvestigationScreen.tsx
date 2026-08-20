"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useState } from "react";

import { ErrorNotice } from "@/components/ErrorNotice";
import {
  Button,
  Chip,
  Empty,
  Field,
  Id,
  JsonBlock,
  LabelledField,
  Loading,
  Panel,
  TextArea,
  TextInput,
} from "@/components/primitives";
import { VersionStatusChip } from "@/components/StatusChips";
import * as api from "@/lib/api";
import { DEMO_ALERT } from "@/lib/demo";
import { prettyJson, timestamp } from "@/lib/format";
import { useResource } from "@/lib/use-resource";
import type { Alert, JsonObject, PlaybookVersionSummary, UUID } from "@/lib/types";

interface AlertForm {
  external_id: string;
  alert_type: string;
  title: string;
  description: string;
  occurred_at: string;
  payload: string;
}

const BLANK: AlertForm = {
  external_id: "",
  alert_type: "",
  title: "",
  description: "",
  occurred_at: "",
  payload: "{}",
};

/** A published version of a playbook that matches the alert's type. */
interface Candidate {
  playbookId: UUID;
  playbookName: string;
  playbookDescription: string;
  version: PlaybookVersionSummary;
}

/**
 * Alert ingestion, then investigation start.
 *
 * Two API calls, in the order the domain requires: an investigation belongs to
 * an alert, and binds to one explicitly chosen published playbook version. The
 * console never guesses the version — the backend deliberately has no
 * auto-resolution, so the choice is recorded as the caller's.
 */
export function StartInvestigationScreen() {
  const router = useRouter();
  const params = useSearchParams();
  const alertId = params.get("alert");
  const preselectedVersion = params.get("version");

  const [form, setForm] = useState<AlertForm>(BLANK);
  const [ingesting, setIngesting] = useState(false);
  const [ingestError, setIngestError] = useState<unknown>(null);
  const [payloadError, setPayloadError] = useState<string | null>(null);
  const [ingestedExisting, setIngestedExisting] = useState(false);

  const [startingVersionId, setStartingVersionId] = useState<UUID | null>(null);
  const [startError, setStartError] = useState<unknown>(null);

  const alert = useResource((signal) => api.getAlert(alertId!, signal), [alertId], { enabled: Boolean(alertId) });

  const field = useCallback(
    <K extends keyof AlertForm>(key: K) =>
      (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
        setForm((current) => ({ ...current, [key]: event.target.value })),
    [],
  );

  function loadDemo() {
    setForm({
      external_id: DEMO_ALERT.external_id,
      alert_type: DEMO_ALERT.alert_type,
      title: DEMO_ALERT.title,
      description: DEMO_ALERT.description,
      // The form works in UTC; `datetime-local` has no zone of its own.
      occurred_at: DEMO_ALERT.occurred_at.slice(0, 16),
      payload: prettyJson(DEMO_ALERT.payload),
    });
    setPayloadError(null);
    setIngestError(null);
  }

  async function ingest(event: React.FormEvent) {
    event.preventDefault();
    setPayloadError(null);
    setIngestError(null);

    let payload: JsonObject = {};
    const raw = form.payload.trim();
    if (raw !== "") {
      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        setPayloadError("Payload is not valid JSON.");
        return;
      }
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
        setPayloadError("Payload must be a JSON object. The API rejects a scalar or an array with a 400.");
        return;
      }
      payload = parsed as JsonObject;
    }

    setIngesting(true);
    try {
      const result = await api.createAlert({
        external_id: form.external_id.trim() === "" ? null : form.external_id.trim(),
        alert_type: form.alert_type.trim(),
        title: form.title.trim(),
        description: form.description.trim(),
        occurred_at: toRfc3339(form.occurred_at),
        payload,
      });
      setIngestedExisting(result.status === 200);
      // The alert id goes in the URL so the page survives a refresh. A version
      // arrived at from the playbook viewer is carried through, so the row the
      // reviewer came here to start stays highlighted.
      const next = new URLSearchParams({ alert: result.data.id });
      if (preselectedVersion) next.set("version", preselectedVersion);
      router.replace(`/investigations/new?${next}`, { scroll: false });
    } catch (err) {
      setIngestError(err);
    } finally {
      setIngesting(false);
    }
  }

  async function start(versionId: UUID) {
    if (!alert.data) return;
    setStartingVersionId(versionId);
    setStartError(null);
    try {
      const investigation = await api.startInvestigation(alert.data.id, { playbook_version_id: versionId });
      router.push(`/investigations/${investigation.id}`);
    } catch (err) {
      setStartError(err);
      setStartingVersionId(null);
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <p className="label mb-1">Step 1 of 2 · ingest, then bind</p>
        <h1 className="text-[19px] font-semibold leading-tight text-ink">Start an investigation</h1>
        <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-ink-muted">
          An alert is an opaque trigger — the engine never interprets its payload. An investigation
          binds that alert to one published playbook version, and a playbook only runs against the
          alert type it was written for.
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* --------------------------------------------------------------- */}
        <Panel
          eyebrow="POST /api/v1/alerts"
          title={alertId ? "Alert ingested" : "Ingest an alert"}
          actions={
            !alertId && (
              <Button variant="secondary" onClick={loadDemo}>
                Load PLC demo alert
              </Button>
            )
          }
        >
          {alertId ? (
            <IngestedAlert
              alert={alert.data}
              loading={alert.loading}
              error={alert.error}
              reload={alert.reload}
              wasExisting={ingestedExisting}
              onIngestAnother={() => {
                setForm(BLANK);
                setIngestedExisting(false);
                setStartError(null);
                router.replace("/investigations/new", { scroll: false });
              }}
            />
          ) : (
            <form onSubmit={ingest} className="space-y-3">
              <div className="grid gap-3 sm:grid-cols-2">
                <LabelledField
                  label="External id"
                  htmlFor="external_id"
                  hint="Optional. Re-ingesting the same id returns the existing alert rather than duplicating it."
                >
                  <TextInput
                    id="external_id"
                    value={form.external_id}
                    onChange={field("external_id")}
                    className="font-mono"
                    placeholder="INTERBELLUM-2026-0001"
                  />
                </LabelledField>
                <LabelledField label="Alert type" htmlFor="alert_type" hint="Must match a playbook's alert type.">
                  <TextInput
                    id="alert_type"
                    required
                    value={form.alert_type}
                    onChange={field("alert_type")}
                    className="font-mono"
                    placeholder="unauthorized_plc_register_write"
                  />
                </LabelledField>
              </div>

              <LabelledField label="Title" htmlFor="title">
                <TextInput id="title" required value={form.title} onChange={field("title")} />
              </LabelledField>

              <LabelledField label="Description" htmlFor="description">
                <TextInput id="description" value={form.description} onChange={field("description")} />
              </LabelledField>

              <LabelledField label="Occurred at (UTC)" htmlFor="occurred_at" hint="When the underlying event happened, not when it was ingested.">
                <TextInput
                  id="occurred_at"
                  type="datetime-local"
                  required
                  step={1}
                  value={form.occurred_at}
                  onChange={field("occurred_at")}
                  className="font-mono"
                />
              </LabelledField>

              <LabelledField
                label="Payload"
                htmlFor="payload"
                hint="Domain-specific JSON object. The engine stores it and hands it back; it never reads it."
              >
                <TextArea
                  id="payload"
                  rows={8}
                  value={form.payload}
                  onChange={field("payload")}
                  className="font-mono !text-[11.5px]"
                  spellCheck={false}
                />
              </LabelledField>

              {payloadError && (
                <p role="alert" className="rounded-[2px] border border-alarm/40 bg-alarm-soft px-2.5 py-1.5 text-[12.5px] text-alarm">
                  {payloadError}
                </p>
              )}
              {ingestError ? <ErrorNotice error={ingestError} /> : null}

              <Button type="submit" variant="primary" disabled={ingesting}>
                {ingesting ? "Ingesting…" : "Ingest alert"}
              </Button>
            </form>
          )}
        </Panel>

        {/* --------------------------------------------------------------- */}
        <Panel
          eyebrow="POST /api/v1/alerts/{alertId}/investigations"
          title="Bind to a published playbook version"
          bodyClassName=""
        >
          {!alert.data ? (
            <Empty title="Ingest an alert first">
              <p>Compatible playbooks are found by matching the alert&apos;s type, so there is nothing to
              list until the alert exists.</p>
            </Empty>
          ) : (
            <CandidateVersions
              alertType={alert.data.alert_type}
              preselectedVersionId={preselectedVersion}
              startingVersionId={startingVersionId}
              startError={startError}
              onStart={start}
            />
          )}
        </Panel>
      </div>
    </div>
  );
}

/** `2026-08-19T10:30` (a UTC-intended local input) → `2026-08-19T10:30:00Z`. */
function toRfc3339(localValue: string): string {
  if (localValue === "") return new Date().toISOString();
  const withSeconds = localValue.length === 16 ? `${localValue}:00` : localValue;
  return `${withSeconds}Z`;
}

function IngestedAlert({
  alert,
  loading,
  error,
  reload,
  wasExisting,
  onIngestAnother,
}: {
  alert: Alert | null;
  loading: boolean;
  error: unknown;
  reload: () => void;
  wasExisting: boolean;
  onIngestAnother: () => void;
}) {
  if (loading) return <Loading what="the alert" />;
  if (error) return <ErrorNotice error={error} onRetry={reload} />;
  if (!alert) return null;

  return (
    <div className="space-y-3">
      {wasExisting && (
        <p className="rounded-[2px] border border-rule bg-sunken px-2.5 py-1.5 text-[12.5px] leading-snug text-ink-muted">
          An alert with this external id already existed, so the API returned it unchanged
          (<span className="font-mono">HTTP 200</span>) instead of creating a duplicate. Ingestion is
          idempotent on <span className="font-mono">external_id</span>.
        </p>
      )}

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
        <Field label="Alert id" className="col-span-2">
          <Id value={alert.id} className="!text-[12px]" />
        </Field>
        <Field label="Title" className="col-span-2">
          {alert.title}
        </Field>
        <Field label="Type" className="col-span-2">
          <Chip tone="neutral">{alert.alert_type}</Chip>
        </Field>
        <Field label="External id">
          <span className="font-mono text-[11.5px]">{alert.external_id ?? "—"}</span>
        </Field>
        <Field label="Occurred at">
          <span className="font-mono text-[11.5px]">{timestamp(alert.occurred_at)}</span>
        </Field>
        <Field label="Ingested at">
          <span className="font-mono text-[11.5px]">{timestamp(alert.created_at)}</span>
        </Field>
        {alert.description && (
          <Field label="Description" className="col-span-2">
            {alert.description}
          </Field>
        )}
      </dl>

      <div>
        <p className="label mb-1">Payload</p>
        <JsonBlock value={alert.payload} maxHeight="14rem" />
      </div>

      <Button variant="quiet" onClick={onIngestAnother}>
        Ingest a different alert
      </Button>
    </div>
  );
}

/**
 * Published versions of playbooks registered for this alert's type.
 *
 * `GET /playbooks?alert_type=` does the type filtering server-side; the version
 * status is read from each playbook's own version list, because a playbook can
 * have drafts and published versions at once.
 */
function CandidateVersions({
  alertType,
  preselectedVersionId,
  startingVersionId,
  startError,
  onStart,
}: {
  alertType: string;
  preselectedVersionId: string | null;
  startingVersionId: UUID | null;
  startError: unknown;
  onStart: (versionId: UUID) => void;
}) {
  const candidates = useResource<Candidate[]>(async (signal) => {
    const { items } = await api.listPlaybooks(alertType, signal);
    const detailed = await Promise.all(items.map((item) => api.getPlaybook(item.id, signal)));
    return detailed.flatMap((playbook) =>
      playbook.versions
        .filter((version) => version.status === "published")
        .sort((a, b) => b.version - a.version)
        .map((version) => ({
          playbookId: playbook.id,
          playbookName: playbook.name,
          playbookDescription: playbook.description,
          version,
        })),
    );
  }, [alertType]);

  if (candidates.loading) return <Loading what="compatible playbooks" />;
  if (candidates.error) return <ErrorNotice error={candidates.error} onRetry={candidates.reload} className="m-3" />;

  if (!candidates.data || candidates.data.length === 0) {
    return (
      <Empty title="No published playbook for this alert type">
        <p>
          Nothing is registered for <span className="font-mono text-[12px]">{alertType}</span> yet.
          Publish a version whose playbook declares that alert type, then come back — the API rejects
          a mismatch with <span className="font-mono text-[12px]">ALERT_TYPE_MISMATCH</span> rather
          than recording an investigation whose questions never applied.
        </p>
      </Empty>
    );
  }

  return (
    <div>
      {startError ? <ErrorNotice error={startError} className="m-3" /> : null}
      <ul className="divide-y divide-rule">
        {candidates.data.map(({ playbookId, playbookName, playbookDescription, version }) => {
          const busy = startingVersionId === version.id;
          return (
            <li
              key={version.id}
              className={`flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3 ${
                preselectedVersionId === version.id ? "bg-signal-soft" : ""
              }`}
            >
              <div className="min-w-0 flex-1">
                <p className="text-[13px] font-medium leading-snug text-ink">
                  {playbookName} <span className="font-mono text-[11.5px] text-ink-muted">v{version.version}</span>
                </p>
                {playbookDescription && (
                  <p className="mt-0.5 text-[12px] leading-snug text-ink-muted">{playbookDescription}</p>
                )}
                <p className="mt-1 flex items-center gap-2">
                  <VersionStatusChip status={version.status} />
                  <span className="font-mono text-[10.5px] text-ink-faint">
                    published {timestamp(version.published_at)}
                  </span>
                </p>
              </div>
              <div className="flex items-center gap-2">
                <a
                  href={`/playbooks?playbook=${playbookId}&version=${version.id}`}
                  className="text-[12.5px] text-ink-muted underline decoration-rule-strong underline-offset-2 hover:text-ink"
                >
                  View graph
                </a>
                <Button variant="primary" onClick={() => onStart(version.id)} disabled={startingVersionId !== null}>
                  {busy ? "Starting…" : "Start investigation"}
                </Button>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
