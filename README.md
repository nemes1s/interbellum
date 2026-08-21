# Interbellum — Agentic Alert Investigation Engine

A backend for running structured, fully auditable security-alert
investigations in industrial/OT environments.

A security engineer authors a **playbook**: a versioned decision graph of
questions and outcomes. When an **alert** arrives, an analyst or an automated
agent starts an **investigation** against a published version of that playbook
and works through it one decision at a time. Every decision is recorded with
the node it was made at, the choice taken, who or what took it, why, and the
evidence gathered — so a completed investigation can be replayed in full,
years later, exactly as it happened.

A human UI and an automated agent use **the same API**. There is no separate
agent surface, and no LLM is embedded in the engine.

---

## Contents

- [Quick start](#quick-start)
- [Web UI](#web-ui)
- [Walkthrough](#walkthrough)
- [Architecture](#architecture)
- [Key decisions and trade-offs](#key-decisions-and-trade-offs)
- [API](#api)
- [Testing](#testing)
- [Configuration](#configuration)
- [Production considerations](#production-considerations)
- [What was intentionally left out](#what-was-intentionally-left-out)

---

## Quick start

Requires Docker. Nothing else.

```bash
docker compose up --build
```

That starts PostgreSQL, the API and the web console. The API applies its own
database migrations at startup and logs what it applied — there is no
migration sidecar and no setup script. When it is ready:

```bash
curl -s localhost:8080/readyz
# {"status":"ok"}
```

| | |
|---|---|
| Web console | <http://localhost:3000> |
| API | <http://localhost:8080> |

To tear it down, including the database volume:

```bash
docker compose down -v
```

<details>
<summary>Using Podman instead of Docker</summary>

Everything works unchanged — the Dockerfile and compose file use no
Docker-specific features. Substitute `podman` for `docker` (verified against
Podman 5.7 with `podman-compose`):

```bash
podman compose up --build -d
podman compose down -v
```

The Makefile targets take a `COMPOSE` override:

```bash
make COMPOSE="podman compose" docker-up
make COMPOSE="podman compose" docker-down
```

A `docker` → `podman` shell alias also works, and makes every command in this
README copy-pasteable as written.

Note that `make docker-up` polls `/readyz` in a loop rather than using
`docker compose up --wait`: the flag is Compose-v2 only and `podman-compose`
rejects it, and polling waits on the API actually answering rather than on the
container merely having started.

</details>

### Running without Docker

Needs Go 1.25+ and a reachable PostgreSQL.

```bash
make docker-up-db      # or bring your own PostgreSQL
make run               # applies migrations, then serves on :8080
```

`make help` lists every available target.

---

## Web UI

```bash
docker compose up --build
```

then open <http://localhost:3000>.

The console is an **optional client of the same API an automated agent calls**.
It has no private endpoints, no server-side shortcuts, and no local state that
the backend does not already hold: every screen is one or two calls from
[`api/openapi.yaml`](api/openapi.yaml). Deleting `frontend/` would leave the
backend exactly as it is.

### The demo flow

**Playbook → Alert → Investigation → Decisions → Report**

Below is the whole flow as a reviewer actually clicks it, on a fresh database.
Every screen is one or two calls from the published contract, and every id lives
in the URL — so any of these pages can be refreshed, bookmarked or shared, and
nothing correctness-bearing is kept in the browser.

On a fresh database `/playbooks` opens empty with a shortcut, **Install PLC demo
playbook**, which creates the assignment's example tree with `POST /playbooks`
and publishes it — two ordinary API calls, with fresh ids so it is repeatable.
The walkthrough below skips it and authors a playbook by hand instead, because
that is the part worth seeing.

---

#### 1. Design a playbook — `/playbooks/new`

![The authoring screen: a Playbook metadata panel and a node list on the left, a live auto-laid-out graph preview on the right, with the root node picker above it](docs/screenshots/01-design-playbook.jpg)

A form, not a canvas. Nodes and edges are typed into lists, edge endpoints are
selects over the nodes that exist, and the graph on the right is a read-only
preview that re-lays out as the draft changes — the same auto-layout every other
screen uses. **Save draft** replaces the whole graph with one `PUT
/playbook-versions/{id}`.

The draft above is deliberately unfinished: the designer added a last question
("Source seen on this segment before?") and its two outcomes, but has not wired
them up yet. Notice the preview shows those three nodes floating unattached at
the top left. Saving that is fine — an incomplete draft is a normal state.

#### 2. Publish, and get every problem at once

![The same screen after pressing Publish version, showing an INVALID_PLAYBOOK_GRAPH / HTTP 422 panel listing three graph problems: one decision node offering no choices and two nodes not reachable from the root](docs/screenshots/02-publish-validation.jpg)

Publishing is what runs the graph validation, and it reports **every** problem it
found in one response rather than stopping at the first: here, the unwired
decision node offering no choices, plus both of its orphaned outcomes being
unreachable from the root. Each is a `node_id` the designer can go and fix in one
pass. Nothing was published — a failed publish changes no state at all.

#### 3. Publish again, and the version freezes

![The editor after a successful publish: a green banner reading "Published. This version is now frozen.", a PUBLISHED chip, a "Create a new version from this one" button, and the complete graph in the preview](docs/screenshots/03-published-frozen.jpg)

With the last two edges added, the same `POST /playbook-versions/{id}/publish`
succeeds. The version is now immutable: the form goes read-only and the only
remaining action is **Create a new version from this one**, which is `POST
/playbooks/{id}/versions` with `clone_from_version_id`. That is how a published
playbook is "edited" — never by mutating it. See
[Authoring a playbook](#authoring-a-playbook) for why.

#### 4. The library — `/playbooks`

![The playbook library: a sidebar listing playbooks and their versions, and the published decision graph laid out automatically with labelled Yes/No edges](docs/screenshots/04-playbook-library.jpg)

The published graph, laid out with dagre and drawn with React Flow. The layout is
computed on every render rather than stored — a playbook is a procedure, not a
drawing, so the backend deliberately persists no coordinates. Edge ordering
(`sort_order`, then label) is what puts "Yes" consistently left of "No", so the
same version always draws identically. Click any node to inspect its metadata
and outgoing choices.

#### 5. Ingest an alert and bind it to a version — `/investigations/new`

![The start-investigation screen: the ingested alert with its id, type, external id and JSON payload on the left, and on the right the one published playbook version whose alert type matches, with a Start investigation button](docs/screenshots/05-ingest-and-bind.jpg)

Two calls, in the order the domain requires. `POST /alerts` first — ingestion is
idempotent on `external_id`, so re-submitting the same one returns the existing
alert (`200`) instead of duplicating it. Then the console lists **published**
versions of playbooks registered for this alert's type, and choosing one calls
`POST /alerts/{alertId}/investigations`.

The console never guesses the version: the backend has no auto-resolution by
`alert_type` on purpose, so the version an investigation is bound to is always
the caller's recorded choice rather than whatever happened to be published at
that instant. (**Load PLC demo alert** fills this form with the fixture values
if you took the demo-playbook shortcut instead of authoring one.)

#### 6. Answer the questions — `/investigations/{id}`

![The investigation runner: the current decision node's question, two choices with No selected, actor type and id, a rationale textarea, and an evidence item with type, summary and JSON data](docs/screenshots/06-investigation-runner.jpg)

Everything on this screen comes from the single `GET /investigations/{id}`
response an automated agent would read: the current question, the choices
available from it, the alert payload, and the history so far.

The submission selects an **`edge_id`, never a node** — the server derives where
that leads. That is the backend's safety property, and the form is built so
there is no way to express anything else. Each decision carries an actor, a
free-text rationale, and evidence items, and mints an `Idempotency-Key`
identifying the *decision* rather than the HTTP attempt, so retrying after a
timeout is a server-side no-op instead of a duplicate row in the audit trail.

![The runner after the last decision: a completed banner showing the terminal resolution escalate_possible_intrusion, the outcome node, and the append-only history of three recorded decisions](docs/screenshots/07-resolution-reached.jpg)

Reaching a terminal node completes the investigation and copies its
`terminal_resolution` out as the final outcome. The history panel is append-only
— nothing in the API can rewrite or delete a recorded step.

#### 7. Read the report — `/investigations/{id}/report`

![The investigation report: the canonical playbook graph with the route taken drawn over it as a heavy blue line with numbered steps, untaken branches dropped to hairline dashes, and the final resolution in a banner](docs/screenshots/08-report-path.jpg)

This is the screen worth opening deliberately, because it is the backend's
central design decision made visible: `GET /report` returns the canonical graph
and the ordered `path` as **separate** fields, and the console joins them. The
graph carries no `was_visited` flags and is never mutated — the heavy blue
conductor with its step numbers, and the hairline dashes on branches not taken,
are both derived from `path` alone.

![The audit timeline beneath the report: each decision numbered, showing the node, the selected edge, the actor, the rationale, the evidence with its JSON data, and the node and edge UUIDs](docs/screenshots/09-audit-timeline.jpg)

Beneath it, the same three steps as an auditor's timeline: which question, which
edge was selected and where it led, who selected it, why, and on what evidence —
with the node and edge UUIDs, so any line can be traced back to the frozen
version it ran against.

### Authoring a playbook

The authoring screen is a **form, not a canvas**: no node dragging, no
edge-drawing by hand, no positions to save. That is not a shortcut — the stored
graph carries no coordinates, because a playbook is a procedure rather than a
drawing, so there is nothing for a canvas to persist. Nodes and edges are typed
into lists, endpoints are chosen from selects over the nodes that exist, and the
graph beside the form is a preview drawn by the same auto-layout every other
screen uses. IDs are minted client-side with `crypto.randomUUID()`, which the
contract allows so a whole graph and its internal references can be authored in
one request.

Steps 1–3 of [the demo flow](#the-demo-flow) show it working. What that walk
through does not spell out is **which rule is enforced when**, which is the whole
reason the screen behaves as it does:

- *Write time.* The form guards only what a draft `PUT` itself rejects with a
  `400` — a node with no title, a terminal without a resolution or a decision
  with one, two edges leaving a node under the same label, metadata that is not
  a JSON object — and marks each inline without sending anything. The
  "resolution iff terminal" rule holds by construction rather than by
  validation: the field renders only on a terminal node and is only ever
  serialized for one.
- *Publish time.* Root set, full reachability, acyclicity, and every decision
  node having a choice while every terminal has none. All of it is left to the
  server, and its `422` — rendered by the same `ErrorNotice` every other screen
  uses — is the point of the screen rather than something to pre-empt.
- *Never.* Completeness. A draft with no root, or no nodes at all, saves
  happily; `PlaybookGraphInput` marks nothing required for exactly this reason.

`alert_type` is settable only at creation. It classifies every version of the
playbook at once, so changing it later would silently reclassify published ones
without creating a new version (docs/domain-model.md §2), and the API has no
endpoint for it.

The edit route also checks that the version in the path really belongs to the
playbook in the path, and refuses rather than mislabels — otherwise a version of
one playbook could open under another's name while a whole-graph `PUT` went to
the first.

### How the report page is built

Step 7 of [the demo flow](#the-demo-flow) shows the result. The mechanism behind
it is the backend's central design decision:

> canonical immutable graph + append-only investigation path = auditable report

`GET /investigations/{id}/report` returns the playbook graph and the ordered
`path` as **separate** fields — the graph carries no `was_visited` flags. The
console joins them in one small pure function
(`frontend/src/lib/report-path.ts`), which is the only place highlighting is
decided: visited nodes come from `path[].node_id` plus the destination of the
last selected edge, and highlighted edges come from `path[].selected_edge_id`.
The graph is never mutated.

There is exactly one case the path cannot describe, and it is handled
explicitly rather than by inference. A version whose root is itself `terminal`
completes an investigation at creation with **zero** steps, so its report has an
empty `path` and nothing to derive an outcome from. For that case — and only
when the investigation is `completed` — the report screen passes the published
`root_node_id` in as `terminalRootNodeId`, and the root is marked visited and
final. Every other highlight still comes from `path` alone.

### Networking

The browser only ever talks to `localhost:3000`. `/api/v1/*` is handled by a
Next.js route handler (`frontend/src/app/api/[...path]/route.ts`) that forwards
to `BACKEND_URL` **server-side** and returns the upstream status and body
untouched — so the Go API needs no CORS configuration, gains no
frontend-specific endpoints, and its address never appears in client
JavaScript.

| Environment | `BACKEND_URL` |
|---|---|
| Docker Compose | `http://api:8080` |
| Local development | `http://localhost:8080` |

### Running the console without Docker

Needs Node 22+ and a reachable API.

```bash
make docker-up-db && make run    # API on :8080, in one terminal
make web-install && make web-dev # console on :3000, in another
```

`make web-check` runs the console's lint, typecheck and tests.

---

## Walkthrough

The script below performs the complete lifecycle against a running API —
create a playbook, publish it, ingest an alert, start an investigation, make
three decisions, fetch the audit report, and confirm a completed investigation
rejects further decisions. Every ID is captured from a real response.

```bash
./scripts/walkthrough.sh          # requires curl and jq
```

If you would rather run it by hand, the same sequence in `curl`. Each command
is complete and executable; shell variables are populated from actual
responses, so there is nothing to paste in.

**1. Create the playbook** (from the committed fixture — the assignment's
example decision tree):

```bash
PLAYBOOK=$(curl -s -X POST localhost:8080/api/v1/playbooks \
  -H 'Content-Type: application/json' \
  -d @test/fixtures/example-playbook.json)

PLAYBOOK_ID=$(echo "$PLAYBOOK"  | jq -r '.id')
VERSION_ID=$(echo "$PLAYBOOK"   | jq -r '.versions[0].id')
echo "playbook=$PLAYBOOK_ID version=$VERSION_ID"
```

**2. Publish it.** This runs graph validation; the version becomes immutable.

```bash
curl -s -X POST "localhost:8080/api/v1/playbook-versions/$VERSION_ID/publish" \
  | jq '{version, status, published_at, nodes: (.nodes|length)}'
```

Publishing an invalid graph returns `422` with *every* problem found, not just
the first:

```json
{
  "code": "INVALID_PLAYBOOK_GRAPH",
  "message": "playbook graph is not valid for publishing (3 problem(s) found)",
  "details": [
    { "node_id": "aa00…01", "reason": "cycle_detected" },
    { "node_id": "aa00…02", "reason": "cycle_detected" },
    { "node_id": "aa00…03", "reason": "unreachable_from_root" }
  ]
}
```

**3. Ingest the alert:**

```bash
ALERT_ID=$(curl -s -X POST localhost:8080/api/v1/alerts \
  -H 'Content-Type: application/json' \
  -d @test/fixtures/example-alert.json | jq -r '.id')
echo "alert=$ALERT_ID"
```

**4. Start the investigation:**

```bash
INVESTIGATION_ID=$(curl -s -X POST "localhost:8080/api/v1/alerts/$ALERT_ID/investigations" \
  -H 'Content-Type: application/json' \
  -d "{\"playbook_version_id\":\"$VERSION_ID\"}" | jq -r '.id')

curl -s "localhost:8080/api/v1/investigations/$INVESTIGATION_ID" \
  | jq '{status, question: .current_node.title,
         choices: [.available_choices[] | {label, edge_id}]}'
```

```json
{
  "status": "in_progress",
  "question": "Write came from a known engineering workstation?",
  "choices": [
    { "label": "Yes", "edge_id": "e0000000-0000-4000-8000-000000000001" },
    { "label": "No",  "edge_id": "e0000000-0000-4000-8000-000000000002" }
  ]
}
```

This single response is everything an agent needs to act: the alert payload,
the current question, the designer's hints, the selectable choices, and the
history so far.

**5. Submit decisions.** The helper picks the edge by label, exactly as an
agent would — read the current choices, then submit one of their `edge_id`s.

```bash
decide() {
  local label="$1" rationale="$2" evidence="$3"
  local edge_id
  edge_id=$(curl -s "localhost:8080/api/v1/investigations/$INVESTIGATION_ID" \
    | jq -r --arg l "$label" '.available_choices[] | select(.label==$l) | .edge_id')

  curl -s -X POST "localhost:8080/api/v1/investigations/$INVESTIGATION_ID/decisions" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $(uuidgen)" \
    -d "$(jq -nc --arg e "$edge_id" --arg r "$rationale" --argjson ev "$evidence" \
      '{edge_id:$e, actor:{type:"agent", id:"investigation-agent-v1"},
        rationale:$r, evidence:$ev}')"
}

decide "Yes" "The source address belongs to ENG-WS-14, a registered engineering workstation." \
  '[{"type":"asset_inventory_lookup","summary":"10.20.1.44 maps to ENG-WS-14","data":{"asset":"ENG-WS-14","trusted":true}}]' \
  | jq '{status, next_question: .current_node.title}'

decide "Yes" "Change calendar shows an approved window 10:00-12:00 UTC covering PLC-17." \
  '[{"type":"change_calendar_lookup","summary":"Approved window 10:00-12:00 UTC","data":{"change_id":"CHG-4471"}}]' \
  | jq '{status, next_question: .current_node.title}'

decide "No" "Register 40021 is a non-safety setpoint; the SIS range is 41000-41100." \
  '[{"type":"register_map_lookup","summary":"40021 is outside the SIS register range","data":{"sis_range":"41000-41100"}}]' \
  | jq '{status, final_resolution, outcome: .current_node.title}'
```

The third decision reaches a terminal node:

```json
{
  "status": "completed",
  "final_resolution": "close_authorized_maintenance",
  "outcome": "Close: authorized maintenance"
}
```

**6. Fetch the audit report:**

```bash
curl -s "localhost:8080/api/v1/investigations/$INVESTIGATION_ID/report" | jq '{
  investigation,
  playbook_version: {id: .playbook_version.id, version: .playbook_version.version,
                     status: .playbook_version.status,
                     nodes: (.playbook_version.nodes|length)},
  path: [.path[] | {step_number, node_id, selected_edge_id,
                    actor: .actor.id, rationale, evidence: [.evidence[].type]}]
}'
```

The report returns the **complete playbook graph** together with a separate
ordered **path** — the exact route taken, with timestamps, actors, rationale
and evidence at each step.

**7. Confirm the safety property.** A completed investigation cannot be
advanced:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST "localhost:8080/api/v1/investigations/$INVESTIGATION_ID/decisions" \
  -H 'Content-Type: application/json' \
  -d '{"edge_id":"e0000000-0000-4000-8000-000000000001","actor":{"type":"human","id":"analyst"}}'
# 409
```

---

## Architecture

Layering, and the rules that keep it honest, are in
[docs/package-structure.md](docs/package-structure.md). Deployment topology,
observability and scaling are in [docs/architecture.md](docs/architecture.md).
The full entity model and invariant list are in
[docs/domain-model.md](docs/domain-model.md).

```
http  ─▶  service  ─▶  domain  ◀─  repository/postgres
```

`internal/domain` has no framework dependencies at all — no pgx, no chi, no
`net/http`. That is what makes the rules that matter (graph validation,
transition legality, terminal behaviour) testable with no database and no
server.

### Playbooks and versions

`playbooks` is the logical container; `playbook_versions` holds the actual
decision graphs; `playbook_nodes` and `playbook_edges` hold the graph
relationally. An investigation binds to a **`playbook_version_id`**, never to
a playbook.

Lifecycle is `draft -> published -> archived`. Drafts are freely editable.
Publishing validates the graph and freezes the version permanently: there is
no API path that mutates a published definition. Editing a published playbook
means creating a *new* version (optionally cloned from an existing one, with
fresh node and edge IDs so the two never share rows).

This is the property everything else rests on: **an investigation from last
year still resolves against exactly the graph it ran on**, no matter how many
times the playbook has been revised since. There is an integration test for
precisely this (`TestHistoricalInvestigationSurvivesPlaybookRevision`).

### Graph model: strict DAG

Cycles are rejected at publish time. This was chosen over general-graph
semantics with traversal guards because it matches how investigation
procedures are actually written, and because it removes a whole class of
runtime concern — with an acyclic graph, decision-time traversal needs no
depth limit or loop detection at all.

Re-convergence *is* allowed: two branches may lead to the same terminal
outcome, which is natural ("either way, escalate") and harmless.

Publish-time validation reports **all** problems at once: missing root,
dangling references, unreachable nodes, cycles, decision nodes with no
choices, terminal nodes with choices, and resolution/kind mismatches.

### Investigation audit trail

`investigation_steps` is append-only and is **the authoritative history**.
`investigations.current_node_id` is a convenience projection maintained in the
same transaction — useful for "what can I do next", never used to reconstruct
what happened. The report is always built from steps.

No route exists that updates or deletes a step. There is no `PUT`, no
`PATCH`, no `DELETE` anywhere on investigation history, which is tested
explicitly (`TestNoRouteMutatesInvestigationHistory`).

### Concurrency

Submitting a decision is one transaction taking `SELECT ... FOR UPDATE` on the
investigation row, then validating, appending the step, and updating state
before commit. Two agents racing on one investigation serialize; the first
applies, and the second is rejected with `409 INVALID_TRANSITION` because its
edge no longer leaves the (now advanced) current node.

A row lock was chosen over an optimistic version column because contention is
rare, the critical section is short, and the failure mode is a clean rejection
rather than a retry loop every client must implement. Reasoning in full in
[docs/architecture.md §3](docs/architecture.md).

### Database integrity

The schema does real work rather than just storing rows. Composite foreign
keys guarantee that an edge's endpoints live in the same playbook version as
the edge, that an investigation's current node belongs to its own version, and
that a step's node and edge belong to its investigation's own version — so
even a service-layer bug cannot record a step pointing into an unrelated
playbook. `CHECK` constraints enforce "terminal resolution set iff terminal
node" and the completion invariant. A partial unique index makes alert
ingestion idempotent on `external_id`; another makes decision retries
idempotent per investigation.

### Why PostgreSQL, and where JSONB is used

The core model is relational: playbooks, versions, nodes, edges,
investigations and steps all have stable shapes, real referential integrity
requirements, and queries that traverse them. Transactions are what make the
decision operation correct.

JSONB is used in exactly three places, all of them genuinely schemaless:
`alerts.payload` (domain-specific data the engine must not need to understand),
`investigation_steps.evidence` (producers disagree about everything but "type,
summary, arbitrary detail"), and `playbook_nodes.metadata` (designer hints).
Nothing structural lives in JSONB.

---

## Key decisions and trade-offs

### Relational graph, not one JSON document

A playbook version *could* be one JSONB column. Storing nodes and edges as
rows was chosen because it lets the database enforce the graph's integrity
rather than trusting application code: an edge cannot reference a node from
another version, a step cannot reference an edge from another playbook, and a
version cannot point at a root that is not its own. Those are composite
foreign keys, and they are unavailable in a JSON blob.

It also means individual nodes and edges are addressable and queryable, and
large playbooks do not require rewriting one enormous document on every edit.
The cost is a more elaborate write path — replacing a draft's graph is a
seven-statement transaction whose ordering is dictated by those same foreign
keys — which is accepted because it buys integrity that cannot be regressed by
a future code change. The API still accepts and returns the whole graph in one
payload, so callers never see this.

### Immutable published versions

An investigation is a record of a decision made under a specific procedure. If
the procedure could change underneath it, the record would become unreadable —
"why did we close this?" would have no answerable form. So publishing freezes,
and editing forks a new version.

The cost is version proliferation and a slightly heavier editing flow. That is
the right trade for a system whose primary product is auditability.

### Append-only history, separate from current state

Keeping `investigation_steps` distinct from `investigations.current_node_id`
means history can never be lost to an update. The pointer is derived state; the
steps are the truth. It costs one extra table and one extra write per decision,
and it is what makes "reconstruct exactly what happened" a query rather than an
archaeology exercise.

### Report: canonical graph plus a separate path

The report returns the playbook graph and an ordered `path` array as separate
top-level fields, rather than annotating graph nodes with
`was_visited` / `was_selected` / `step_number` flags.

The graph belongs to the playbook version and is identical for every
investigation that used it; the path belongs to this one run. Keeping them
apart means a UI can cache the graph per version and overlay any number of
paths on it, comparing two investigations is a diff of two small arrays, and a
completed investigation's path is stable regardless of anything else in the
system. Embedding flags would make the graph a per-investigation object and
force a re-fetch for every one. A UI highlights the route by indexing `path`
by `node_id` — a two-line transform.

### Synchronous API, not a workflow engine

Every operation is a short state transition on one row, initiated by a caller
who wants the result. There is no long-running work to schedule and nothing to
retry in the background, so a queue or workflow engine would add operational
surface and an eventual-consistency window in exchange for nothing.

Autonomous investigation fits naturally *without* changing this: an agent that
gathers evidence from historians, asset inventories or an LLM runs outside the
API and calls `POST /decisions` exactly as a human UI does. Slow, failure-prone
external calls stay out of the decision transaction. See
[docs/architecture.md §7](docs/architecture.md).

### No built-in LLM

The engine exposes an agent-friendly API and stops there. `GET
/investigations/{id}` returns the alert payload, the current question, the
designer's metadata hints, the available choices and the full history in one
response — enough for a model to reason and act — and `POST /decisions`
accepts free-form rationale and evidence.

Embedding a provider would couple workflow state to a model's availability,
pricing and output format, and make the audit trail depend on a vendor.
Keeping the agent a *client* means the engine is equally usable by a human, a
rules engine, a script, or a model, and the recorded investigation looks the
same in all four cases.

### A playbook only runs against its own alert type

`POST /alerts/{id}/investigations` rejects a playbook whose `alert_type`
differs from the alert's, with `409 ALERT_TYPE_MISMATCH`.

The alternative — treat playbook selection as entirely the caller's
responsibility — is defensible, but it would leave `alert_type` decorative: a
label the system stores and never acts on. Enforcing it means an investigation
can never record a PLC-register-write procedure being walked against a
failed-login alert, producing an audit trail whose questions never applied to
the alert they were asked about.

The cost is that a deliberately generic "triage anything" playbook is not
expressible today. If that is wanted, the clean extension is an explicit
opt-out on the playbook — a wildcard `alert_type`, or an
`applies_to_any_alert_type` flag — rather than dropping the check, so that
running a playbook outside its declared scope stays a decision someone made on
purpose.

### Enforced request shapes, not just documented ones

The OpenAPI document is a contract, so the implementation rejects anything it
does not describe rather than accepting whatever happens to be valid JSON.
`evidence` is decoded into typed items with required `type` and `summary`;
`payload` and node `metadata` are decoded through JSON objects, so a scalar or
an array is a `400`.

This is worth stating because the lax version is the easy default: decode into
`json.RawMessage`, check the bytes parse, store them in JSONB. That happily
accepts `"evidence": 123`, persists it, and later returns it from an endpoint
whose published schema promises an array of objects — a contract violation
that only shows up when someone writes a client against the spec.

### Idempotency

`POST /decisions` accepts an optional `Idempotency-Key` header, unique per
investigation. Retrying an equivalent request is a no-op that returns current
state; reusing the key with a *different* body is rejected with `409
IDEMPOTENCY_KEY_REUSED` rather than silently returning the wrong step.

This matters specifically because agents time out and retry, and a duplicate
step would corrupt an audit trail that is supposed to be exact. The retry is
recognised *before* the completion check, so a retry that arrives after the
original decision closed the investigation is still a correct no-op rather
than a spurious "already completed" error.

Alert ingestion is idempotent too, on `external_id`, because upstream alerting
systems retry the same way. First write wins: a retry with drifted values
returns what was originally stored rather than rewriting a record an
investigation may already reference.

---

## API

The full contract is [`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.0.3),
with schemas, status codes and examples. It is usable standalone by a frontend
or an agent, and is linted in CI.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Process liveness (checks nothing external) |
| `GET` | `/readyz` | Readiness, including database connectivity |
| `POST` | `/api/v1/playbooks` | Create a playbook, optionally with an initial draft graph |
| `GET` | `/api/v1/playbooks` | List playbooks (filterable by `alert_type`) |
| `GET` | `/api/v1/playbooks/{playbookId}` | Playbook metadata and its versions |
| `POST` | `/api/v1/playbooks/{playbookId}/versions` | New draft version, optionally cloned |
| `GET` | `/api/v1/playbook-versions/{versionId}` | Full graph definition |
| `PUT` | `/api/v1/playbook-versions/{versionId}` | Replace a draft's graph (409 if published) |
| `POST` | `/api/v1/playbook-versions/{versionId}/publish` | Validate and publish |
| `POST` | `/api/v1/alerts` | Ingest an alert (idempotent on `external_id`) |
| `GET` | `/api/v1/alerts/{alertId}` | Retrieve an alert |
| `POST` | `/api/v1/alerts/{alertId}/investigations` | Start an investigation |
| `GET` | `/api/v1/investigations/{investigationId}` | Current state and available choices |
| `POST` | `/api/v1/investigations/{investigationId}/decisions` | Submit a decision |
| `GET` | `/api/v1/investigations/{investigationId}/report` | Full audit report |

### Errors

Every failure returns a machine-readable envelope. Clients branch on `code`,
never on message text.

```json
{
  "code": "INVALID_TRANSITION",
  "message": "edge 5b1f… originates at node a000…03, but the investigation is at node a000…01"
}
```

| Status | Codes |
|---|---|
| 400 | `BAD_REQUEST`, `VALIDATION_FAILED` |
| 404 | `RESOURCE_NOT_FOUND` |
| 409 | `CONFLICT`, `PLAYBOOK_VERSION_NOT_DRAFT`, `PLAYBOOK_VERSION_NOT_PUBLISHED`, `INVESTIGATION_ALREADY_COMPLETED`, `INVALID_TRANSITION`, `IDEMPOTENCY_KEY_REUSED`, `ALERT_TYPE_MISMATCH` |
| 405 | `METHOD_NOT_ALLOWED` |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `INVALID_PLAYBOOK_GRAPH` (with a `details` array of every graph problem) |
| 503 | `NOT_READY` |

Database errors are never surfaced. Anything unclassified becomes a `500` with
a fixed opaque message; the cause goes to the logs.

### Safety properties the server enforces

A client — human or agent — cannot:

- choose an arbitrary next node (it selects an *edge*; the server derives the node);
- submit an edge that does not originate at the current node;
- submit an edge belonging to a different playbook version;
- advance a completed investigation;
- modify a published playbook;
- rewrite or delete a recorded step.

Each of these has a dedicated test in `test/integration/api_safety_test.go`.

---

## Testing

```bash
make test               # unit tests, no database needed
make test-integration   # everything, against a real PostgreSQL
make lint               # gofmt + go vet
make web-check          # web console: eslint + tsc + vitest
```

`make test-integration` expects PostgreSQL at
`postgres://interbellum:interbellum@localhost:5432/interbellum`, which is what
`make docker-up-db` provides:

```bash
make docker-up-db && make test-integration
```

Point it elsewhere with `TEST_DATABASE_URL=... make test-integration`.

**Unit tests** (`internal/domain/...`) cover the rules, with no database and
no HTTP: graph validation and every illegal-graph case, valid and invalid
transition selection, terminal-node behaviour, terminal-root investigations,
and publishing rules.

**Integration tests** (`test/integration/`) run against real PostgreSQL —
never mocks, because most of what they protect (composite foreign keys, unique
indexes, row locking, transaction atomicity) has no behaviour in a mock. They
cover playbook persistence and cloning, publish validation and freezing,
append-only ordered history, decision advancement and terminal completion,
concurrent decisions serializing, idempotent retries, and the full HTTP stack
including routing, decoding and error mapping.

The headline test is `TestFullInvestigationWorkflow`: create and publish the
example playbook, ingest the alert, start an investigation, submit three
decisions, reach a terminal state, request the report, and assert the exact
path, rationale, evidence and resolution.

Integration tests **skip** rather than fail when `TEST_DATABASE_URL` is unset,
so a fresh clone runs `go test ./...` green with no setup. CI runs both modes:
once without a database (proving the domain tests genuinely need none) and once
with one.

**Web console tests** (`frontend/tests/`) are deliberately few, and cover the
logic that could be wrong without anyone noticing rather than the markup:
mapping a report's `path` onto the canonical graph (including step-number
stamping, the terminal node the last edge leads to, and an empty path);
deterministic graph layout; evidence-form serialization against the published
`EvidenceItem` schema; how a completed investigation renders; and how each
structured backend error code is surfaced. There is no end-to-end browser suite
— the happy path is covered end-to-end by `TestFullInvestigationWorkflow`
against the real API, and adding Playwright would have cost more in
flakiness than it bought.

---

## Configuration

All configuration is environment variables, with defaults suited to
docker-compose. No secrets are committed.

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | `postgres://interbellum:interbellum@localhost:5432/interbellum?sslmode=disable` | PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` | Listen address |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | `json` or `text` (readable locally) |
| `RUN_MIGRATIONS` | `true` | Apply migrations at startup |
| `MAX_REQUEST_BYTES` | `1048576` | Request body limit |
| `HTTP_READ_TIMEOUT` | `15s` | |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | |
| `HTTP_WRITE_TIMEOUT` | `30s` | |
| `HTTP_IDLE_TIMEOUT` | `60s` | |
| `SHUTDOWN_TIMEOUT` | `20s` | Graceful drain budget |
| `DB_MAX_CONNS` | `10` | Pool size |
| `DB_MIN_CONNS` | `2` | |
| `DB_MAX_CONN_LIFETIME` | `1h` | |
| `DB_MAX_CONN_IDLE_TIME` | `30m` | |
| `DB_CONNECT_TIMEOUT` | `10s` | |

The web console has two of its own, read by the Next.js server only — never by
the browser:

| Variable | Default | Purpose |
|---|---|---|
| `BACKEND_URL` | `http://localhost:8080` | API address the proxy forwards to (`http://api:8080` in compose) |
| `PORT` | `3000` | Console listen port |

---

## Production considerations

**Authentication and authorization.** Out of scope here; the API is
unauthenticated. Production would place it behind an authenticating gateway
with these roles:

| Role | Permissions |
|---|---|
| Playbook designer | Create and edit drafts, **publish** versions. Cannot submit decisions. |
| Analyst | Start investigations, submit decisions, read reports. Cannot modify playbooks. |
| Automated agent | Same as analyst, scoped to specific alert types. Explicitly **cannot** modify playbooks. |
| Read-only auditor | Read playbooks, investigations and reports. No writes at all. |

The important consequence is that `actor` stops being self-reported: it would
be derived from the authenticated principal and the request field ignored,
making the audit trail attributable rather than merely descriptive.

**Secrets.** `DATABASE_URL` from a secret manager, injected as an environment
variable or a mounted file. The compose credentials are local development
values, not secrets.

**Database HA and backups.** Managed PostgreSQL (or a primary with a
synchronous standby), automated backups plus point-in-time recovery. An
investigation report is a record of a security decision; losing one is losing
evidence. Read replicas for report and analytics traffic once that grows.

**Rate limiting.** At the gateway, per authenticated principal. Agents are the
concern: a retry loop against `POST /decisions` should be shed at the edge, not
absorbed by the database.

**Observability.** Structured logs are implemented. Production would add
metrics, tracing, error-rate and latency alerts, database monitoring, and a
domain-level alert on investigations stuck `in_progress` — an agent that dies
mid-investigation produces no error anywhere else. See
[docs/architecture.md §5](docs/architecture.md).

**Evidence retention and large artifacts.** Investigations and steps are
compliance records kept for years. Large binary evidence (pcaps, screenshots,
historian exports) belongs in object storage, referenced by key from the
evidence JSON, with a shorter lifecycle policy — the reference stays in the
audit trail after the object expires, so the report still shows what was
collected. See [docs/architecture.md §6](docs/architecture.md).

**Async processing.** Not needed at this scale and deliberately absent; the
conditions that would justify it (autonomous evidence-gathering agents,
high-volume ingestion) and why neither requires an API redesign are in
[docs/architecture.md §7](docs/architecture.md).

**Bulk graph writes.** Nodes and edges are written as two pipelined
`pgx.Batch` round trips rather than one per row, so a large playbook does not
become thousands of sequential round trips inside an open transaction. The two
batches stay separate because every edge's foreign key needs its endpoints to
exist first. `COPY FROM` would be faster still for very large graphs, but it
bypasses the per-row error reporting that turns a constraint violation into a
useful `400`.

**Idempotency.** Implemented for decisions and alert ingestion. Production
would add a retention policy for idempotency keys — they accumulate forever
today, which is fine at this scale but is a slow leak at high volume.

**Schema evolution.** Migrations are backward compatible for the length of a
rollout, since old and new instances share a schema. Additive changes ship
freely; destructive ones ship a release later. Production would run
`cmd/migrate` as a separate deployment step rather than at API startup — see
[docs/architecture.md §4](docs/architecture.md).

---

## What was intentionally left out

Each of these was a deliberate scope decision, to keep the work concentrated
on the investigation domain and its correctness.

- **A production frontend.** The API is the deliverable. `frontend/` is a
  deliberately scoped review console — enough to author a playbook, inspect it,
  run an investigation and read its report — and not a product UI. It has no
  authentication, no client-side cache or store, no optimistic updates, no
  investigation list or search, no playbook archival, and no pagination. See
  [Web UI](#web-ui).
- **A drag-and-drop playbook canvas.** The authoring screen is a form over the
  node and edge lists, with the auto-laid-out graph as a read-only preview. A
  canvas would need positions to be meaningful, and the stored graph
  deliberately has none — a playbook is a procedure, not a drawing. See
  [Authoring a playbook](#authoring-a-playbook).
- **Production authentication and authorization.** Designed above, not built.
  Building it would consume time without exercising the domain.
- **An LLM provider integration.** Deliberate, and explained in [Key
  decisions](#no-built-in-llm). The API is agent-shaped; the agent stays a
  client.
- **Server-side playbook auto-resolution by `alert_type`.** The assignment
  offers it as optional. Requiring an explicit `playbook_version_id` means the
  version an investigation is bound to is always the caller's recorded choice
  rather than whatever happened to be published at that instant — which is the
  more auditable behaviour, and simpler.
- **Playbook archival workflow.** The `archived` status exists in the model
  and the schema so the lifecycle is representable; no endpoint drives it.
- **Kubernetes manifests, a message broker, a distributed tracing backend, and
  a metrics stack.** Adding infrastructure that nothing uses would demonstrate
  configuration, not judgment. The topology and what would trigger each
  addition are documented in [docs/architecture.md](docs/architecture.md).
- **Pagination on list endpoints.** `GET /playbooks` returns everything.
  Playbook counts are small by nature; this would be the first thing to add if
  that stopped being true.
