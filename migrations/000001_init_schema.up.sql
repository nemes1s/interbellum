-- Initial schema for the Agentic Alert Investigation Engine.
-- See docs/domain-model.md for the entity/invariant rationale behind these
-- tables and constraints.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- =====================================================================
-- Playbook design: playbooks, versions, nodes, edges
-- =====================================================================

CREATE TABLE playbooks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    alert_type  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Supports resolving a published playbook by alert_type.
CREATE INDEX idx_playbooks_alert_type ON playbooks (alert_type);

CREATE TYPE playbook_version_status AS ENUM ('draft', 'published', 'archived');

CREATE TABLE playbook_versions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playbook_id   UUID NOT NULL REFERENCES playbooks (id) ON DELETE CASCADE,
    version       INTEGER NOT NULL,
    status        playbook_version_status NOT NULL DEFAULT 'draft',
    root_node_id  UUID NULL, -- FK added below, after playbook_nodes exists
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ NULL,

    UNIQUE (playbook_id, version),

    -- draft has never been published; published/archived always retain the
    -- timestamp of when they first became published.
    CONSTRAINT chk_playbook_versions_published_at CHECK (
        (status = 'draft' AND published_at IS NULL) OR
        (status IN ('published', 'archived') AND published_at IS NOT NULL)
    )
);

-- Playbook detail page: list versions for a playbook.
CREATE INDEX idx_playbook_versions_playbook_id ON playbook_versions (playbook_id);
-- Resolving "the published version" for a playbook/alert_type.
CREATE INDEX idx_playbook_versions_status ON playbook_versions (status);

CREATE TYPE playbook_node_kind AS ENUM ('decision', 'terminal');

CREATE TABLE playbook_nodes (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playbook_version_id  UUID NOT NULL REFERENCES playbook_versions (id) ON DELETE CASCADE,
    kind                 playbook_node_kind NOT NULL,
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    terminal_resolution  TEXT NULL,
    metadata             JSONB NULL,

    CONSTRAINT chk_playbook_nodes_terminal_resolution CHECK (
        (kind = 'terminal' AND terminal_resolution IS NOT NULL) OR
        (kind = 'decision' AND terminal_resolution IS NULL)
    ),

    -- Lets other tables take a composite FK on (playbook_version_id, id) to
    -- guarantee a referenced node belongs to the expected version.
    CONSTRAINT uq_playbook_nodes_version_id UNIQUE (playbook_version_id, id)
);

CREATE INDEX idx_playbook_nodes_version_id ON playbook_nodes (playbook_version_id);

-- Now that playbook_nodes exists, constrain root_node_id to reference a node
-- that belongs to *this same* playbook_version (not just any node).
--
-- Because of this FK, replacing a draft's whole graph (PUT) must run, in one
-- transaction, in exactly this order:
--   1. UPDATE playbook_versions SET root_node_id = NULL   (frees the old root)
--   2. DELETE the old playbook_edges                      (children first)
--   3. DELETE the old playbook_nodes                       (now unreferenced)
--   4. INSERT the new playbook_nodes
--   5. INSERT the new playbook_edges                       (nodes must exist first)
--   6. UPDATE playbook_versions SET root_node_id = <new root>  (or leave NULL
--      if the replacement draft doesn't set one yet)
--   7. COMMIT
-- Steps 2/3 and 4/5 are ordered by the edge->node FK dependency
-- (fk_playbook_edges_from/to_node_same_version below); step 1 is required or
-- step 3 fails with a FK violation on whichever node is still the root.
ALTER TABLE playbook_versions
    ADD CONSTRAINT fk_playbook_versions_root_node
    FOREIGN KEY (id, root_node_id) REFERENCES playbook_nodes (playbook_version_id, id);

CREATE TABLE playbook_edges (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playbook_version_id  UUID NOT NULL REFERENCES playbook_versions (id) ON DELETE CASCADE,
    from_node_id         UUID NOT NULL,
    to_node_id           UUID NOT NULL,
    label                TEXT NOT NULL,
    description          TEXT NULL,
    sort_order           INTEGER NOT NULL DEFAULT 0,

    -- A node can't have two edges with the same label (keeps choices
    -- unambiguous for a UI/agent rendering options at a node).
    UNIQUE (from_node_id, label),

    -- Composite FKs: guarantees from/to nodes belong to the SAME playbook
    -- version as the edge itself. This is the DB-level enforcement of
    -- "every edge stays within the same playbook version" from the spec.
    CONSTRAINT fk_playbook_edges_from_node_same_version
        FOREIGN KEY (playbook_version_id, from_node_id) REFERENCES playbook_nodes (playbook_version_id, id),
    CONSTRAINT fk_playbook_edges_to_node_same_version
        FOREIGN KEY (playbook_version_id, to_node_id) REFERENCES playbook_nodes (playbook_version_id, id),

    -- Lets investigation_steps take a composite FK on (playbook_version_id,
    -- selected_edge_id), the same trick used for playbook_nodes above.
    CONSTRAINT uq_playbook_edges_version_id UNIQUE (playbook_version_id, id)
);

CREATE INDEX idx_playbook_edges_version_id ON playbook_edges (playbook_version_id);
-- "What choices exist at this node" — the hot path when serving current
-- investigation state / available decisions.
CREATE INDEX idx_playbook_edges_from_node_id ON playbook_edges (from_node_id);

-- =====================================================================
-- Alerts
-- =====================================================================

CREATE TABLE alerts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id  TEXT NULL,
    alert_type   TEXT NOT NULL,
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at  TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_alerts_alert_type ON alerts (alert_type);
-- Idempotent ingestion from an upstream alerting system, when it supplies one.
CREATE UNIQUE INDEX uq_alerts_external_id ON alerts (external_id) WHERE external_id IS NOT NULL;

-- =====================================================================
-- Investigations
-- =====================================================================

CREATE TYPE investigation_status AS ENUM ('in_progress', 'completed');

CREATE TABLE investigations (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id             UUID NOT NULL REFERENCES alerts (id),
    playbook_version_id  UUID NOT NULL REFERENCES playbook_versions (id),
    status               investigation_status NOT NULL DEFAULT 'in_progress',
    current_node_id      UUID NOT NULL,
    started_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at         TIMESTAMPTZ NULL,
    final_resolution     TEXT NULL,

    CONSTRAINT chk_investigations_completion CHECK (
        (status = 'completed' AND completed_at IS NOT NULL AND final_resolution IS NOT NULL) OR
        (status = 'in_progress' AND completed_at IS NULL AND final_resolution IS NULL)
    ),

    -- current_node_id must belong to the SAME playbook_version the
    -- investigation is bound to.
    CONSTRAINT fk_investigations_current_node_same_version
        FOREIGN KEY (playbook_version_id, current_node_id) REFERENCES playbook_nodes (playbook_version_id, id),

    -- Lets investigation_steps take a composite FK on (investigation_id,
    -- playbook_version_id), forcing every step's playbook_version_id to
    -- match its own investigation's — the same trick used for playbook_nodes
    -- and playbook_edges above.
    CONSTRAINT uq_investigations_id_playbook_version_id UNIQUE (id, playbook_version_id)
);

-- "Investigations for this alert".
CREATE INDEX idx_investigations_alert_id ON investigations (alert_id);
-- "All in-progress investigations" (agent polling / operational dashboards).
CREATE INDEX idx_investigations_status ON investigations (status);
CREATE INDEX idx_investigations_playbook_version_id ON investigations (playbook_version_id);

CREATE TYPE investigation_actor_type AS ENUM ('human', 'agent');

CREATE TABLE investigation_steps (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- No standalone FK on investigation_id: it's covered by the composite FK
    -- below, which additionally forces playbook_version_id (next column) to
    -- match this specific investigation's own playbook_version_id.
    investigation_id      UUID NOT NULL,
    -- Denormalized copy of investigations.playbook_version_id (immutable
    -- once an investigation is created). Exists so node_id/selected_edge_id
    -- below can be composite-FK'd to require they belong to the SAME
    -- playbook version as the investigation — otherwise a service bug could
    -- insert a step pointing at a node/edge from an unrelated version. Its
    -- own consistency with the parent investigation is DB-enforced by
    -- fk_investigation_steps_investigation_same_version below, not merely
    -- assumed of the service layer.
    playbook_version_id   UUID NOT NULL REFERENCES playbook_versions (id),
    sequence_number       INTEGER NOT NULL,
    node_id               UUID NOT NULL,
    -- The edge selected at node_id. Every step is a decision — see
    -- docs/domain-model.md §8 — so this is never null; a terminal root
    -- completes an investigation with zero steps instead of a stepless edge.
    selected_edge_id      UUID NOT NULL,
    actor_type            investigation_actor_type NOT NULL,
    actor_id              TEXT NULL,
    rationale             TEXT NULL,
    evidence              JSONB NULL,
    idempotency_key       TEXT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (investigation_id, sequence_number),

    -- Forces playbook_version_id to be exactly this investigation's own
    -- playbook_version_id (see uq_investigations_id_playbook_version_id).
    CONSTRAINT fk_investigation_steps_investigation_same_version
        FOREIGN KEY (investigation_id, playbook_version_id) REFERENCES investigations (id, playbook_version_id),
    CONSTRAINT fk_investigation_steps_node_same_version
        FOREIGN KEY (playbook_version_id, node_id) REFERENCES playbook_nodes (playbook_version_id, id),
    CONSTRAINT fk_investigation_steps_edge_same_version
        FOREIGN KEY (playbook_version_id, selected_edge_id) REFERENCES playbook_edges (playbook_version_id, id)
);

-- Authoritative audit trail retrieval, in order — the hot path for reports.
CREATE INDEX idx_investigation_steps_investigation_id_seq
    ON investigation_steps (investigation_id, sequence_number);

-- Idempotent decision submission: same (investigation, key) is a retry, not a
-- new step. NULL keys (idempotency not used by the caller) are unconstrained.
CREATE UNIQUE INDEX uq_investigation_steps_idempotency
    ON investigation_steps (investigation_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
