"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import { ErrorNotice } from "@/components/ErrorNotice";
import { PlaybookGraph } from "@/components/graph/PlaybookGraph";
import {
  Button,
  Chip,
  Empty,
  Field,
  Id,
  JsonBlock,
  Loading,
  Panel,
} from "@/components/primitives";
import { VersionStatusChip } from "@/components/StatusChips";
import * as api from "@/lib/api";
import { buildDemoPlaybookRequest } from "@/lib/demo";
import { humanizeToken, timestamp } from "@/lib/format";
import { useResource } from "@/lib/use-resource";
import type { PlaybookVersionSummary, UUID } from "@/lib/types";

/**
 * Playbook viewer.
 *
 * Selection lives in the URL (`?playbook=…&version=…`) so a reviewer can link
 * straight to a specific graph. This screen reads playbooks; the only write it
 * offers is `publish`, because a draft that cannot be published is not usable
 * for anything else on the console.
 */
export function PlaybooksScreen() {
  const router = useRouter();
  const params = useSearchParams();
  const playbookId = params.get("playbook");
  const versionId = params.get("version");

  const playbooks = useResource((signal) => api.listPlaybooks(undefined, signal), []);
  const playbook = useResource((signal) => api.getPlaybook(playbookId!, signal), [playbookId], {
    enabled: Boolean(playbookId),
  });
  const version = useResource((signal) => api.getPlaybookVersion(versionId!, signal), [versionId], {
    enabled: Boolean(versionId),
  });

  const [inspectedNodeId, setInspectedNodeId] = useState<UUID | null>(null);
  const [installing, setInstalling] = useState(false);
  const [installError, setInstallError] = useState<unknown>(null);
  const [publishing, setPublishing] = useState(false);
  const [publishError, setPublishError] = useState<unknown>(null);

  const select = useCallback(
    (nextPlaybook: string | null, nextVersion: string | null) => {
      const next = new URLSearchParams();
      if (nextPlaybook) next.set("playbook", nextPlaybook);
      if (nextVersion) next.set("version", nextVersion);
      router.replace(next.size > 0 ? `/playbooks?${next}` : "/playbooks", { scroll: false });
    },
    [router],
  );

  // Land on something useful: the first playbook, and its newest version.
  const items = playbooks.data?.items;
  useEffect(() => {
    if (!playbookId && items && items.length > 0) select(items[0].id, null);
  }, [items, playbookId, select]);

  const versions = playbook.data?.versions;
  useEffect(() => {
    if (!playbookId || !versions || versions.length === 0) return;
    if (versions.some((v) => v.id === versionId)) return;

    // Prefer the newest *published* version: a newer draft is often empty, and
    // opening on an empty canvas is a worse answer to "show me this playbook".
    const byVersion = [...versions].sort((a, b) => b.version - a.version);
    const landing = byVersion.find((v) => v.status === "published") ?? byVersion[0];
    select(playbookId, landing.id);
  }, [versions, versionId, playbookId, select]);

  useEffect(() => setInspectedNodeId(null), [versionId]);

  const inspectedNode = useMemo(
    () => version.data?.nodes.find((node) => node.id === inspectedNodeId) ?? null,
    [version.data, inspectedNodeId],
  );

  async function installDemoPlaybook() {
    setInstalling(true);
    setInstallError(null);
    try {
      // The normal API, twice: create the draft, then publish it. Nothing is
      // seeded behind the console's back.
      const created = await api.createPlaybook(buildDemoPlaybookRequest());
      const draft = created.versions[0];
      await api.publishPlaybookVersion(draft.id);
      playbooks.reload();
      select(created.id, draft.id);
    } catch (err) {
      setInstallError(err);
    } finally {
      setInstalling(false);
    }
  }

  async function publish(id: UUID) {
    setPublishing(true);
    setPublishError(null);
    try {
      version.set(await api.publishPlaybookVersion(id));
      playbook.reload();
    } catch (err) {
      setPublishError(err);
    } finally {
      setPublishing(false);
    }
  }

  const sortedVersions: PlaybookVersionSummary[] = useMemo(
    () => [...(playbook.data?.versions ?? [])].sort((a, b) => b.version - a.version),
    [playbook.data],
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="label mb-1">Playbook library</p>
          <h1 className="text-[19px] font-semibold leading-tight text-ink">
            Versioned decision graphs
          </h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-ink-muted">
            A published version is frozen for good. Investigations bind to a version, never to a
            playbook, which is why a report from last year still resolves against the exact procedure
            it ran on.
          </p>
        </div>
        {items && items.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="secondary" onClick={installDemoPlaybook} disabled={installing}>
              {installing ? "Installing…" : "Install PLC demo playbook"}
            </Button>
            <Link
              href="/playbooks/new"
              className="inline-flex items-center rounded-[2px] border border-signal bg-signal px-3 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-[#1a44bb]"
            >
              Design a playbook
            </Link>
          </div>
        )}
      </div>

      {installError ? <ErrorNotice error={installError} /> : null}

      <div className="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
        {/* ---------------------------------------------------------------- */}
        <div className="space-y-4">
          <Panel eyebrow="GET /api/v1/playbooks" title="Playbooks" bodyClassName="">
            {playbooks.loading && <Loading what="playbooks" />}
            {playbooks.error ? <ErrorNotice error={playbooks.error} onRetry={playbooks.reload} className="m-3" /> : null}
            {items && items.length === 0 && (
              <Empty title="No playbooks yet">
                <p>
                  Install the assignment&apos;s PLC example to get a publishable graph and a runnable
                  investigation in one step, or author one from scratch.
                </p>
                <div className="mt-3 flex flex-wrap justify-center gap-2">
                  <Button variant="primary" onClick={installDemoPlaybook} disabled={installing}>
                    {installing ? "Installing…" : "Install PLC demo playbook"}
                  </Button>
                  <Link
                    href="/playbooks/new"
                    className="inline-flex items-center rounded-[2px] border border-rule-strong bg-panel px-3 py-1.5 text-[13px] font-medium text-ink transition-colors hover:bg-sunken"
                  >
                    Design a playbook
                  </Link>
                </div>
              </Empty>
            )}
            {items && items.length > 0 && (
              <ul className="divide-y divide-rule">
                {items.map((item) => {
                  const active = item.id === playbookId;
                  return (
                    <li key={item.id}>
                      <button
                        onClick={() => select(item.id, null)}
                        aria-current={active ? "true" : undefined}
                        className={`w-full px-3 py-2.5 text-left transition-colors ${
                          active ? "bg-signal-soft" : "hover:bg-sunken"
                        }`}
                      >
                        <p className={`text-[13px] font-medium leading-snug ${active ? "text-signal" : "text-ink"}`}>
                          {item.name}
                        </p>
                        <p className="mt-1 break-all font-mono text-[10.5px] text-ink-faint">
                          {item.alert_type}
                        </p>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </Panel>

          {sortedVersions.length > 0 && (
            <Panel eyebrow="Versions" title={playbook.data?.name} bodyClassName="">
              <ul className="divide-y divide-rule">
                {sortedVersions.map((v) => {
                  const active = v.id === versionId;
                  return (
                    <li key={v.id}>
                      <button
                        onClick={() => select(playbookId, v.id)}
                        aria-current={active ? "true" : undefined}
                        className={`flex w-full items-center gap-2 px-3 py-2 text-left transition-colors ${
                          active ? "bg-signal-soft" : "hover:bg-sunken"
                        }`}
                      >
                        <span className={`font-mono text-[12px] ${active ? "text-signal" : "text-ink"}`}>
                          v{v.version}
                        </span>
                        <VersionStatusChip status={v.status} />
                        <span className="ml-auto font-mono text-[10px] text-ink-faint">
                          {timestamp(v.published_at ?? v.created_at).slice(0, 11)}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </Panel>
          )}
        </div>

        {/* ---------------------------------------------------------------- */}
        <div className="min-w-0 space-y-4">
          {!versionId && !playbooks.loading && (
            <Panel>
              <Empty title="Select a playbook version">
                <p>Its decision graph will be laid out here automatically.</p>
              </Empty>
            </Panel>
          )}

          {version.error ? <ErrorNotice error={version.error} onRetry={version.reload} /> : null}
          {publishError ? <ErrorNotice error={publishError} /> : null}

          {version.data && (
            <>
              <Panel
                eyebrow={`GET /api/v1/playbook-versions/${version.data.id.slice(0, 8)}…`}
                title={`${playbook.data?.name ?? "Playbook"} · version ${version.data.version}`}
                actions={
                  <>
                    <VersionStatusChip status={version.data.status} />
                    <Link
                      href={`/playbooks/${version.data.playbook_id}/versions/${version.data.id}/edit`}
                      className="inline-flex items-center rounded-[2px] border border-rule-strong bg-panel px-3 py-1.5 text-[13px] font-medium text-ink transition-colors hover:bg-sunken"
                    >
                      {version.data.status === "draft" ? "Edit draft" : "Open in editor"}
                    </Link>
                    {version.data.status === "draft" && (
                      <Button variant="primary" onClick={() => publish(version.data!.id)} disabled={publishing}>
                        {publishing ? "Publishing…" : "Publish version"}
                      </Button>
                    )}
                    {version.data.status === "published" && (
                      <Link
                        href={`/investigations/new?version=${version.data.id}`}
                        className="inline-flex items-center rounded-[2px] border border-rule-strong bg-panel px-3 py-1.5 text-[13px] font-medium text-ink transition-colors hover:bg-sunken"
                      >
                        Start an investigation
                      </Link>
                    )}
                  </>
                }
                bodyClassName=""
              >
                <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-b border-rule px-4 py-3 sm:grid-cols-4">
                  <Field label="Alert type">
                    <span className="break-all font-mono text-[12px]">
                      {playbook.data?.alert_type ?? "—"}
                    </span>
                  </Field>
                  <Field label="Nodes">
                    {version.data.nodes.filter((n) => n.kind === "decision").length} decision ·{" "}
                    {version.data.nodes.filter((n) => n.kind === "terminal").length} terminal
                  </Field>
                  <Field label="Choices">{version.data.edges.length} edges</Field>
                  <Field label={version.data.published_at ? "Published" : "Created"}>
                    <span className="font-mono text-[11.5px]">
                      {timestamp(version.data.published_at ?? version.data.created_at)}
                    </span>
                  </Field>
                </dl>

                <PlaybookGraph
                  className="h-[min(62vh,640px)] w-full"
                  nodes={version.data.nodes}
                  edges={version.data.edges}
                  rootNodeId={version.data.root_node_id}
                  onNodeClick={setInspectedNodeId}
                  selectedNodeId={inspectedNodeId}
                />

                <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 border-t border-rule px-4 py-2.5">
                  <span className="label">Legend</span>
                  <LegendSwatch accent="var(--color-ink-muted)" label="Decision — a question with labelled choices" />
                  <LegendSwatch accent="var(--color-ink)" label="Terminal — carries the resolution" />
                  <span className="text-[11.5px] text-ink-faint">Click a node to inspect its metadata.</span>
                </div>
              </Panel>

              {inspectedNode && (
                <Panel
                  eyebrow={inspectedNode.kind}
                  title={inspectedNode.title}
                  actions={
                    <Button variant="quiet" onClick={() => setInspectedNodeId(null)}>
                      Close
                    </Button>
                  }
                >
                  <dl className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_260px]">
                    <div className="space-y-3">
                      <Field label="Node id">
                        <Id value={inspectedNode.id} className="!text-[12px]" />
                      </Field>
                      {inspectedNode.description && (
                        <Field label="Description">{inspectedNode.description}</Field>
                      )}
                      {inspectedNode.terminal_resolution && (
                        <Field label="Terminal resolution">
                          <Chip tone="neutral">{inspectedNode.terminal_resolution}</Chip>
                          <p className="mt-1 text-ink-muted">{humanizeToken(inspectedNode.terminal_resolution)}</p>
                        </Field>
                      )}
                      {inspectedNode.kind === "decision" && (
                        <Field label="Choices from here">
                          <ul className="mt-0.5 space-y-1">
                            {version.data.edges
                              .filter((edge) => edge.from_node_id === inspectedNode.id)
                              .sort((a, b) => a.sort_order - b.sort_order)
                              .map((edge) => (
                                <li key={edge.id} className="flex items-baseline gap-2">
                                  <Chip tone="signal">{edge.label}</Chip>
                                  <span className="text-[12.5px] text-ink-muted">
                                    {edge.description ?? "→"}{" "}
                                    {version.data!.nodes.find((n) => n.id === edge.to_node_id)?.title}
                                  </span>
                                </li>
                              ))}
                          </ul>
                        </Field>
                      )}
                    </div>
                    <Field label="Designer metadata">
                      {inspectedNode.metadata ? (
                        <JsonBlock value={inspectedNode.metadata} maxHeight="12rem" />
                      ) : (
                        <p className="text-ink-faint">None. The engine never reads this field either way.</p>
                      )}
                    </Field>
                  </dl>
                </Panel>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function LegendSwatch({ accent, label }: { accent: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[11.5px] text-ink-muted">
      <span className="h-3 w-[3px]" style={{ background: accent }} aria-hidden />
      {label}
    </span>
  );
}
