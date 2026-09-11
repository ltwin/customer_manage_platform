package creativecontent

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// RetainDerivativeRights keeps all contributing declarations authoritative after
// copying/editing a composition. It never grants a new purpose.
func RetainDerivativeRights(ctx context.Context, tx store.TxAccountScope, resultID string, sources []Revision) error {
	declarations := map[string]bool{}
	for _, source := range sources {
		declarations[source.DeclarationID] = true
		rows, err := tx.QueryPage(ctx, "creative_content_required_grants", "declaration_id", "content_revision_id=$2", []store.OrderBy{{Column: "declaration_id"}}, 1001, 0, source.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			declarations[id] = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if len(declarations) > 1000 {
			return ErrUsageDenied
		}
	}
	for id := range declarations {
		exists, err := tx.Exists(ctx, "creative_content_required_grants", "content_revision_id=$2 AND declaration_id=$3", resultID, id)
		if err != nil {
			return err
		}
		if !exists {
			if err = tx.Insert(ctx, "creative_content_required_grants", []string{"content_revision_id", "declaration_id"}, resultID, id); err != nil {
				return err
			}
		}
	}
	return nil
}
