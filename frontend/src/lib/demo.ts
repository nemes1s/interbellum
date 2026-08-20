import type { CreateAlertRequest, CreatePlaybookRequest, PlaybookEdge, PlaybookNode } from "./types";

/**
 * UI-only sample values for the assignment's "Unauthorized PLC Register Write"
 * example, mirroring `test/fixtures/`.
 *
 * These are inputs to the normal API, never a substitute for it: the demo
 * playbook is created with `POST /playbooks` and published with
 * `POST /playbook-versions/{id}/publish`, and the demo alert only pre-fills the
 * ingestion form. Nothing here fakes server state.
 */

const ALERT_TYPE = "unauthorized_plc_register_write";

/** Node ids used inside this file only; real ids are generated per install. */
type NodeKey =
  | "known_workstation"
  | "maintenance_window"
  | "safety_register"
  | "historian_anomaly"
  | "seen_before"
  | "close_authorized"
  | "escalate_safety"
  | "escalate_unauthorized"
  | "contain_isolate"
  | "escalate_suspicious"
  | "escalate_intrusion";

interface DemoNode {
  key: NodeKey;
  kind: PlaybookNode["kind"];
  title: string;
  description: string;
  terminal_resolution?: string;
  metadata?: PlaybookNode["metadata"];
}

interface DemoEdge {
  from: NodeKey;
  to: NodeKey;
  label: string;
  description?: string;
  sort_order: number;
}

const DEMO_NODES: DemoNode[] = [
  {
    key: "known_workstation",
    kind: "decision",
    title: "Write came from a known engineering workstation?",
    description: "Resolve the source address against the asset inventory.",
    metadata: { suggested_evidence: ["asset_inventory_lookup"], payload_fields: ["source_ip"] },
  },
  {
    key: "maintenance_window",
    kind: "decision",
    title: "Inside an approved maintenance window?",
    description: "Check the change calendar for an approved window covering this asset.",
    metadata: { suggested_evidence: ["change_calendar_lookup"] },
  },
  {
    key: "safety_register",
    kind: "decision",
    title: "Register tied to a safety (SIS) function?",
    description: "Determine whether the register participates in a safety instrumented function.",
    metadata: { suggested_evidence: ["register_map_lookup"] },
  },
  {
    key: "historian_anomaly",
    kind: "decision",
    title: "Historian shows anomalous process values?",
    description: "Compare process values around the write against the historian baseline.",
    metadata: { suggested_evidence: ["historian_query"] },
  },
  {
    key: "seen_before",
    kind: "decision",
    title: "Source device seen on this network before?",
    description: "Check whether the source address has prior presence on this segment.",
    metadata: { suggested_evidence: ["network_flow_history"] },
  },
  {
    key: "close_authorized",
    kind: "terminal",
    title: "Close: authorized maintenance",
    description: "Known workstation, approved window, non-safety register.",
    terminal_resolution: "close_authorized_maintenance",
  },
  {
    key: "escalate_safety",
    kind: "terminal",
    title: "Escalate: safety review",
    description: "Authorized change touching a safety function requires review.",
    terminal_resolution: "escalate_safety_review",
  },
  {
    key: "escalate_unauthorized",
    kind: "terminal",
    title: "Escalate: unauthorized change",
    description: "Known workstation, but outside any approved maintenance window.",
    terminal_resolution: "escalate_unauthorized_change",
  },
  {
    key: "contain_isolate",
    kind: "terminal",
    title: "Contain: isolate segment, start incident response",
    description: "Unknown source combined with anomalous process values.",
    terminal_resolution: "contain_isolate_segment",
  },
  {
    key: "escalate_suspicious",
    kind: "terminal",
    title: "Escalate: suspicious activity",
    description: "Unknown source, no process anomaly, device seen before.",
    terminal_resolution: "escalate_suspicious_activity",
  },
  {
    key: "escalate_intrusion",
    kind: "terminal",
    title: "Escalate: possible intrusion",
    description: "Unknown source never seen on this network before.",
    terminal_resolution: "escalate_possible_intrusion",
  },
];

const DEMO_EDGES: DemoEdge[] = [
  {
    from: "known_workstation",
    to: "maintenance_window",
    label: "Yes",
    description: "Source resolves to a registered engineering workstation.",
    sort_order: 1,
  },
  {
    from: "known_workstation",
    to: "historian_anomaly",
    label: "No",
    description: "Source is unknown or not an engineering workstation.",
    sort_order: 2,
  },
  { from: "maintenance_window", to: "safety_register", label: "Yes", sort_order: 1 },
  { from: "maintenance_window", to: "escalate_unauthorized", label: "No", sort_order: 2 },
  { from: "safety_register", to: "escalate_safety", label: "Yes", sort_order: 1 },
  { from: "safety_register", to: "close_authorized", label: "No", sort_order: 2 },
  { from: "historian_anomaly", to: "contain_isolate", label: "Yes", sort_order: 1 },
  { from: "historian_anomaly", to: "seen_before", label: "No", sort_order: 2 },
  { from: "seen_before", to: "escalate_suspicious", label: "Yes", sort_order: 1 },
  { from: "seen_before", to: "escalate_intrusion", label: "No", sort_order: 2 },
];

/**
 * Build the create-playbook request with freshly generated node and edge IDs.
 *
 * IDs are client-supplied by the contract, and the committed fixture uses fixed
 * ones — which means installing it twice would collide on the primary key. New
 * IDs per install make the demo action safely repeatable.
 */
export function buildDemoPlaybookRequest(newId: () => string = () => crypto.randomUUID()): CreatePlaybookRequest {
  const ids = new Map<NodeKey, string>(DEMO_NODES.map((node) => [node.key, newId()]));

  const nodes: PlaybookNode[] = DEMO_NODES.map((node) => ({
    id: ids.get(node.key)!,
    kind: node.kind,
    title: node.title,
    description: node.description,
    terminal_resolution: node.terminal_resolution ?? null,
    metadata: node.metadata ?? null,
  }));

  const edges: PlaybookEdge[] = DEMO_EDGES.map((edge) => ({
    id: newId(),
    from_node_id: ids.get(edge.from)!,
    to_node_id: ids.get(edge.to)!,
    label: edge.label,
    description: edge.description ?? null,
    sort_order: edge.sort_order,
  }));

  return {
    name: "Unauthorized PLC Register Write",
    description: "Triage procedure for an unexpected write to a PLC holding register.",
    alert_type: ALERT_TYPE,
    definition: {
      root_node_id: ids.get("known_workstation")!,
      nodes,
      edges,
    },
  };
}

/** The assignment's example alert, as ingestion-form values. */
export const DEMO_ALERT: Required<Omit<CreateAlertRequest, "external_id">> & { external_id: string } = {
  external_id: "INTERBELLUM-2026-0001",
  alert_type: ALERT_TYPE,
  title: "Unauthorized PLC Register Write",
  description: "Write detected on PLC-17",
  occurred_at: "2026-08-19T10:30:00Z",
  payload: {
    plc: "PLC-17",
    register: "40021",
    source_ip: "10.20.1.44",
  },
};

/**
 * Suggested rationale and evidence per choice label, offered as a one-click
 * fill in the decision form so the reviewer does not have to invent OT detail
 * to see a realistic audit trail. Purely a form convenience.
 */
export const DEMO_DECISIONS: Record<string, { rationale: string; evidence: { type: string; summary: string; data: string } }> = {
  "Write came from a known engineering workstation?|Yes": {
    rationale: "The source address belongs to ENG-WS-14, a registered engineering workstation.",
    evidence: {
      type: "asset_inventory_lookup",
      summary: "10.20.1.44 maps to ENG-WS-14",
      data: '{\n  "asset": "ENG-WS-14",\n  "trusted": true\n}',
    },
  },
  "Inside an approved maintenance window?|Yes": {
    rationale: "Change calendar shows an approved window 10:00-12:00 UTC covering PLC-17.",
    evidence: {
      type: "change_calendar_lookup",
      summary: "Approved window 10:00-12:00 UTC",
      data: '{\n  "change_id": "CHG-4471"\n}',
    },
  },
  "Register tied to a safety (SIS) function?|No": {
    rationale: "Register 40021 is a non-safety setpoint; the SIS range is 41000-41100.",
    evidence: {
      type: "register_map_lookup",
      summary: "40021 is outside the SIS register range",
      data: '{\n  "sis_range": "41000-41100"\n}',
    },
  },
};

/** Look up a suggested decision fill for a node title and choice label. */
export function suggestedDecision(nodeTitle: string, choiceLabel: string) {
  return DEMO_DECISIONS[`${nodeTitle}|${choiceLabel}`] ?? null;
}
