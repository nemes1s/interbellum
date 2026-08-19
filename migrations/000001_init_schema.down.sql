DROP TABLE IF EXISTS investigation_steps;
DROP TABLE IF EXISTS investigations;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS playbook_edges;
ALTER TABLE IF EXISTS playbook_versions DROP CONSTRAINT IF EXISTS fk_playbook_versions_root_node;
DROP TABLE IF EXISTS playbook_nodes;
DROP TABLE IF EXISTS playbook_versions;
DROP TABLE IF EXISTS playbooks;

DROP TYPE IF EXISTS investigation_actor_type;
DROP TYPE IF EXISTS investigation_status;
DROP TYPE IF EXISTS playbook_node_kind;
DROP TYPE IF EXISTS playbook_version_status;
