package creativelibrary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type AssetPage struct {
	Items           []Asset              `json:"items"`
	NextCursor      string               `json:"next_cursor"`
	TotalCount      int64                `json:"total_count"`
	LibraryRevision creativeops.Revision `json:"library_revision"`
}
type Search struct {
	View        string   `json:"view"`
	GroupID     string   `json:"group_id"`
	Descendants bool     `json:"include_descendants"`
	Kind        string   `json:"kind"`
	Q           string   `json:"q"`
	TagIDs      []string `json:"tag_ids"`
	TagMode     string   `json:"tag_mode"`
	Sort        string   `json:"sort"`
	Limit       int      `json:"limit"`
	Cursor      string   `json:"-"`
}
type searchCursor struct {
	Account string    `json:"account"`
	Query   string    `json:"query"`
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

func searchKey(q Search) (string, error) {
	q.Cursor = ""
	raw, err := json.Marshal(q)
	return textindex.Version + ":" + string(raw), err
}
func (q Search) normalized() (Search, error) {
	if utf8.RuneCountInString(q.Q) > 200 {
		return q, creativeops.ErrValidation
	}
	if q.View == "" {
		q.View = "all"
	}
	if q.Sort == "" {
		q.Sort = "recent"
	}
	if q.TagMode == "" {
		q.TagMode = "all"
	}
	q.Q = textindex.Normalize(q.Q)
	if q.View != "all" && q.View != "favorites" && q.View != "unclassified" && q.View != "trash" || q.Sort != "recent" && q.Sort != "oldest" && q.Sort != "name" || q.TagMode != "all" && q.TagMode != "any" || q.Kind != "" && q.Kind != "text" && q.Kind != "link" || q.Limit < 1 || q.Limit > 100 || utf8.RuneCountInString(q.Q) > 200 || q.GroupID != "" && q.View == "unclassified" || q.Descendants && q.GroupID == "" {
		return q, creativeops.ErrValidation
	}
	var err error
	q.TagIDs, err = uniqueIDs(q.TagIDs, 50)
	return q, err
}
func ListAssets(ctx context.Context, s store.AccountScope, limit int, cursor string) (AssetPage, error) {
	return SearchAssets(ctx, s, Search{Limit: limit, Cursor: cursor})
}
func SearchAssets(ctx context.Context, sc store.AccountScope, query Search) (AssetPage, error) {
	q, err := query.normalized()
	if err != nil {
		return AssetPage{}, err
	}
	key, err := searchKey(q)
	if err != nil {
		return AssetPage{}, err
	}
	var cursor searchCursor
	if q.Cursor != "" {
		if len(q.Cursor) > 8192 {
			return AssetPage{}, creativeops.ErrValidation
		}
		data, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || creativeops.Decode(data, &cursor) != nil || cursor.Account != sc.AccountID() || cursor.Query != key || cursor.ID == "" || cursor.Created.IsZero() {
			return AssetPage{}, creativeops.ErrValidation
		}
	}
	result := AssetPage{Items: []Asset{}}
	err = sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		settings, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		result.LibraryRevision = settings.LibraryRevision
		if q.GroupID != "" {
			if err = requireEntity(ctx, tx, "creative_asset_groups", q.GroupID); err != nil {
				return err
			}
		}
		for _, id := range q.TagIDs {
			if err = requireEntity(ctx, tx, "creative_tags", id); err != nil {
				return err
			}
		}
		cond := "deleted_at IS NULL"
		if q.View == "trash" {
			cond = "deleted_at IS NOT NULL"
		}
		if q.View == "favorites" {
			cond += " AND is_favorite"
		}
		if q.View == "unclassified" {
			cond += " AND NOT EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id)"
		}
		args := []any{}
		bind := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)+1) }
		if q.GroupID != "" {
			groups := []string{q.GroupID}
			if q.Descendants {
				all, e := readGroups(ctx, tx)
				if e != nil {
					return e
				}
				seen := map[string]bool{q.GroupID: true}
				for i := 0; i < len(groups); i++ {
					for _, g := range all {
						if parentKey(g.ParentID) == groups[i] && !seen[g.ID] {
							seen[g.ID] = true
							groups = append(groups, g.ID)
						}
					}
				}
			}
			cond += " AND EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id AND m.group_id=ANY(" + bind(groups) + "::text[]))"
		}
		if q.Kind != "" {
			cond += " AND kind=" + bind(q.Kind)
		}
		if len(q.TagIDs) > 0 {
			p := bind(q.TagIDs)
			if q.TagMode == "any" {
				cond += " AND EXISTS(SELECT 1 FROM creative_asset_tags t WHERE t.account_id=$1 AND t.asset_id=creative_assets.id AND t.tag_id=ANY(" + p + "::text[]))"
			} else {
				cond += " AND (SELECT count(*) FROM creative_asset_tags t WHERE t.account_id=$1 AND t.asset_id=creative_assets.id AND t.tag_id=ANY(" + p + "::text[]))=cardinality(" + p + "::text[])"
			}
		}
		if q.Q != "" {
			pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q.Q) + "%"
			p := bind(pattern)
			pairs := []string{}
			for _, source := range []planningmedia.SourceClass{planningmedia.SourceOfficial, planningmedia.SourceAnimeScreenshot, planningmedia.SourceSettingBook, planningmedia.SourceFan, planningmedia.SourceUnknownWeb, planningmedia.SourcePhotographerOwned, planningmedia.SourceLicensed, planningmedia.SourceCustomerSupplied} {
				for _, basis := range []planningmedia.RightsBasis{planningmedia.RightsCitationOrDisplay, planningmedia.RightsOwnershipAttested, planningmedia.RightsLicenseRecorded, planningmedia.RightsDisplayConsent} {
					if planningmedia.ValidatePurpose(planningmedia.RightsDeclarationInput{SourceClass: source, RightsBasis: basis}, planningmedia.PurposeMoodboardDisplay) == nil {
						pairs = append(pairs, string(source)+":"+string(basis))
					}
				}
			}
			rights := bind(pairs)
			// Revoked/deleted content cannot be inferred through keyword matches.
			cond += ` AND (normalized_title LIKE ` + p + ` ESCAPE '\' OR EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=$1 AND s.asset_id=creative_assets.id AND s.normalized_text LIKE ` + p + ` ESCAPE '\' AND EXISTS(SELECT 1 FROM creative_content_revisions r JOIN creative_rights_declarations d ON d.account_id=r.account_id AND d.id=r.rights_declaration_id JOIN creative_usage_grants u ON u.account_id=r.account_id AND u.declaration_id=r.rights_declaration_id WHERE r.account_id=$1 AND r.id=creative_assets.content_revision_id AND r.state='ready' AND u.purpose='display' AND u.revoked_at IS NULL AND d.source_class||':'||d.rights_basis=ANY(` + rights + `::text[]))) OR EXISTS(SELECT 1 FROM creative_asset_tags a JOIN creative_tags t ON t.account_id=a.account_id AND t.id=a.tag_id WHERE a.account_id=$1 AND a.asset_id=creative_assets.id AND t.normalized_name LIKE ` + p + ` ESCAPE '\'))`
		}
		result.TotalCount, err = tx.Count(ctx, "creative_assets", cond, args...)
		if err != nil {
			return err
		}
		order := []store.OrderBy{{Column: "created_at", Desc: q.Sort == "recent"}, {Column: "id", Desc: q.Sort == "recent"}}
		if q.Sort == "name" {
			order = []store.OrderBy{{Column: "normalized_title"}, {Column: "id"}}
		}
		if q.Cursor != "" {
			if q.Sort == "name" {
				cond += " AND (normalized_title,id)>(" + bind(cursor.Name) + "," + bind(cursor.ID) + ")"
			} else {
				op := ">"
				if q.Sort == "recent" {
					op = "<"
				}
				cond += " AND (created_at,id)" + op + "(" + bind(cursor.Created) + "," + bind(cursor.ID) + ")"
			}
		}
		rows, err := tx.QueryPage(ctx, "creative_assets", assetColumns, cond, order, q.Limit+1, 0, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			a, e := scanAsset(rows)
			if e != nil {
				rows.Close()
				return e
			}
			result.Items = append(result.Items, a)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if len(result.Items) > q.Limit {
			result.Items = result.Items[:q.Limit]
			last := result.Items[q.Limit-1]
			data, e := json.Marshal(searchCursor{Account: sc.AccountID(), Query: key, ID: last.ID, Name: textindex.Normalize(last.Title), Created: last.CreatedAt})
			if e != nil {
				return e
			}
			result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		}
		for i := range result.Items {
			a := &result.Items[i]
			if err = attachRelations(ctx, tx, a); err != nil {
				return err
			}
			r, e := creativecontent.ReadInSnapshot(ctx, tx, a.ContentRevisionID, "display")
			if errors.Is(e, creativecontent.ErrUsageDenied) {
				a.Unavailable = true
				continue
			}
			if e != nil {
				return e
			}
			if r.ContentID != a.ContentID || r.Kind != a.Kind {
				return creativecontent.ErrMissingRoot
			}
			preview := creativecontent.Preview(r)
			a.Content = &preview
		}
		return nil
	})
	return result, err
}
func attachRelations(ctx context.Context, tx libraryReader, a *Asset) error {
	for _, rel := range []struct {
		table, column string
		target        *[]string
	}{{"creative_asset_group_members", "group_id", &a.GroupIDs}, {"creative_asset_tags", "tag_id", &a.TagIDs}} {
		*rel.target = []string{}
		rows, err := tx.Query(ctx, rel.table, rel.column, "asset_id=$2", a.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			*rel.target = append(*rel.target, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
