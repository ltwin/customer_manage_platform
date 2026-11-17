-- When a canvas object first appeared. The identity row outlives deletion
-- (is_live=false), so a node restored by undo takes its creation time from
-- here instead of the fresh creative_nodes row. clock_timestamp() is
-- volatile, so adding the column rewrites the table once (as 0060 and 0072
-- did); the backfill then gives live nodes their real time. Identities of nodes deleted before this
-- migration have no source and keep the migration time.
ALTER TABLE creative_graph_identities
 ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp();

UPDATE creative_graph_identities i SET created_at = n.created_at
 FROM creative_nodes n
 WHERE n.account_id = i.account_id AND n.id = i.id;
