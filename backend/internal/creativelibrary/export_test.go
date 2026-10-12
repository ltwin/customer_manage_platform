package creativelibrary

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ReadableAssetsForTest evaluates readableContent the way the list applies it,
// so a test can hold it against creativecontent.ReadDisplayBatch.
func ReadableAssetsForTest(ctx context.Context, tx store.ReadTxAccountScope) (map[string]bool, error) {
	var args sqlArgs
	rows, err := tx.Query(ctx, "creative_assets", "content_revision_id", readableContent("creative_assets.content_revision_id", &args), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
