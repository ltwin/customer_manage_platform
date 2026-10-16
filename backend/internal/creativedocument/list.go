package creativedocument

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	defaultListLimit = 20
	maxListLimit     = 50
	maxQueryRunes    = 100
)

// ListQuery filters an account's documents.
type ListQuery struct {
	Type    string
	Q       string
	Trashed bool
	Limit   int
	Cursor  string
}

// ListItem is one document row with its type summary.
type ListItem struct {
	ID        string               `json:"id"`
	TypeKey   string               `json:"type_key"`
	Title     string               `json:"title"`
	Revision  creativeops.Revision `json:"revision"`
	TrashedAt *time.Time           `json:"trashed_at"`
	UpdatedAt time.Time            `json:"updated_at"`
	Summary   json.RawMessage      `json:"summary"`
}

// ListPage is newest first; NextCursor is nil on the last page.
type ListPage struct {
	Items      []ListItem `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List pages the account's documents by last update.
func List(ctx context.Context, scope store.AccountScope, q ListQuery) (ListPage, error) {
	if q.Limit == 0 {
		q.Limit = defaultListLimit
	}
	if q.Limit < 1 || q.Limit > maxListLimit {
		return ListPage{}, creativeops.Explain(creativeops.ErrValidation, "limit 必须在 1 到 %d 之间", maxListLimit)
	}
	if utf8.RuneCountInString(q.Q) > maxQueryRunes {
		return ListPage{}, creativeops.Explain(creativeops.ErrValidation, "搜索词最多 %d 个字", maxQueryRunes)
	}
	bound := strings.Join([]string{"documents", q.Type, q.Q, fmt.Sprint(q.Trashed)}, "\x00")
	cursor, err := creativeops.DecodePageCursor(q.Cursor, scope.AccountID(), bound)
	if err != nil {
		return ListPage{}, creativeops.Explain(creativeops.ErrValidation, "游标无效")
	}
	page := ListPage{Items: []ListItem{}}
	err = scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		cond := "($2::text='' OR type_key=$2) AND ($3::text='' OR title ILIKE '%'||$3||'%' ESCAPE '\\') AND (trashed_at IS NOT NULL)=$4"
		args := []any{q.Type, escapeLike(q.Q), q.Trashed}
		if !cursor.Time.IsZero() {
			cond += " AND (updated_at,id)<($5,$6)"
			args = append(args, cursor.Time, cursor.ID)
		}
		rows, err := tx.QueryPage(ctx, "creative_documents", headColumns, cond, []store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, q.Limit+1, 0, args...)
		if err != nil {
			return err
		}
		var heads []Document
		for rows.Next() {
			d, err := scanHead(rows)
			if err != nil {
				rows.Close()
				return err
			}
			heads = append(heads, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(heads) > q.Limit {
			heads = heads[:q.Limit]
			last := heads[q.Limit-1]
			next, err := creativeops.EncodePageCursor(scope.AccountID(), bound, last.UpdatedAt, last.ID)
			if err != nil {
				return err
			}
			page.NextCursor = &next
		}
		// A document whose summary cannot be shown still lists, with a null summary.
		summaries, err := summarize(ctx, tx, heads)
		if err != nil {
			return err
		}
		for _, d := range heads {
			item := ListItem{ID: d.ID, TypeKey: d.TypeKey, Title: d.Title, Revision: creativeops.Revision(d.Revision), TrashedAt: d.TrashedAt, UpdatedAt: d.UpdatedAt, Summary: json.RawMessage("null")}
			if s := summaries[d.ID]; s.Usable {
				item.Summary = s.Summary
			}
			page.Items = append(page.Items, item)
		}
		return nil
	})
	return page, err
}
