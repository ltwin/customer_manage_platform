package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CheckReadAccess authorizes an event subscription without loading node content.
func CheckReadAccess(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		_, err := lockCanvas(ctx, tx, id, false)
		return err
	})
}
