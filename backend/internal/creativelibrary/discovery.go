package creativelibrary

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrCursorStale = errors.New("library cursor stale; restart the query")
	ErrReadBudget  = errors.New("library item exceeds result budget; reduce the query")
)

type LibraryName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Signed cursors are account-local and versioned. The random key is persisted
// with the library root, so continuation is safe across server restarts.
func libraryCursorKey(ctx context.Context, tx libraryReader) ([]byte, error) {
	var key string
	err := tx.QueryRow(ctx, "creative_library_settings", "cursor_key", "").Scan(&key)
	if errors.Is(err, store.ErrNoRows) {
		return nil, creativeops.ErrValidation
	}
	return []byte(key), err
}
func encodeLibraryCursor(ctx context.Context, tx libraryReader, value any) (string, error) {
	key, err := libraryCursorKey(ctx, tx)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func decodeLibraryCursor(ctx context.Context, tx libraryReader, encoded string, target any) error {
	if len(encoded) > 16384 {
		return creativeops.ErrValidation
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return creativeops.ErrValidation
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return creativeops.ErrValidation
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return creativeops.ErrValidation
	}
	key, err := libraryCursorKey(ctx, tx)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return creativeops.ErrValidation
	}
	return creativeops.Decode(raw, target)
}

func readNames(ctx context.Context, tx libraryReader, table string, ids []string) ([]LibraryName, error) {
	result := []LibraryName{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.QueryPage(ctx, table, "id,name", "id=ANY($2::text[])", []store.OrderBy{{Column: "id"}}, len(ids), 0, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item LibraryName
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// DiscoverAssets projects full metadata and relations in the search snapshot.
// Both item and byte limits shorten a prefix; its cursor always names that prefix.
func DiscoverAssets(ctx context.Context, sc store.AccountScope, query Search, budget int) (json.RawMessage, error) {
	query.Snapshot = true
	var output json.RawMessage
	_, err := searchAssets(ctx, sc, query, func(tx store.ReadTxAccountScope, page *AssetPage, q Search, key string) error {
		items := []map[string]any{}
		for _, a := range page.Items {
			tags, err := readNames(ctx, tx, "creative_tags", a.TagIDs)
			if err != nil {
				return err
			}
			groups, err := readNames(ctx, tx, "creative_asset_groups", a.GroupIDs)
			if err != nil {
				return err
			}
			item := map[string]any{"asset_id": a.ID, "content_revision_id": a.ContentRevisionID, "revision": a.Revision, "title": a.Title, "description": a.Description, "kind": a.Kind, "rating": a.Rating, "is_favorite": a.Favorite, "trashed": a.DeletedAt != nil, "unavailable": a.Unavailable, "tags": tags, "groups": groups, "relations_complete": true, "tag_count": len(tags), "group_count": len(groups)}
			// A model's only sized media is its 256 px thumbnail; its brief says
			// how big the model is instead.
			if a.Content != nil && a.Kind == "model" {
				if f, ok := creativecontent.ModelFactsOf(a.Content.Payload); ok {
					item["model"] = f.Brief()
				}
			} else if a.Content != nil {
				for _, m := range a.Content.Media {
					if m.Width != nil && m.Height != nil {
						item["width"], item["height"] = *m.Width, *m.Height
						break
					}
				}
			}
			items = append(items, item)
		}
		render := func(n int) ([]byte, error) {
			more := n < len(page.Items) || page.NextCursor != ""
			next := ""
			if more && n > 0 {
				a := page.Items[n-1]
				c := searchCursor{Account: sc.AccountID(), Query: key, ID: a.ID, Name: textindex.Normalize(a.Title), Created: a.CreatedAt, Revision: page.LibraryRevision}
				if a.Rating != nil {
					v := ratingHalves(*a.Rating)
					c.Rating = &v
				}
				var err error
				next, err = encodeLibraryCursor(ctx, tx, c)
				if err != nil {
					return nil, err
				}
			}
			return json.Marshal(map[string]any{"items": items[:n], "total_count": page.TotalCount, "library_revision": page.LibraryRevision, "has_more": more, "next_cursor": next})
		}
		for n := len(items); n > 0; n-- {
			raw, err := render(n)
			if err != nil {
				return err
			}
			if len(raw) <= budget {
				output = raw
				return nil
			}
		}
		if len(items) > 0 {
			// Only a single oversize relation projection is replaced by a complete
			// continuation contract; core metadata is never shortened.
			item := items[0]
			delete(item, "tags")
			delete(item, "groups")
			item["relations_complete"] = false
			item["relations_query"] = map[string]any{"kind": "asset_relations", "asset_id": page.Items[0].ID, "expected_library_revision": page.LibraryRevision}
			raw, err := render(1)
			if err != nil {
				return err
			}
			if len(raw) > budget {
				return ErrReadBudget
			}
			output = raw
			return nil
		}
		raw, err := render(0)
		if err != nil {
			return err
		}
		if len(raw) > budget {
			return ErrReadBudget
		}
		output = raw
		return nil
	})
	return output, err
}

// Raw nullable filters distinguish absent from explicit null.
type LibraryQuery struct {
	Kind                    string               `json:"kind,omitempty"`
	ID                      string               `json:"id,omitempty"`
	Query                   string               `json:"query,omitempty"`
	ParentID                json.RawMessage      `json:"parent_id,omitempty"`
	CategoryID              json.RawMessage      `json:"category_id,omitempty"`
	AssetID                 string               `json:"asset_id,omitempty"`
	ExpectedLibraryRevision creativeops.Revision `json:"expected_library_revision,omitempty"`
	Limit                   int                  `json:"limit,omitempty"`
	Cursor                  string               `json:"cursor,omitempty"`
}
type libraryPageCursor struct {
	Account  string               `json:"account"`
	Query    string               `json:"query"`
	Revision creativeops.Revision `json:"revision"`
	Kind     int                  `json:"kind"`
	ID       string               `json:"id"`
}
type DeleteImpact struct {
	AssetCount         *int64 `json:"asset_count,omitempty"`
	TagCount           *int64 `json:"tag_count,omitempty"`
	PromotedGroupCount *int64 `json:"promoted_group_count,omitempty"`
}
type LibraryItem struct {
	Kind         string               `json:"kind"`
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Revision     creativeops.Revision `json:"revision"`
	ParentID     *string              `json:"parent_id,omitempty"`
	CategoryID   *string              `json:"category_id,omitempty"`
	DeleteImpact *DeleteImpact        `json:"delete_impact,omitempty"`
}
type libraryPart struct {
	kind, table, columns string
	rank                 int
}

func (q *LibraryQuery) normalize() error {
	if q.Kind == "" {
		q.Kind = "all"
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 100 || utf8.RuneCountInString(q.Query) > 200 {
		return creativeops.ErrValidation
	}
	q.Query = textindex.Normalize(q.Query)
	switch q.Kind {
	case "all", "group", "category", "tag", "asset_relations":
	default:
		return creativeops.ErrValidation
	}
	if q.ParentID != nil && q.Kind != "group" || q.CategoryID != nil && q.Kind != "tag" {
		return creativeops.ErrValidation
	}
	for _, raw := range []json.RawMessage{q.ParentID, q.CategoryID} {
		if raw != nil {
			var id *string
			if json.Unmarshal(raw, &id) != nil || id != nil && *id == "" {
				return creativeops.ErrValidation
			}
		}
	}
	if q.Kind == "asset_relations" {
		if q.AssetID == "" || q.ID != "" || q.Query != "" || q.ParentID != nil || q.CategoryID != nil || q.Cursor == "" && q.ExpectedLibraryRevision < 1 {
			return creativeops.ErrValidation
		}
	} else if q.AssetID != "" || q.ExpectedLibraryRevision != 0 {
		return creativeops.ErrValidation
	}
	return nil
}
func (q LibraryQuery) condition(part libraryPart, args *sqlArgs) string {
	cond := "true"
	if q.ID != "" {
		cond += " AND id=" + args.bind(q.ID)
	}
	if q.Query != "" {
		value := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q.Query) + "%"
		column := "normalized_name"
		if part.kind == "group" || part.kind == "category" {
			column = "lower(normalize(name,NFKC))"
		}
		cond += " AND " + column + " LIKE " + args.bind(value) + ` ESCAPE '\'`
	}
	for _, f := range []struct {
		raw    json.RawMessage
		column string
	}{{q.ParentID, "parent_id"}, {q.CategoryID, "category_id"}} {
		if f.raw != nil {
			var id *string
			_ = json.Unmarshal(f.raw, &id)
			if id == nil {
				cond += " AND " + f.column + " IS NULL"
			} else {
				cond += " AND " + f.column + "=" + args.bind(*id)
			}
		}
	}
	if q.Kind == "asset_relations" {
		table, column := "creative_asset_tags", "tag_id"
		if part.kind == "group" {
			table, column = "creative_asset_group_members", "group_id"
		}
		cond += " AND id IN (SELECT " + column + " FROM " + table + " WHERE account_id=$1 AND asset_id=" + args.bind(q.AssetID) + ")"
	}
	return cond
}

// BrowseLibrary performs bounded keyset queries, including counts from the same
// snapshot. It never fetches all 10,000 structures merely to trim them in Go.
func BrowseLibrary(ctx context.Context, sc store.AccountScope, q LibraryQuery, budget int) (json.RawMessage, error) {
	if err := q.normalize(); err != nil {
		return nil, err
	}
	canonical := q
	canonical.Cursor = ""
	canonical.Limit = 0
	canonical.ExpectedLibraryRevision = 0
	keyBytes, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	key := "library-v1:" + string(keyBytes)
	var output json.RawMessage
	err = sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		settings, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		cursor := libraryPageCursor{Account: sc.AccountID(), Query: key, Revision: settings.LibraryRevision, Kind: -1}
		if q.Cursor != "" {
			if err := decodeLibraryCursor(ctx, tx, q.Cursor, &cursor); err != nil {
				return err
			}
			if cursor.Account != sc.AccountID() || cursor.Query != key || cursor.ID == "" || cursor.Kind < 0 || cursor.Kind > 2 {
				return creativeops.ErrValidation
			}
			if cursor.Revision != settings.LibraryRevision {
				return ErrCursorStale
			}
		}
		if q.ExpectedLibraryRevision > 0 && q.ExpectedLibraryRevision != settings.LibraryRevision {
			return ErrCursorStale
		}
		var assetRevision creativeops.Revision
		if q.Kind == "asset_relations" {
			err := tx.QueryRow(ctx, "creative_assets", "revision", "id=$2", q.AssetID).Scan(&assetRevision)
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		parts := []libraryPart{{"group", "creative_asset_groups", "id,name,revision,parent_id", 0}, {"category", "creative_tag_categories", "id,name,revision", 1}, {"tag", "creative_tags", "id,name,revision,category_id", 2}}
		if q.Kind == "asset_relations" {
			parts = []libraryPart{{"tag", "creative_tags", "id,name,revision,category_id", 0}, {"group", "creative_asset_groups", "id,name,revision,parent_id", 1}}
		}
		items := []LibraryItem{}
		ranks := []int{}
		var total int64
		for _, part := range parts {
			if q.Kind != "all" && q.Kind != "asset_relations" && q.Kind != part.kind {
				continue
			}
			var args sqlArgs
			cond := q.condition(part, &args)
			count, err := tx.Count(ctx, part.table, cond, args...)
			if err != nil {
				return err
			}
			total += count
			if part.rank < cursor.Kind || len(items) > q.Limit {
				continue
			}
			if part.rank == cursor.Kind {
				cond += " AND id>" + args.bind(cursor.ID)
			}
			rows, err := tx.QueryPage(ctx, part.table, part.columns, cond, []store.OrderBy{{Column: "id"}}, q.Limit+1-len(items), 0, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				item := LibraryItem{Kind: part.kind}
				dest := []any{&item.ID, &item.Name, &item.Revision}
				if part.kind == "group" {
					dest = append(dest, &item.ParentID)
				}
				if part.kind == "tag" {
					dest = append(dest, &item.CategoryID)
				}
				if err := rows.Scan(dest...); err != nil {
					rows.Close()
					return err
				}
				items = append(items, item)
				ranks = append(ranks, part.rank)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		if q.Kind != "asset_relations" {
			for i := range items {
				item := &items[i]
				impact := &DeleteImpact{}
				var count int64
				var err error
				switch item.Kind {
				case "tag":
					count, err = tx.Count(ctx, "creative_asset_tags", "tag_id=$2", item.ID)
					impact.AssetCount = &count
				case "category":
					count, err = tx.Count(ctx, "creative_tags", "category_id=$2", item.ID)
					impact.TagCount = &count
				case "group":
					count, err = tx.Count(ctx, "creative_asset_group_members", "group_id=$2", item.ID)
					impact.AssetCount = &count
					if err == nil {
						var promoted int64
						promoted, err = tx.Count(ctx, "creative_asset_groups", "parent_id=$2", item.ID)
						impact.PromotedGroupCount = &promoted
					}
				}
				if err != nil {
					return err
				}
				item.DeleteImpact = impact
			}
		}
		max := min(len(items), q.Limit)
		for n := max; n >= 0; n-- {
			if n == 0 && len(items) > 0 {
				return ErrReadBudget
			}
			more := n < len(items)
			next := ""
			if more {
				c := libraryPageCursor{Account: sc.AccountID(), Query: key, Revision: settings.LibraryRevision, Kind: ranks[n-1], ID: items[n-1].ID}
				next, err = encodeLibraryCursor(ctx, tx, c)
				if err != nil {
					return err
				}
			}
			page := map[string]any{"items": items[:n], "total_count": total, "has_more": more, "next_cursor": next, "library_revision": settings.LibraryRevision}
			if q.Kind == "asset_relations" {
				page["asset_id"] = q.AssetID
				page["asset_revision"] = assetRevision
			}
			raw, err := json.Marshal(page)
			if err != nil {
				return err
			}
			if len(raw) <= budget {
				output = raw
				return nil
			}
		}
		return ErrReadBudget
	})
	return output, err
}
