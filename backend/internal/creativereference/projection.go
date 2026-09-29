// Package creativereference projects live owning references across creative domains.
// It owns only the read projection: writes and lifecycle transitions remain with
// each owning domain. Never authorize using provenance IDs or an unverified cache.
package creativereference

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Reader interface {
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
}

// Live reads owners in the caller's transaction. Expiry, deletion and execution
// retention are evaluated here for both single reads and aggregate snapshots.
// All subqueries stay in the account bound by the store scope ($1).
func Live(ctx context.Context, tx Reader, revisionIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return out, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_content_revisions", "id", "id=ANY($2::text[]) AND ("+liveOwnerCondition+")", []store.OrderBy{{Column: "id"}}, len(revisionIDs), 0, revisionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

const liveOwnerCondition = `EXISTS (SELECT 1 FROM creative_assets a JOIN creative_contents c ON c.account_id=a.account_id AND c.id=a.content_id WHERE a.account_id=$1 AND a.content_revision_id=creative_content_revisions.id AND a.content_id=creative_content_revisions.content_id AND a.kind=c.kind)
OR EXISTS (SELECT 1 FROM creative_nodes n JOIN creative_contents c ON c.account_id=n.account_id AND c.id=n.content_id WHERE n.account_id=$1 AND n.content_revision_id=creative_content_revisions.id AND n.content_id=creative_content_revisions.content_id AND n.type_key='core.'||c.kind)
OR EXISTS (SELECT 1 FROM creative_message_content_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id)
OR EXISTS (SELECT 1 FROM creative_checkpoint_content_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND checkpoint_id IN (SELECT id FROM creative_agent_checkpoints WHERE account_id=$1 AND retained_until>clock_timestamp()))
OR EXISTS (SELECT 1 FROM creative_context_item_content_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND item_id IN (SELECT id FROM creative_agent_context_items WHERE account_id=$1 AND retained_until>clock_timestamp()))
OR EXISTS (SELECT 1 FROM creative_change_content_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND expires_at>clock_timestamp())
OR EXISTS (SELECT 1 FROM creative_node_inputs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id)
OR EXISTS (SELECT 1 FROM creative_node_prompt_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id)
OR EXISTS (SELECT 1 FROM creative_node_versions WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND deleted_at IS NULL AND node_id IN (SELECT id FROM creative_nodes WHERE account_id=$1))
OR EXISTS (SELECT 1 FROM creative_node_version_input_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND version_id IN (SELECT id FROM creative_node_versions WHERE account_id=$1 AND deleted_at IS NULL AND node_id IN (SELECT id FROM creative_nodes WHERE account_id=$1)))
OR EXISTS (SELECT 1 FROM creative_execution_refs WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND execution_id IN (SELECT id FROM creative_node_executions WHERE account_id=$1 AND (retained_until>clock_timestamp() OR state IN ('queued','running','reconciling'))))
OR EXISTS (SELECT 1 FROM creative_documents WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id)
OR EXISTS (SELECT 1 FROM creative_upload_candidates WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND state='pending' AND expires_at>clock_timestamp())
OR EXISTS (SELECT 1 FROM creative_revision_holds WHERE account_id=$1 AND content_revision_id=creative_content_revisions.id AND (expires_at IS NULL OR expires_at>clock_timestamp()))`
