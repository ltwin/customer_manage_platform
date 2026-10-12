package creativepalette

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// WriteInTx stores a derived fact only while its account-scoped blob is ready.
// Callers protect publication or a read pin with the account writer barrier.
func WriteInTx(ctx context.Context, tx store.TxAccountScope, blob string, result Result) error {
	if result.State == "" {
		return nil
	} // Older staged imports are filled by maintenance.
	ready, err := tx.Exists(ctx, "creative_blobs", "id=$2 AND state='ready'", blob)
	if err != nil || !ready {
		return err
	}
	raw, err := json.Marshal(result.Colors)
	if err != nil {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	return tx.Upsert(ctx, "creative_blob_palettes", []string{"blob_id", "algorithm", "state", "failure_reason", "colors", "computed_at"}, []string{"account_id", "blob_id"}, []string{"algorithm", "state", "failure_reason", "colors", "computed_at"}, blob, Algorithm, result.State, result.Reason, raw, now)
}
