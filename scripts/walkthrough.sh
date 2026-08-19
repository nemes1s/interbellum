#!/usr/bin/env bash
#
# End-to-end walkthrough of the investigation engine, using only the public
# API. This is the same sequence as the README's curl example, with the IDs
# captured from real responses instead of pasted by hand.
#
# Usage:
#   ./scripts/walkthrough.sh                       # against http://localhost:8080
#   API=http://host:port ./scripts/walkthrough.sh
#
# Requires: curl, jq.

set -euo pipefail

API="${API:-http://localhost:8080}"
FIXTURES="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/test/fixtures"

for tool in curl jq; do
  command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 1; }
done

step() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }

# Fail loudly on an unexpected status rather than carrying a broken ID forward.
request() {
  local method=$1 path=$2 expected=$3 body=${4:-}
  local response status payload

  if [[ -n "$body" ]]; then
    response=$(curl -sS -w '\n%{http_code}' -X "$method" "$API$path" \
      -H 'Content-Type: application/json' -d "$body")
  else
    response=$(curl -sS -w '\n%{http_code}' -X "$method" "$API$path")
  fi

  status=$(tail -n1 <<<"$response")
  payload=$(sed '$d' <<<"$response")

  if [[ "$status" != "$expected" ]]; then
    echo "error: $method $path returned $status, expected $expected" >&2
    echo "$payload" >&2
    exit 1
  fi
  echo "$payload"
}

step "Checking the API is reachable"
request GET /readyz 200 | jq -c .

step "1. Creating the example playbook (draft)"
PLAYBOOK=$(request POST /api/v1/playbooks 201 "$(cat "$FIXTURES/example-playbook.json")")
PLAYBOOK_ID=$(jq -r '.id' <<<"$PLAYBOOK")
VERSION_ID=$(jq -r '.versions[0].id' <<<"$PLAYBOOK")
echo "playbook_id=$PLAYBOOK_ID"
echo "playbook_version_id=$VERSION_ID"

step "2. Publishing the version (validates the graph)"
request POST "/api/v1/playbook-versions/$VERSION_ID/publish" 200 \
  | jq -c '{id, version, status, published_at, nodes: (.nodes|length), edges: (.edges|length)}'

step "3. Ingesting the alert"
ALERT=$(request POST /api/v1/alerts 201 "$(cat "$FIXTURES/example-alert.json")")
ALERT_ID=$(jq -r '.id' <<<"$ALERT")
echo "alert_id=$ALERT_ID"

step "4. Starting the investigation"
STARTED=$(request POST "/api/v1/alerts/$ALERT_ID/investigations" 201 \
  "{\"playbook_version_id\":\"$VERSION_ID\"}")
INVESTIGATION_ID=$(jq -r '.id' <<<"$STARTED")
echo "investigation_id=$INVESTIGATION_ID"
jq -c '{status, question: .current_node.title, choices: [.available_choices[].label]}' <<<"$STARTED"

# Picks the edge whose label matches, exactly as an agent would: read the
# available choices from the current state, then submit one of their edge IDs.
decide() {
  local label=$1 rationale=$2 evidence=$3

  local state edge_id
  state=$(request GET "/api/v1/investigations/$INVESTIGATION_ID" 200)
  edge_id=$(jq -r --arg l "$label" '.available_choices[] | select(.label==$l) | .edge_id' <<<"$state")

  if [[ -z "$edge_id" || "$edge_id" == "null" ]]; then
    echo "error: no choice labelled '$label' at the current node" >&2
    jq -c '{question: .current_node.title, choices: [.available_choices[].label]}' <<<"$state" >&2
    exit 1
  fi

  request POST "/api/v1/investigations/$INVESTIGATION_ID/decisions" 200 "$(jq -nc \
    --arg edge "$edge_id" --arg rationale "$rationale" --argjson evidence "$evidence" \
    '{edge_id: $edge, actor: {type: "agent", id: "investigation-agent-v1"},
      rationale: $rationale, evidence: $evidence}')"
}

step "5. Decision 1 — known engineering workstation? Yes"
decide "Yes" \
  "The source address belongs to ENG-WS-14, a registered engineering workstation." \
  '[{"type":"asset_inventory_lookup","summary":"10.20.1.44 maps to ENG-WS-14","data":{"asset":"ENG-WS-14","trusted":true}}]' \
  | jq -c '{status, question: .current_node.title}'

step "6. Decision 2 — inside an approved maintenance window? Yes"
decide "Yes" \
  "Change calendar shows an approved window 10:00-12:00 UTC covering PLC-17." \
  '[{"type":"change_calendar_lookup","summary":"Approved window 10:00-12:00 UTC","data":{"change_id":"CHG-4471"}}]' \
  | jq -c '{status, question: .current_node.title}'

step "7. Decision 3 — safety (SIS) register? No -> terminal"
decide "No" \
  "Register 40021 is a non-safety setpoint; the SIS range is 41000-41100." \
  '[{"type":"register_map_lookup","summary":"40021 is outside the SIS register range","data":{"sis_range":"41000-41100"}}]' \
  | jq -c '{status, final_resolution, outcome: .current_node.title}'

step "8. Fetching the audit report"
request GET "/api/v1/investigations/$INVESTIGATION_ID/report" 200 | jq '{
  investigation,
  alert: {id: .alert.id, external_id: .alert.external_id, payload: .alert.payload},
  playbook_version: {id: .playbook_version.id, version: .playbook_version.version,
                     status: .playbook_version.status,
                     nodes: (.playbook_version.nodes|length),
                     edges: (.playbook_version.edges|length)},
  path: [.path[] | {step_number, node_id, selected_edge_id,
                    actor: .actor.id, rationale,
                    evidence: [.evidence[].type]}]
}'

step "9. Safety check — the same decision cannot be replayed on a completed investigation"
STALE=$(jq -r '.path[0].selected_edge_id' <<<"$(request GET "/api/v1/investigations/$INVESTIGATION_ID/report" 200)")
request POST "/api/v1/investigations/$INVESTIGATION_ID/decisions" 409 \
  "{\"edge_id\":\"$STALE\",\"actor\":{\"type\":\"human\",\"id\":\"analyst\"}}" | jq -c .

printf '\n\033[1;32mWalkthrough complete.\033[0m\n'
