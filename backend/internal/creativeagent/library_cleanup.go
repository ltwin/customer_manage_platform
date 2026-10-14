package creativeagent

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Keep a small expired tombstone so an expired result is never reported as an
// empty success. Conversation deletion cascades through the account-scoped FK.
func pruneLibraryResults(ctx context.Context, scope store.AccountScope, batch int) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.QueryPage(ctx, libraryCallTable, "id", "retained_until<=clock_timestamp() AND plan <> '{\"items\":[]}'::jsonb", []store.OrderBy{{Column: "retained_until"}, {Column: "id"}}, batch, 0)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		_, err = tx.Update(ctx, libraryCallTable, "plan='{\"items\":[]}'::jsonb,progress='[]'::jsonb,details='[]'::jsonb,summary=NULL,state='complete'", "id=ANY($2::text[]) AND retained_until<=clock_timestamp()", ids)
		return err
	})
}
