# Package structure

```text
.
├── api/
│   └── openapi.yaml                    # source of truth for the HTTP contract
├── cmd/
│   ├── server/
│   │   └── main.go                     # wiring only: config, pool, router, graceful shutdown
│   └── migrate/
│       └── main.go                     # apply migrations and exit (production deploy step;
│                                       #  the server also applies them when RUN_MIGRATIONS=true)
├── internal/
│   ├── domain/
│   │   ├── playbook/                   # Playbook, PlaybookVersion, Node, Edge, graph validation
│   │   │   ├── playbook.go             # entities + lifecycle rules (draft/published/archived)
│   │   │   ├── graph.go                # publish-time validation (root, reachability, cycles)
│   │   │   └── repository.go           # Repository port (interface) consumed by the service layer
│   │   ├── alert/                      # Alert entity
│   │   │   ├── alert.go
│   │   │   └── repository.go
│   │   └── investigation/              # Investigation, InvestigationStep, Actor, EvidenceItem
│   │       ├── investigation.go        # entities
│   │       ├── decision.go             # "apply decision" domain logic (pure: given state+edge -> next state)
│   │       └── repository.go
│   │
│   ├── service/                        # use-case orchestration: DTOs in, domain calls out
│   │   ├── playbookservice/            # create/version/publish
│   │   ├── alertservice/               # create/get
│   │   └── investigationservice/       # start/report; decide is a thin pass-through (see below)
│   │
│   ├── repository/
│   │   └── postgres/                   # the only package that imports pgx
│   │       ├── postgres.go             # pgxpool setup, inTx helper, pg-error -> apperror mapping
│   │       ├── migrate.go              # applies the embedded migrations
│   │       ├── playbook.go             # implements domain/playbook.Repository
│   │       ├── alert.go                # implements domain/alert.Repository
│   │       └── investigation.go        # implements domain/investigation.Repository,
│   │                                   # including the decision transaction:
│   │                                   # ApplyDecision (BEGIN; SELECT ... FOR UPDATE;
│   │                                   # domain/investigation decision check; INSERT step;
│   │                                   # UPDATE investigation; COMMIT)
│   │
│   ├── http/                           # one package; see "Handlers are files, not packages"
│   │   ├── server.go                   # http.Server construction, timeouts, graceful shutdown
│   │   ├── router.go                   # chi routes -> handlers, wires /healthz /readyz
│   │   ├── middleware.go               # request ID, structured access log, panic recovery, body limit
│   │   ├── respond.go                  # JSON encode/decode + the single apperror -> HTTP status mapping
│   │   ├── health_handler.go
│   │   ├── playbook_handler.go
│   │   ├── alert_handler.go
│   │   ├── investigation_handler.go
│   │   └── httpdto/                    # request/response structs + mapping to/from domain types
│   │                                   # (kept separate from domain so API shape can evolve
│   │                                   #  without leaking into business logic)
│   │
│   ├── apperror/                       # domain error type + code, and its HTTP status mapping
│   ├── config/                         # env var loading (DATABASE_URL, HTTP_ADDR, LOG_LEVEL, ...)
│   └── logging/                        # slog setup (structured logging)
│
├── migrations/                         # golang-migrate SQL files, numbered, up/down pairs
├── test/
│   ├── integration/                    # real-Postgres repository + full-HTTP-stack tests
│   └── fixtures/                       # example "Unauthorized PLC Register Write" playbook JSON
├── scripts/
│   └── walkthrough.sh                  # the README's end-to-end example, executable
├── docs/
│   ├── domain-model.md
│   ├── package-structure.md
│   └── architecture.md                 # deployment diagram + production considerations
├── .github/workflows/ci.yml
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
└── README.md
```

## Boundary rules

- **`domain/*`** has zero framework dependencies (no `pgx`, no `chi`, no `net/http`).
  It defines entities, invariants, and the `Repository` port interfaces it needs.
  This is what unit tests in §15 of the spec ("playbook validation", "valid/invalid
  transition selection", "terminal-node behavior") exercise directly, with no
  database or HTTP involved.
- **`service/*`** depends on `domain/*` only (via the port interfaces). It
  owns input validation, use-case orchestration across repositories, and
  domain-event logging — never transactions. **Submitting a decision is the
  case worth spelling out**, because a naive reading of
  "service owns transactions" and "only `repository/postgres` imports `pgx`"
  are mutually impossible without a third abstraction:
  - `domain/investigation.Repository` exposes one atomic method,
    `ApplyDecision(ctx, investigationID, decisionInput) (Investigation, Step, error)`,
    alongside the ordinary `Create`/`Get`.
  - `repository/postgres/investigation.go` implements it by opening one
    transaction, `SELECT ... FOR UPDATE` on the investigation row, loading
    the candidate edge, calling the **pure** domain function in
    `domain/investigation/decision.go` (given current node + candidate edge,
    return the resulting state or a domain validation error — no I/O, fully
    unit-testable on its own), then inserting the step / updating the
    investigation / committing based on that result.
  - `investigationservice.SubmitDecision` is a thin pass-through: DTO in,
    one call to `repository.ApplyDecision`, error mapped, DTO out. It does
    not open a transaction itself.

  This was chosen over introducing a generic `Transactor` port (a `WithTx`
  abstraction the service would drive, with the repository just executing
  statements inside it). A cross-cutting transaction abstraction is the kind
  of premature architecture the assignment explicitly warns against; named
  atomic repository methods are simpler, and correctness of those
  transactions — not the purity of "service owns transactions" as a rule — is
  what's actually being evaluated. The business rule itself (is this edge
  valid from this node) still lives in `domain/investigation` as a pure
  function and stays unit-testable without a database; only its atomic
  *application* is repository-owned.

  **The same pattern applies to the other operations whose correctness needs
  more than one statement**, and for the same reason — each is one named
  repository method owning one transaction:
  - `Create` — playbook + version 1 + initial graph, so a playbook can never
    exist without a version.
  - `CreateVersion` — locks the parent playbook to assign the next version
    number, and clones the source graph with fresh IDs.
  - `ReplaceGraph` — the seven-step whole-graph swap whose ordering is forced
    by the root/edge/node foreign keys (see §4 of the domain model).
  - `Publish` — loads the graph, runs the **pure** `playbook.Graph.Validate`,
    and flips the status, all under a `SELECT ... FOR UPDATE` on the version
    row. The lock is the point: validating in the service and publishing in
    the repository would leave a window in which a concurrent `PUT` could
    substitute a different graph between "this graph is valid" and "this
    version is published".
- **`repository/postgres`** depends on `domain/*` (implements its `Repository`
  interfaces) and `pgx`. It never talks to `service` or `http`.
- **`http/*`** depends on `service/*` and `httpdto` only — handlers translate
  DTOs to service calls and service errors to HTTP responses via `apperror`.
  Handlers never touch `pgx` or construct SQL.
- **Handlers are files, not packages.** An earlier draft of this document put
  each handler group in its own package (`playbookhandler/`, `alerthandler/`,
  …) alongside a `middleware/` package. That was changed during
  implementation, because every handler needs the same three unexported
  helpers — `decodeJSON`, `writeJSON`, and `writeError`, the last of which is
  the *single* place an `apperror` becomes an HTTP status. Splitting handlers
  across packages forces one of two bad outcomes: duplicate those helpers per
  package, or add a shared `httputil` package purely to re-export them. The
  first loses the "one error mapping" property that keeps status codes
  consistent; the second is a micro-package created solely to satisfy a
  directory layout. One `internal/http` package with one file per resource
  keeps the same navigability at no cost, and the boundary that actually
  carries weight — `httpdto`, isolating wire shape from domain types — is
  still its own package.
- **Interfaces exist only at these boundaries** (repository ports, and nowhere
  else): no interface is introduced for a struct that has a single
  implementation and no boundary to cross, per the assignment's guidance
  against over-abstraction. This is also why there's no generic `Transactor`
  interface — see above.

Dependency direction is strictly:

```
http  ─▶  service  ─▶  domain  ◀─  repository/postgres
cmd/server ─▶ (all of the above, for wiring)
```

`domain` depends on exactly one other internal package, `apperror`, and on
nothing else — no `pgx`, no `chi`, no `net/http`, not even transitively.

`apperror` is deliberately not a layer in that diagram: it is a shared
*vocabulary* rather than a dependency in the architectural sense. It defines
the error type and the stable code constants (`INVALID_TRANSITION`,
`PLAYBOOK_VERSION_NOT_DRAFT`, …) that every layer speaks, and it imports only
`errors` and `fmt` from the standard library. The mapping from those codes to
HTTP statuses lives in `internal/http/status.go`, not with the codes
themselves, precisely so that this stays true: a code means "the investigation
is already completed", and only the transport layer decides that this is a 409.

That is what keeps the domain independently unit-testable — and it is checked,
not just asserted:

```console
$ go list -deps ./internal/domain/... | grep -x net/http   # no output
```

The alternative, a separate error type per layer with translation at each
boundary, was rejected as ceremony: there is one error contract in this
system, and duplicating it three times to preserve a diagram would make the
code worse, not cleaner.
