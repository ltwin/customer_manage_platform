package creativelibrary

import (
	"context"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type AssetPage struct {
	Items           []Asset              `json:"items"`
	NextCursor      string               `json:"next_cursor"`
	TotalCount      int64                `json:"total_count"`
	LibraryRevision creativeops.Revision `json:"library_revision"`
}

func ListAssets(ctx context.Context, scope store.AccountScope, limit int, cursor string) (AssetPage, error) {
	if limit < 1 || limit > 100 {
		return AssetPage{}, creativeops.ErrValidation
	}
	key, err := creativeops.DecodePageCursor(cursor, scope.AccountID(), "assets:recent")
	if err != nil {
		return AssetPage{}, err
	}
	result := AssetPage{Items: []Asset{}, LibraryRevision: 1}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var revision int64
		err := tx.QueryRow(ctx, "creative_library_settings", "library_revision", "").Scan(&revision)
		if err != nil && !errors.Is(err, store.ErrNoRows) {
			return err
		}
		if err == nil {
			result.LibraryRevision = creativeops.Revision(revision)
		}
		count, err := tx.Count(ctx, "creative_assets", "deleted_at IS NULL")
		if err != nil {
			return err
		}
		result.TotalCount = int64(count)
		cond := "deleted_at IS NULL"
		var args []any
		if cursor != "" {
			cond += " AND (created_at,id)<($2,$3)"
			args = []any{key.Time, key.ID}
		}
		rows, err := tx.QueryPage(ctx, "creative_assets", assetColumns+",created_at", cond, []store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}}, limit+1, 0, args...)
		if err != nil {
			return err
		}
		var times []time.Time
		for rows.Next() {
			var a Asset
			var revision int64
			var created time.Time
			if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.Kind, &revision, &a.ContentID, &a.ContentRevisionID, &created); err != nil {
				rows.Close()
				return err
			}
			a.Revision = creativeops.Revision(revision)
			result.Items = append(result.Items, a)
			times = append(times, created)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
			result.NextCursor, err = creativeops.EncodePageCursor(scope.AccountID(), "assets:recent", times[limit-1], result.Items[limit-1].ID)
			if err != nil {
				return err
			}
		}
		for i := range result.Items {
			a := &result.Items[i]
			r, err := creativecontent.RequireUsable(ctx, tx, a.ContentRevisionID, "display")
			if errors.Is(err, creativecontent.ErrUsageDenied) {
				a.Unavailable = true
				continue
			}
			if err != nil {
				return err
			}
			if r.ContentID != a.ContentID || r.Kind != a.Kind {
				return creativecontent.ErrMissingRoot
			}
			preview := creativecontent.Preview(r)
			a.Content = &preview
		}
		return nil
	})
	if err != nil {
		return AssetPage{}, err
	}
	return result, nil
}
