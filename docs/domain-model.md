# Domain Model — Indurex Agentic Alert Investigation Engine

This document defines the entities, relationships, lifecycles, and invariants of the
investigation engine, independent of storage or transport concerns. It is the
reference used to derive the database schema (`migrations/`) and the HTTP API
(`api/openapi.yaml`).

## 1. Bounded concepts

There are three loosely-coupled groups of entities:

1. **Playbook design** — `Playbook`, `PlaybookVersion`, `PlaybookNode`, `PlaybookEdge`.
   Authored by a designer, versioned, and published for reuse.
2. **Alerts** — `Alert`. An opaque, domain-agnostic trigger. The engine does not
   interpret `payload`; it exists so playbook authors and evidence can reference
   alert-specific data.
3. **Investigations** — `Investigation`, `InvestigationStep`. The runtime execution
   of one alert against one immutable playbook version, producing an append-only
   audit trail.

None of these groups need to know the internals of the others beyond stable IDs.
`investigation.alert_id` and `investigation.playbook_version_id` are the only
cross-group references.

## 2. Entities

### Playbook

Logical container for a family of versions. Its own fields (`name`,
`description`, `alert_type`) are metadata, not part of the versioned graph —
investigations never read them, only `playbook_version` and its
`nodes`/`edges`, so changing this metadata never affects historical
investigations.

`alert_type` is the exception worth calling out: unlike `name`/`description`
it is load-bearing (it classifies which alerts a playbook is *for*, and is
the field optional auto-resolution would key on when starting an
investigation — see `Alert` below). Changing
it after creation would silently reclassify every existing version, published
ones included, without creating a new version — the one thing §4's versioning
story exists to prevent. **`alert_type` is therefore immutable after
creation.** There is no update endpoint for `Playbook` in v1 at all, so this
is moot in practice today; it's stated explicitly so a future `PATCH
/playbooks/{id}` (for `name`/`description` only) doesn't reopen the question.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| name | string | display metadata |
| description | string | display metadata |
| alert_type | string | immutable after creation; used for optional auto-resolution of a published version for a new alert |
| created_at | timestamptz | |
| updated_at | timestamptz | |

### PlaybookVersion

One immutable (once published) snapshot of a decision graph belonging to a
`Playbook`. This is the unit an `Investigation` binds to.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| playbook_id | UUID | FK → playbook |
| version | integer | monotonically increasing per playbook, assigned on creation |
| status | enum | `draft \| published \| archived` |
| root_node_id | UUID nullable | FK → playbook_node; must be set before publish |
| created_at | timestamptz | |
| published_at | timestamptz nullable | set on publish |

Lifecycle: `draft -> published -> archived`. See §4.

### PlaybookNode

A single point in the decision graph, scoped to exactly one `PlaybookVersion`.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| playbook_version_id | UUID | FK → playbook_version |
| kind | enum | `decision \| terminal` |
| title | string | the question (decision) or outcome name (terminal) |
| description | string | |
| terminal_resolution | string nullable | required iff `kind = terminal`, forbidden otherwise |
| metadata | JSONB nullable | designer-defined hints (e.g. suggested evidence queries); opaque to the engine |

### PlaybookEdge

A directed, labeled choice from one node to another, scoped to exactly one
`PlaybookVersion`.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| playbook_version_id | UUID | FK → playbook_version (denormalized for scoping + composite FK integrity, see schema doc) |
| from_node_id | UUID | FK → playbook_node, same version |
| to_node_id | UUID | FK → playbook_node, same version |
| label | string | e.g. "Yes" / "No"; unique per `from_node_id` |
| description | string nullable | |
| sort_order | integer | display ordering of choices at a node |

### Alert

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| external_id | string nullable | unique when present |
| alert_type | string | correlates to `playbook.alert_type` for optional auto-resolution |
| title | string | |
| description | string | |
| payload | JSONB | domain-specific data; opaque to the engine |
| occurred_at | timestamptz | when the underlying event happened |
| created_at | timestamptz | when it was ingested |

**Idempotent ingestion.** `POST /alerts` with an `external_id` that already
exists returns the existing alert (`200`) rather than creating a duplicate or
erroring (`409`). Chosen over a conflict response because upstream alerting
systems commonly retry ingestion after a timeout with no way to know whether
the original request actually landed — idempotent-by-default avoids forcing
every caller to pre-check existence, consistent with the idempotency
treatment of decisions (§7, invariant 14). The first write wins: a second
`POST` with the same `external_id` but different field values does not
update the stored alert, it just returns what's already there.

### Investigation

The mutable "current state" projection of one run through a playbook version.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| alert_id | UUID | FK → alert |
| playbook_version_id | UUID | FK → playbook_version; must be `published` at start time |
| status | enum | `in_progress \| completed` |
| current_node_id | UUID | FK → playbook_node, same version; convenience projection, not the source of truth |
| started_at | timestamptz | |
| completed_at | timestamptz nullable | |
| final_resolution | string nullable | copied from the terminal node's `terminal_resolution` on completion |

### InvestigationStep

Append-only audit record of one decision applied to an investigation. **This is
the authoritative history** — `investigation.current_node_id` is derived from it
and must never be treated as a substitute for it.

| field | type | notes |
|---|---|---|
| id | UUID | PK |
| investigation_id | UUID | FK → investigation |
| playbook_version_id | UUID | denormalized copy of `investigation.playbook_version_id`; DB-enforced equal to it via a composite FK on `(investigation_id, playbook_version_id)`, and in turn lets `node_id`/`selected_edge_id` take a composite FK proving they belong to the investigation's own version (see §7, invariant 4b) |
| sequence_number | integer | 1-based, strictly increasing per investigation, assigned inside the decision transaction |
| node_id | UUID | the node the decision was made *from* |
| selected_edge_id | UUID | **not nullable.** the edge chosen; determines the resulting node. Every step is a decision — there is no evidence-only/non-advancing step (§8) — so this is always set. A `terminal` root produces zero steps rather than a step with no edge; see §4. |
| actor_type | enum | `human \| agent` |
| actor_id | string nullable | free-form identifier (username, agent version tag) |
| rationale | string nullable | free text justification |
| evidence | JSONB nullable | array of evidence items, see §3 |
| idempotency_key | string nullable | client-supplied key for safe retries, unique per investigation |
| created_at | timestamptz | |

## 3. Value objects

### Actor

Not a standalone table — embedded on `InvestigationStep` as `(actor_type, actor_id)`.
Represents whoever submitted a decision: a human analyst or an automated agent,
identified by an opaque string (username, agent build tag, etc). The engine does
not authenticate or register actors; it only records the claim (see README
"Production considerations" for how authn/z would attach a verified identity here).

### EvidenceItem

Not a standalone table — stored as a JSONB array on `InvestigationStep.evidence`.

```json
{
  "type": "asset_inventory_lookup",
  "summary": "10.20.1.44 maps to ENG-WS-14",
  "data": { "asset": "ENG-WS-14", "trusted": true }
}
```

`type` and `summary` are conventionally present so a UI/report can render a
timeline without understanding every producer's schema; `data` is opaque.
Large binary evidence is out of scope — see §6 below.

## 4. Lifecycles

### PlaybookVersion

```
draft ──publish (validate graph)──▶ published ──(manual/administrative)──▶ archived
  │
  └─(PUT graph while draft)  loops back to draft
```

- Only a `draft` version accepts `PUT` graph replacement.
- `publish` runs graph validation (§5) and, only if it passes, flips status to
  `published`, stamps `published_at`, and freezes the version permanently:
  no further node/edge/root mutation through any API.
- `archived` exists so a designer can retire a version from being selectable for
  *new* investigations while historical investigations that already reference it
  continue to resolve normally. Archiving is out of scope for the v1 API surface
  beyond the status value itself (no dedicated archive endpoint is required by
  the assignment; the column exists so the lifecycle is representable and
  future-proof).
- Editing a published playbook always means: create a new `PlaybookVersion` row
  (via `POST /playbooks/{id}/versions`, optionally cloning an existing version's
  graph as a starting point), never mutate the existing one. This is what makes
  historical investigations auditable independent of later playbook changes.
- **Replacing a draft's graph (`PUT`) and the root FK.** Because
  `playbook_versions.root_node_id` is FK-constrained to a node in that same
  version (§7, invariant 2), and `playbook_edges` rows are FK-constrained to
  the nodes they connect, a whole-graph replace must run, in one transaction,
  in exactly this order:
  1. `UPDATE playbook_versions SET root_node_id = NULL` — frees the old root.
  2. Delete the old `playbook_edges` — children first.
  3. Delete the old `playbook_nodes` — now unreferenced.
  4. Insert the new `playbook_nodes`.
  5. Insert the new `playbook_edges` — the nodes they reference must already exist.
  6. `UPDATE playbook_versions SET root_node_id = <new root>` — or leave it
     `NULL` if the replacement draft doesn't specify one yet (see §5: drafts
     may be incomplete).
  7. Commit.

  Steps 2/3 and 4/5 are ordered by the edge→node FK dependency; step 1 is
  required or step 3 fails with a FK violation on whichever node is still
  the root.

### Investigation

```
in_progress ──(decision reaches a terminal node)──▶ completed
```

- Created only against a `published` playbook version.
- Every decision must originate at `current_node_id`; the server — never the
  client — computes the destination node from the selected edge.
- Once `completed`, the decisions endpoint rejects further submissions (409).
- **Terminal root.** A playbook version's root node can itself be `terminal`
  (a degenerate but valid graph — nothing in §5 forbids a single-node
  playbook). Starting an investigation against one creates it already
  `completed`: `current_node_id` = root, `final_resolution` copied from the
  root's `terminal_resolution`, `completed_at` set, and **zero**
  `investigation_step` rows. This keeps invariant "every step is an edge
  selection" intact (no synthetic zero-edge step is invented just to record
  arrival) at the cost of a report with an empty `path` for such
  investigations.

## 5. Graph validity (enforced at publish time, not at draft-edit time)

Chosen model: **strict DAG (tree-like), cycles rejected.** Simpler to reason
about, matches the assignment's example playbook, and avoids needing traversal
depth guards at decision time. Documented as a deliberate choice over general
graph semantics with cycle-guards, per the assignment's suggested alternatives.

A version can only transition `draft -> published` if all of the following hold:

1. `root_node_id` is set and references a node in this version.
2. Every edge's `from_node_id`/`to_node_id` references a node in this same version
   (enforced by DB constraint at write time, re-checked here for completeness).
3. No dangling references (already implied by FK, listed for validation-report
   completeness).
4. Every `decision` node has ≥1 outgoing edge.
5. Every `terminal` node has 0 outgoing edges.
6. Every `terminal` node has `terminal_resolution` set; every `decision` node has
   it null.
7. Every node is reachable from `root_node_id`.
8. The graph is acyclic.

Validation failures are collected (not fail-fast on the first issue) and returned
as a structured list — see the `PublishValidationError` schema in the OpenAPI
document — so a designer can fix everything in one pass.

Note what is deliberately *not* on that list: duplicate edge labels at a node.
That's rejected earlier and unconditionally, as a `400` on the `PUT` that
would create it, by the DB unique constraint on `(from_node_id, label)` — a
draft graph with that problem can never be persisted, so publish-time
validation never needs to check for it.

**Draft input is intentionally permissive.** Nothing above applies to
`PUT /playbook-versions/{versionId}` or to `POST /playbooks`'s optional
initial `definition` — a draft may be created and edited with zero nodes, or
nodes but `root_node_id: null`. The API schema for a draft graph
(`PlaybookGraphInput`) reflects this: nothing is `required`. All eight checks
above run once, at `/publish`, never on a draft write.

## 6. Evidence and large artifacts (documented, not built)

JSONB is sufficient for the assignment's evidence shape. In production, large
binary evidence (pcaps, screenshots, historian exports) would not be inlined into
`evidence` JSONB; instead the evidence item would carry a reference:

```json
{ "type": "pcap_capture", "summary": "...", "data": { "object_key": "s3://.../..." } }
```

with the actual bytes in object storage (S3-compatible), and the investigation
engine remaining unaware of storage-provider details.

## 7. Key invariants (drive both schema constraints and service-layer checks)

| # | Invariant | Enforced by |
|---|---|---|
| 1 | A published version's graph is immutable | service layer rejects writes when `status != draft` |
| 2 | A published version has exactly one valid root, set before publish | publish validation; root FK requires the node to be in the same version |
| 3 | Decision nodes have ≥1 outgoing edge; terminal nodes have 0 | publish validation |
| 4a | Every edge stays within its playbook version | composite FK on `playbook_edges` (`playbook_version_id, from/to_node_id → playbook_nodes`) |
| 4b | Every investigation step's node/edge belong to the investigation's own playbook version | composite FK on `investigation_steps` (`playbook_version_id, node_id/selected_edge_id → playbook_nodes/playbook_edges`); `playbook_version_id` itself is a denormalized copy of the parent investigation's, and is DB-enforced equal to it via a composite FK `(investigation_id, playbook_version_id) → investigations(id, playbook_version_id)` (backed by a unique constraint on that pair on `investigations`) — see §2 `InvestigationStep` |
| 5 | Terminal resolution set iff node is terminal | DB `CHECK` constraint |
| 6 | Graph is acyclic and fully reachable from root | publish validation (BFS/DFS) |
| 7 | Investigations only start against `published` versions | service layer |
| 8 | `investigation_step` is append-only; never updated/deleted via API | no `PUT`/`DELETE` route exists; DB grants could further restrict in production |
| 9 | `current_node_id` is a projection, `investigation_step` sequence is authoritative | service always derives report/history from steps, not from the pointer |
| 10 | A submitted edge must originate at `current_node_id` | service layer check inside the decision transaction |
| 11 | Concurrent decisions on one investigation cannot both apply | `SELECT ... FOR UPDATE` on the investigation row inside the decision transaction |
| 12 | `sequence_number` is gapless and strictly increasing per investigation | assigned as `MAX(sequence_number)+1` under the same row lock |
| 13 | A decision on a completed investigation is rejected (409) | service layer status check under the same lock |
| 14 | Retrying `Idempotency-Key` with an equivalent request is a no-op (no duplicate step, returns current state); retrying it with a different `edge_id`/`actor`/`rationale`/`evidence` is rejected | unique `(investigation_id, idempotency_key)` catches the duplicate insert; service layer compares the stored step's content against the retry to decide no-op vs. `409 IDEMPOTENCY_KEY_REUSED` |

## 8. What is explicitly NOT modeled

- Authentication/authorization identities (actors are free-form strings; see
  README §Production considerations).
- Node/edge versioning within a draft (a draft is replaced wholesale via `PUT`,
  not patched incrementally).
- Playbook archival workflow beyond the status enum value.
- Evidence-only steps that don't advance the investigation (every
  `investigation_step` corresponds to exactly one edge selection).
