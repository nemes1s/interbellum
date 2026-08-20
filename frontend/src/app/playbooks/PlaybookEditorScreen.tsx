"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";

import { ErrorNotice } from "@/components/ErrorNotice";
import { PlaybookGraph } from "@/components/graph/PlaybookGraph";
import { EdgeListEditor } from "@/components/playbook/EdgeListEditor";
import { FieldError } from "@/components/playbook/FieldError";
import { NodeListEditor } from "@/components/playbook/NodeListEditor";
import {
  Button,
  Chip,
  Field,
  Id,
  LabelledField,
  Loading,
  Panel,
  Select,
  TextInput,
} from "@/components/primitives";
import { VersionStatusChip } from "@/components/StatusChips";
import * as api from "@/lib/api";
import { timestamp } from "@/lib/format";
import {
  draftFromDefinition,
  EMPTY_DRAFT,
  newEdgeDraft,
  newNodeDraft,
  previewGraph,
  serializePlaybookDraft,
  type DraftError,
  type EdgeDraft,
  type NodeDraft,
  type PlaybookDraft,
} from "@/lib/playbook-draft";
import type { PlaybookNodeKind, UUID } from "@/lib/types";
import { useResource } from "@/lib/use-resource";

export interface PlaybookEditorScreenProps {
  /** Both absent when authoring a brand-new playbook at `/playbooks/new`. */
  playbookId?: UUID;
  versionId?: UUID;
}

/**
 * Playbook authoring.
 *
 * A form, not a canvas. The stored graph carries no coordinates — a playbook is
 * a procedure, not a drawing — so there is nothing to position, and the graph
 * beside the form is a preview that re-lays out from the same auto-layout every
 * other screen uses.
 *
 * The screen is built around the version lifecycle rather than around the form:
 * a draft is saved as a whole graph with one `PUT`, publishing freezes it for
 * good, and the way to change a published version is to start a new draft from
 * it. Publishing is also where the server's validation runs — this form guards
 * only what a *draft write* rejects, and deliberately leaves "is this procedure
 * finished" to the `422`, which reports every problem at once.
 */
export function PlaybookEditorScreen({ playbookId, versionId }: PlaybookEditorScreenProps) {
  const router = useRouter();

  const playbook = useResource((signal) => api.getPlaybook(playbookId!, signal), [playbookId], {
    enabled: Boolean(playbookId),
  });
  const version = useResource((signal) => api.getPlaybookVersion(versionId!, signal), [versionId], {
    enabled: Boolean(versionId),
  });

  // Playbook metadata is only ever set at creation: `alert_type` classifies
  // every version at once, so changing it would silently reclassify published
  // ones (docs/domain-model.md §2), and the contract has no update endpoint.
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [alertType, setAlertType] = useState("");
  const [metadataErrors, setMetadataErrors] = useState<Record<string, string>>({});

  const [draft, setDraft] = useState<PlaybookDraft>(EMPTY_DRAFT);
  const [errors, setErrors] = useState<DraftError[]>([]);
  const [dirty, setDirty] = useState(false);

  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<unknown>(null);
  const [publishing, setPublishing] = useState(false);
  const [publishError, setPublishError] = useState<unknown>(null);
  const [cloning, setCloning] = useState(false);
  const [cloneError, setCloneError] = useState<unknown>(null);
  const [justPublished, setJustPublished] = useState(false);

  // Seed the form from the stored version once. Guarded by the version id
  // rather than by a mount effect so a reload of the same version does not
  // discard edits the designer has since made.
  const seededVersionId = useRef<string | null>(null);
  useEffect(() => {
    const definition = version.data;
    if (!definition || seededVersionId.current === definition.id) return;
    seededVersionId.current = definition.id;
    setDraft(draftFromDefinition(definition));
    setDirty(false);
    setErrors([]);
  }, [version.data]);

  const status = version.data?.status ?? "draft";
  const frozen = Boolean(version.data) && status !== "draft";
  const locked = frozen || saving || publishing || cloning;

  function mutate(next: (current: PlaybookDraft) => PlaybookDraft) {
    setDraft(next);
    setDirty(true);
  }

  function addNode(kind: PlaybookNodeKind) {
    mutate((current) => {
      const node = newNodeDraft({ kind });
      return {
        ...current,
        nodes: [...current.nodes, node],
        // The first node authored is almost always the root, and a graph with
        // no root cannot be published at all.
        rootNodeId: current.rootNodeId === "" && current.nodes.length === 0 ? node.id : current.rootNodeId,
      };
    });
  }

  function updateNode(index: number, patch: Partial<NodeDraft>) {
    mutate((current) => ({
      ...current,
      nodes: current.nodes.map((node, i) => (i === index ? { ...node, ...patch } : node)),
    }));
  }

  function removeNode(index: number) {
    mutate((current) => {
      const removed = current.nodes[index];
      return {
        // A node's choices do not outlive it, and neither does its rootship —
        // both would otherwise be dangling references the write rejects.
        rootNodeId: current.rootNodeId === removed.id ? "" : current.rootNodeId,
        nodes: current.nodes.filter((_, i) => i !== index),
        edges: current.edges.filter(
          (edge) => edge.fromNodeId !== removed.id && edge.toNodeId !== removed.id,
        ),
      };
    });
  }

  function addEdge(fromNodeId: UUID | "" = "") {
    mutate((current) => ({
      ...current,
      edges: [
        ...current.edges,
        newEdgeDraft({ fromNodeId, sortOrder: String(outgoingCount(current.edges, fromNodeId) + 1) }),
      ],
    }));
  }

  function updateEdge(index: number, patch: Partial<EdgeDraft>) {
    mutate((current) => ({
      ...current,
      edges: current.edges.map((edge, i) => {
        if (i !== index) return edge;
        const next = { ...edge, ...patch };
        // Picking the source is what makes an ordering meaningful: the new
        // choice joins the end of that node's list rather than tying with its
        // first, and sort order is what decides left-to-right in the drawing.
        if (patch.fromNodeId !== undefined && edge.sortOrder === "0") {
          next.sortOrder = String(
            outgoingCount(current.edges.filter((_, j) => j !== index), patch.fromNodeId) + 1,
          );
        }
        return next;
      }),
    }));
  }

  function removeEdge(index: number) {
    mutate((current) => ({ ...current, edges: current.edges.filter((_, i) => i !== index) }));
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (locked) return;

    const serialized = serializePlaybookDraft(draft);
    const missing: Record<string, string> = {};
    if (!versionId) {
      if (name.trim() === "") missing.name = "A name is required.";
      if (alertType.trim() === "") missing.alertType = "An alert type is required.";
    }
    setMetadataErrors(missing);
    setErrors(serialized.ok ? [] : serialized.errors);
    if (!serialized.ok || Object.keys(missing).length > 0) return;

    setSaving(true);
    setSaveError(null);
    // A publish rejection describes the graph as it was; this write changes it.
    setPublishError(null);
    try {
      if (versionId) {
        const updated = await api.replacePlaybookVersionGraph(versionId, serialized.graph);
        version.set(updated);
        setDraft(draftFromDefinition(updated));
        setDirty(false);
      } else {
        // One call creates the playbook and its first draft version together.
        const created = await api.createPlaybook({
          name: name.trim(),
          description: description.trim(),
          alert_type: alertType.trim(),
          definition: serialized.graph,
        });
        router.replace(editHref(created.id, created.versions[0].id));
      }
    } catch (err) {
      setSaveError(err);
    } finally {
      setSaving(false);
    }
  }

  async function publish() {
    if (!versionId || locked) return;
    setPublishing(true);
    setPublishError(null);
    setSaveError(null);
    try {
      version.set(await api.publishPlaybookVersion(versionId));
      setJustPublished(true);
      playbook.reload();
    } catch (err) {
      setPublishError(err);
    } finally {
      setPublishing(false);
    }
  }

  async function startNewVersion() {
    if (!playbookId || !versionId || cloning) return;
    setCloning(true);
    setCloneError(null);
    try {
      const next = await api.createPlaybookVersion(playbookId, { clone_from_version_id: versionId });
      router.push(editHref(playbookId, next.id));
    } catch (err) {
      setCloneError(err);
      setCloning(false);
    }
  }

  const preview = useMemo(() => previewGraph(draft), [draft]);
  const decisionCount = draft.nodes.filter((node) => node.kind === "decision").length;

  if (versionId && version.loading && !version.data) return <Loading what="the draft" />;

  return (
    <form onSubmit={save} className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="label mb-1">{versionId ? "Playbook design" : "Playbook design · new"}</p>
          <h1 className="text-[19px] font-semibold leading-tight text-ink">
            {versionId
              ? `${playbook.data?.name ?? "Playbook"} · version ${version.data?.version ?? "—"}`
              : "Design a playbook"}
          </h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-ink-muted">
            A draft may be saved half-finished — that is the point of the draft state. Publishing is
            what runs the graph validation, and it reports every problem it finds in one response
            rather than the first.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {version.data && <VersionStatusChip status={version.data.status} />}
          {!frozen && (
            <Button type="submit" variant="primary" disabled={locked || (Boolean(versionId) && !dirty)}>
              {saving ? "Saving…" : versionId ? "Save draft" : "Create draft"}
            </Button>
          )}
          {versionId && !frozen && (
            <Button type="button" onClick={publish} disabled={locked || dirty}>
              {publishing ? "Publishing…" : "Publish version"}
            </Button>
          )}
          {frozen && playbookId && (
            <Button type="button" variant="primary" onClick={startNewVersion} disabled={cloning}>
              {cloning ? "Creating…" : "Create a new version from this one"}
            </Button>
          )}
          {versionId && (
            <Link
              href={`/playbooks?playbook=${playbookId ?? ""}&version=${versionId}`}
              className="text-[12.5px] text-ink-muted underline decoration-rule-strong underline-offset-2 hover:text-ink"
            >
              View in library
            </Link>
          )}
        </div>
      </div>

      {version.error ? <ErrorNotice error={version.error} onRetry={version.reload} /> : null}
      {saveError ? <ErrorNotice error={saveError} /> : null}
      {publishError ? <ErrorNotice error={publishError} /> : null}
      {cloneError ? <ErrorNotice error={cloneError} /> : null}

      {errors.length > 0 && (
        <p
          role="alert"
          className="rounded-[3px] border border-alarm/40 bg-alarm-soft px-3 py-2 text-[12.5px] leading-snug text-alarm"
        >
          {errors.length} field{errors.length === 1 ? "" : "s"} would be rejected by the draft write
          with a <span className="font-mono">400</span>. Each is marked below. Nothing was sent.
        </p>
      )}

      {frozen && (
        <div className="rounded-[3px] border border-clear/40 bg-clear-soft px-3 py-2.5">
          <p className="text-[13px] font-medium leading-snug text-ink">
            {justPublished ? "Published. This version is now frozen." : "This version is published and frozen."}
          </p>
          <p className="mt-1 text-[12.5px] leading-relaxed text-ink-muted">
            Its graph can never change again — investigations bind to a version, so a report from
            years from now still resolves against exactly this procedure. To change the playbook,
            create a new version seeded from this one.{" "}
            <Link
              href={`/investigations/new?version=${versionId}`}
              className="underline decoration-clear/50 underline-offset-2 hover:text-ink"
            >
              Start an investigation against it
            </Link>
            .
          </p>
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,620px)]">
        {/* ------------------------------------------------------------- */}
        <fieldset disabled={locked} className="min-w-0 space-y-4">
          <Panel
            eyebrow={versionId ? "GET /api/v1/playbooks/{playbookId}" : "POST /api/v1/playbooks"}
            title="Playbook"
          >
            {versionId ? (
              <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
                <Field label="Name" className="col-span-2">
                  {playbook.data?.name ?? "—"}
                </Field>
                <Field label="Alert type">
                  <span className="break-all font-mono text-[12px]">{playbook.data?.alert_type ?? "—"}</span>
                </Field>
                <Field label={version.data?.published_at ? "Published" : "Created"}>
                  <span className="font-mono text-[11.5px]">
                    {timestamp(version.data?.published_at ?? version.data?.created_at)}
                  </span>
                </Field>
                {playbook.data?.description && (
                  <Field label="Description" className="col-span-2 sm:col-span-4">
                    {playbook.data.description}
                  </Field>
                )}
                <Field label="Version id" className="col-span-2 sm:col-span-4">
                  <Id value={versionId} className="!text-[12px]" />
                </Field>
              </dl>
            ) : (
              <div className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <LabelledField label="Name" htmlFor="playbook-name">
                    <TextInput
                      id="playbook-name"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="Unauthorized PLC Register Write"
                      aria-invalid={Boolean(metadataErrors.name)}
                    />
                    <FieldError message={metadataErrors.name} />
                  </LabelledField>
                  <LabelledField
                    label="Alert type"
                    htmlFor="playbook-alert-type"
                    hint="Fixed at creation. It decides which alerts this playbook may run against, for every version at once."
                  >
                    <TextInput
                      id="playbook-alert-type"
                      value={alertType}
                      onChange={(e) => setAlertType(e.target.value)}
                      className="font-mono"
                      placeholder="unauthorized_plc_register_write"
                      aria-invalid={Boolean(metadataErrors.alertType)}
                    />
                    <FieldError message={metadataErrors.alertType} />
                  </LabelledField>
                </div>
                <LabelledField label="Description" htmlFor="playbook-description">
                  <TextInput
                    id="playbook-description"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Triage procedure for an unexpected write to a PLC holding register."
                  />
                </LabelledField>
              </div>
            )}
          </Panel>

          <Panel
            eyebrow={`${decisionCount} decision · ${draft.nodes.length - decisionCount} terminal`}
            title="Nodes"
          >
            <NodeListEditor
              nodes={draft.nodes}
              rootNodeId={draft.rootNodeId}
              errors={errors}
              disabled={locked}
              onAdd={addNode}
              onUpdate={updateNode}
              onRemove={removeNode}
              onAddChoice={addEdge}
            />
          </Panel>

          <Panel eyebrow={`${draft.edges.length} edges`} title="Choices">
            <EdgeListEditor
              edges={draft.edges}
              nodes={draft.nodes}
              errors={errors}
              disabled={locked}
              onAdd={() => addEdge()}
              onUpdate={updateEdge}
              onRemove={removeEdge}
            />
          </Panel>
        </fieldset>

        {/* ------------------------------------------------------------- */}
        <div className="min-w-0">
          <div className="space-y-4 xl:sticky xl:top-[68px]">
            <Panel eyebrow="Auto-laid out, read-only" title="Live preview" bodyClassName="">
              <div className="border-b border-rule p-3">
                <LabelledField
                  label="Root node"
                  htmlFor="root-node"
                  hint="Where every investigation against this version starts. Required to publish, optional to save."
                >
                  <Select
                    id="root-node"
                    value={draft.rootNodeId}
                    disabled={locked}
                    onChange={(e) => mutate((current) => ({ ...current, rootNodeId: e.target.value }))}
                  >
                    <option value="">— no root chosen —</option>
                    {draft.nodes.map((node, index) => (
                      <option key={node.id} value={node.id}>
                        {index + 1}. {node.title.trim() === "" ? "Untitled node" : node.title.trim()}
                      </option>
                    ))}
                  </Select>
                </LabelledField>
              </div>

              <PlaybookGraph
                className="h-[min(66vh,620px)] w-full"
                nodes={preview.nodes}
                edges={preview.edges}
                rootNodeId={draft.rootNodeId === "" ? null : draft.rootNodeId}
              />

              <div className="space-y-1.5 border-t border-rule px-3 py-2.5">
                {dirty && versionId && (
                  <p className="flex items-center gap-2 text-[12px] leading-snug text-ink-muted">
                    <Chip tone="hold">unsaved</Chip>
                    Publish validates the graph the server holds, so save the draft first.
                  </p>
                )}
                <p className="text-[11.5px] leading-relaxed text-ink-faint">
                  Publishing checks what this preview cannot: that a root is set, that every node is
                  reachable from it, that the graph is acyclic, and that every decision node offers a
                  choice while every terminal node offers none.
                </p>
              </div>
            </Panel>
          </div>
        </div>
      </div>
    </form>
  );
}

function editHref(playbookId: UUID, versionId: UUID): string {
  return `/playbooks/${playbookId}/versions/${versionId}/edit`;
}

function outgoingCount(edges: EdgeDraft[], fromNodeId: string): number {
  return fromNodeId === "" ? 0 : edges.filter((edge) => edge.fromNodeId === fromNodeId).length;
}
