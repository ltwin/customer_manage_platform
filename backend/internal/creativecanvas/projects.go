package creativecanvas

import (
	"context"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Project struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	DefaultCanvasID string               `json:"default_canvas_id"`
	Revision        creativeops.Revision `json:"revision"`
	Archived        bool                 `json:"archived"`
	UpdatedAt       time.Time            `json:"updated_at"`
}
type ProjectPage struct {
	Items      []Project `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

func ListProjects(ctx context.Context, scope store.AccountScope, archived bool, limit int, cursor string) (ProjectPage, error) {
	if limit < 1 || limit > 100 {
		return ProjectPage{}, creativeops.ErrValidation
	}
	query := "projects:" + strconv.FormatBool(archived)
	key, err := creativeops.DecodePageCursor(cursor, scope.AccountID(), query)
	if err != nil {
		return ProjectPage{}, err
	}
	result := ProjectPage{Items: []Project{}}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		cond := "(archived_at IS NOT NULL)=$2"
		args := []any{archived}
		if cursor != "" {
			cond += " AND (updated_at,id)<($3,$4)"
			args = append(args, key.Time, key.ID)
		}
		rows, err := tx.QueryPage(ctx, "creative_projects", "id,name,revision,updated_at", cond, []store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, limit+1, 0, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p Project
			var revision int64
			if err := rows.Scan(&p.ID, &p.Name, &revision, &p.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			p.Revision = creativeops.Revision(revision)
			p.Archived = archived
			result.Items = append(result.Items, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
			last := result.Items[limit-1]
			result.NextCursor, err = creativeops.EncodePageCursor(scope.AccountID(), query, last.UpdatedAt, last.ID)
			if err != nil {
				return err
			}
		}
		for i := range result.Items {
			p := &result.Items[i]
			if err := tx.QueryRow(ctx, "creative_canvases", "id", "project_id=$2 AND is_default", p.ID).Scan(&p.DefaultCanvasID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProjectPage{}, err
	}
	return result, nil
}
