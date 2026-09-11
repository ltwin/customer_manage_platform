-- Explicit account keys; graph ownership is checked by the command transaction.
ALTER TABLE creative_nodes DROP CONSTRAINT creative_nodes_type_key_check;
ALTER TABLE creative_nodes ADD COLUMN selected_version_id TEXT;
ALTER TABLE creative_nodes ADD COLUMN document_id TEXT;
ALTER TABLE creative_nodes ADD COLUMN status_revision BIGINT NOT NULL DEFAULT 1 CHECK(status_revision>0);
ALTER TABLE creative_nodes ADD COLUMN active_execution_id TEXT;
ALTER TABLE creative_nodes ADD COLUMN latest_execution_id TEXT;
CREATE TABLE creative_edges (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL,
 source_node_id TEXT NOT NULL, target_node_id TEXT NOT NULL, source_port TEXT NOT NULL, target_port TEXT NOT NULL,
 role TEXT NOT NULL, ordinal INTEGER NOT NULL DEFAULT 0, revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 UNIQUE(account_id,id), UNIQUE(account_id,canvas_id,source_node_id,target_node_id,source_port,target_port), CHECK(source_node_id<>target_node_id)
);
CREATE INDEX creative_edges_canvas ON creative_edges(account_id,canvas_id);
CREATE TABLE creative_node_inputs (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, node_id TEXT NOT NULL,
 slot TEXT NOT NULL, ordinal INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL,
 source_node_id TEXT, content_revision_id TEXT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0), UNIQUE(account_id,id), UNIQUE(account_id,node_id,slot,ordinal),
 CHECK((source_node_id IS NULL)<>(content_revision_id IS NULL))
);
CREATE INDEX creative_inputs_canvas ON creative_node_inputs(account_id,canvas_id);
CREATE INDEX creative_inputs_root ON creative_node_inputs(account_id,content_revision_id);
CREATE TABLE creative_graph_identities (
 id TEXT NOT NULL, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, kind TEXT NOT NULL,
 is_live BOOLEAN NOT NULL, placement_revision BIGINT NOT NULL DEFAULT 0, data_revision BIGINT NOT NULL DEFAULT 0,
 effect_heads JSONB NOT NULL CHECK(jsonb_typeof(effect_heads)='object'), PRIMARY KEY(account_id,id)
);
CREATE INDEX creative_identity_canvas ON creative_graph_identities(account_id,canvas_id);
CREATE TABLE creative_changes (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, operation_id TEXT NOT NULL,
 change_group_id TEXT, inverse_of TEXT, result_revision BIGINT NOT NULL, before_after JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), expires_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()+interval '30 days',
 UNIQUE(account_id,id), UNIQUE(account_id,operation_id)
);
CREATE INDEX creative_change_canvas ON creative_changes(account_id,canvas_id,result_revision);
CREATE INDEX creative_change_group ON creative_changes(account_id,canvas_id,change_group_id);
CREATE TABLE creative_change_content_refs (
 account_id TEXT NOT NULL, change_id TEXT NOT NULL, content_revision_id TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(account_id,change_id,content_revision_id)
);
CREATE INDEX creative_change_refs_root ON creative_change_content_refs(account_id,content_revision_id);
CREATE TABLE creative_node_prompt_drafts (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, node_id TEXT NOT NULL,
 action_key TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '' CHECK(char_length(text)<=32000), model_key TEXT NOT NULL DEFAULT '',
 parameters JSONB NOT NULL DEFAULT '{}', revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0), UNIQUE(account_id,node_id), UNIQUE(account_id,id)
);
CREATE TABLE creative_node_prompt_refs (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, draft_id TEXT NOT NULL, node_id TEXT NOT NULL,
 slot TEXT NOT NULL, ordinal INTEGER NOT NULL, role TEXT NOT NULL, source_node_id TEXT, content_revision_id TEXT,
 CHECK((source_node_id IS NULL)<>(content_revision_id IS NULL)), UNIQUE(account_id,draft_id,slot,ordinal)
);
CREATE INDEX creative_prompt_refs_canvas ON creative_node_prompt_refs(account_id,canvas_id);
CREATE INDEX creative_prompt_refs_root ON creative_node_prompt_refs(account_id,content_revision_id);
CREATE TABLE creative_node_versions (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, node_id TEXT NOT NULL,
 version_no BIGINT NOT NULL CHECK(version_no>0), origin_kind TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 execution_id_snapshot TEXT, output_ordinal INTEGER, generation_provenance JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), UNIQUE(account_id,node_id,version_no), UNIQUE(account_id,id),
 UNIQUE(account_id,execution_id_snapshot,output_ordinal)
);
CREATE INDEX creative_versions_root ON creative_node_versions(account_id,content_revision_id);
CREATE TABLE creative_node_version_input_refs (
 account_id TEXT NOT NULL, version_id TEXT NOT NULL, ordinal INTEGER NOT NULL, content_revision_id TEXT NOT NULL, role TEXT NOT NULL,
 PRIMARY KEY(account_id,version_id,ordinal)
);
CREATE INDEX creative_version_inputs_root ON creative_node_version_input_refs(account_id,content_revision_id);
CREATE TABLE creative_node_executions (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL, node_id TEXT NOT NULL, action_key TEXT NOT NULL, executor_version TEXT NOT NULL,
 operation_id TEXT NOT NULL, request_hash TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled','reconciling')),
 apply_state TEXT NOT NULL DEFAULT 'pending' CHECK(apply_state IN ('pending','applied','conflicted','discarded','expired')),
 execution_epoch BIGINT NOT NULL DEFAULT 0, lease_until TIMESTAMPTZ, deadline TIMESTAMPTZ NOT NULL,
 target_data_revision BIGINT NOT NULL, selected_version_id TEXT, draft_revision BIGINT NOT NULL, prompt_snapshot JSONB NOT NULL, read_set JSONB NOT NULL,
 caller_kind TEXT NOT NULL CHECK(caller_kind IN ('photographer','system','agent')), caller_agent_run_id TEXT,
 publish_operation_id TEXT NOT NULL, change_id TEXT, error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), retained_until TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()+interval '90 days',
 UNIQUE(account_id,id), UNIQUE(account_id,operation_id)
);
CREATE TABLE creative_execution_refs (
 account_id TEXT NOT NULL, execution_id TEXT NOT NULL, direction TEXT NOT NULL CHECK(direction IN ('input','output')),
 ordinal INTEGER NOT NULL, content_revision_id TEXT NOT NULL, role TEXT NOT NULL,
 PRIMARY KEY(account_id,execution_id,direction,ordinal)
);
CREATE INDEX creative_execution_refs_root ON creative_execution_refs(account_id,content_revision_id);
CREATE TABLE creative_documents (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, type_key TEXT NOT NULL, title TEXT NOT NULL,
 content_revision_id TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1, UNIQUE(account_id,id)
);
CREATE INDEX creative_documents_root ON creative_documents(account_id,content_revision_id);

CREATE TABLE creative_graph_migration_state (id INTEGER PRIMARY KEY CHECK(id=1), completed BOOLEAN NOT NULL DEFAULT false);
INSERT INTO creative_graph_migration_state(id) VALUES(1);
ALTER TABLE creative_node_prompt_refs ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0);
ALTER TABLE creative_changes ADD COLUMN reversible BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE creative_node_versions ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE TABLE creative_node_version_counters (account_id TEXT NOT NULL,node_id TEXT NOT NULL,last_version_no BIGINT NOT NULL DEFAULT 0,PRIMARY KEY(account_id,node_id));
ALTER TABLE creative_node_versions ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0);
CREATE TABLE creative_content_required_grants (
 account_id TEXT NOT NULL, content_revision_id TEXT NOT NULL, declaration_id TEXT NOT NULL,
 PRIMARY KEY(account_id,content_revision_id,declaration_id)
);
ALTER TABLE creative_node_executions ADD COLUMN result_operation_id TEXT NOT NULL;
