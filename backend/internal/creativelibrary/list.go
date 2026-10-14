package creativelibrary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	Facets          *Facets              `json:"facets,omitempty"`
}

// Facets are the first page's counts, read in the same snapshot as its items.
type Facets struct {
	Kinds map[string]int64 `json:"kinds"`
	Tags  []TagCount       `json:"tags"`
}
type TagCount struct {
	TagID string `json:"tag_id"`
	Count int64  `json:"count"`
}
type Search struct {
	AssetID     string   `json:"asset_id,omitempty"`
	Untagged    bool     `json:"untagged,omitempty"`
	Snapshot    bool     `json:"snapshot,omitempty"`
	Shape       string   `json:"shape,omitempty"`
	Color       string   `json:"color,omitempty"`
	View        string   `json:"view"`
	GroupID     string   `json:"group_id"`
	Descendants bool     `json:"include_descendants"`
	Kind        string   `json:"kind"`
	Q           string   `json:"q"`
	TagIDs      []string `json:"tag_ids"`
	TagMode     string   `json:"tag_mode"`
	Sort        string   `json:"sort"`
	Limit       int      `json:"limit"`
	MinRating   *float64 `json:"min_rating,omitempty"`
	Unrated     bool     `json:"unrated,omitempty"`
	// Facets are counted on the first page only and do not bind the cursor.
	IncludeFacets bool   `json:"-"`
	Cursor        string `json:"-"`
}
type searchCursor struct {
	Account  string               `json:"account"`
	Query    string               `json:"query"`
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Created  time.Time            `json:"created"`
	Rating   *int16               `json:"rating,omitempty"`
	Revision creativeops.Revision `json:"library_revision,omitempty"`
}

var assetKinds = []string{"text", "link", "image", "video", "audio"}

func searchKey(q Search) (string, error) {
	q.Cursor = ""
	q.Limit = 0
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
	if !slices.Contains([]string{"all", "recent", "favorites", "unclassified", "trash"}, q.View) || !slices.Contains([]string{"recent", "oldest", "name", "rating"}, q.Sort) || q.TagMode != "all" && q.TagMode != "any" || q.Kind != "" && !slices.Contains(assetKinds, q.Kind) || q.Limit < 1 || q.Limit > 100 || utf8.RuneCountInString(q.Q) > 200 || q.GroupID != "" && q.View == "unclassified" || q.Descendants && q.GroupID == "" {
		return q, creativeops.ErrValidation
	}
	// A present min_rating is always validated; nil alone means "not given".
	if q.MinRating != nil && ratingHalves(*q.MinRating) == 0 || q.Unrated && q.MinRating != nil || q.IncludeFacets && q.Cursor != "" {
		return q, creativeops.ErrValidation
	}
	var err error
	if q.Shape != "" && !slices.Contains([]string{"landscape", "square", "portrait"}, q.Shape) {
		return q, creativeops.ErrValidation
	}
	q.Color, err = normalizedColor(q.Color)
	if err != nil {
		return q, creativeops.ErrValidation
	}
	q.TagIDs, err = uniqueIDs(q.TagIDs, 50)
	return q, err
}

// sqlArgs numbers placeholders after the scope's $1 account binding.
type sqlArgs []any

func (a *sqlArgs) bind(v any) string {
	*a = append(*a, v)
	return fmt.Sprintf("$%d", len(*a)+1)
}

// searchFilter is a validated query with its group subtree resolved. The page,
// total_count and facets all build their WHERE clause here, inside one read
// snapshot, so the numbers always describe the listed items.
type searchFilter struct {
	q      Search
	groups []string
}

// condition renders the filter; facets leave out the kind or tag part they count.
func (f searchFilter) condition(withKind, withTags bool, args *sqlArgs) string {
	q := f.q
	cond := "deleted_at IS NULL"
	switch q.View {
	case "trash":
		cond = "deleted_at IS NOT NULL"
	case "recent":
		cond += " AND created_at >= now() - interval '7 days'"
	case "favorites":
		cond += " AND is_favorite"
	case "unclassified":
		cond += " AND NOT EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id)"
	}
	if q.AssetID != "" {
		cond += " AND id=" + args.bind(q.AssetID)
	}
	if q.Untagged {
		cond += " AND NOT EXISTS(SELECT 1 FROM creative_asset_tags t WHERE t.account_id=$1 AND t.asset_id=creative_assets.id)"
	}
	if len(f.groups) > 0 {
		cond += " AND EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id AND m.group_id=ANY(" + args.bind(f.groups) + "::text[]))"
	}
	if withKind && q.Kind != "" {
		cond += " AND kind=" + args.bind(q.Kind)
	}
	if withTags && len(q.TagIDs) > 0 {
		p := args.bind(q.TagIDs)
		if q.TagMode == "any" {
			cond += " AND EXISTS(SELECT 1 FROM creative_asset_tags t WHERE t.account_id=$1 AND t.asset_id=creative_assets.id AND t.tag_id=ANY(" + p + "::text[]))"
		} else {
			cond += " AND (SELECT count(*) FROM creative_asset_tags t WHERE t.account_id=$1 AND t.asset_id=creative_assets.id AND t.tag_id=ANY(" + p + "::text[]))=cardinality(" + p + "::text[])"
		}
	}
	if q.Unrated {
		cond += " AND rating_halves IS NULL"
	} else if q.MinRating != nil {
		cond += " AND rating_halves >= " + args.bind(ratingHalves(*q.MinRating))
	}
	if q.Q != "" {
		p := args.bind("%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q.Q) + "%")
		// Revoked/deleted content cannot be inferred through keyword matches.
		cond += ` AND (normalized_title LIKE ` + p + ` ESCAPE '\' OR EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=$1 AND s.asset_id=creative_assets.id AND s.normalized_text LIKE ` + p + ` ESCAPE '\' AND ` + readableContent("creative_assets.content_revision_id", args) + `) OR EXISTS(SELECT 1 FROM creative_asset_tags a JOIN creative_tags t ON t.account_id=a.account_id AND t.id=a.tag_id WHERE a.account_id=$1 AND a.asset_id=creative_assets.id AND t.normalized_name LIKE ` + p + ` ESCAPE '\'))`
	}
	return cond + appearanceCondition(q, args)
}

// displayRights lists the declaration pairs that allow moodboard display.
func displayRights() []string {
	pairs := []string{}
	for _, source := range []planningmedia.SourceClass{planningmedia.SourceOfficial, planningmedia.SourceAnimeScreenshot, planningmedia.SourceSettingBook, planningmedia.SourceFan, planningmedia.SourceUnknownWeb, planningmedia.SourcePhotographerOwned, planningmedia.SourceLicensed, planningmedia.SourceCustomerSupplied} {
		for _, basis := range []planningmedia.RightsBasis{planningmedia.RightsCitationOrDisplay, planningmedia.RightsOwnershipAttested, planningmedia.RightsLicenseRecorded, planningmedia.RightsDisplayConsent} {
			if planningmedia.ValidatePurpose(planningmedia.RightsDeclarationInput{SourceClass: source, RightsBasis: basis}, planningmedia.PurposeMoodboardDisplay) == nil {
				pairs = append(pairs, string(source)+":"+string(basis))
			}
		}
	}
	return pairs
}

// readableContent is the SQL form of creativecontent.ReadDisplayBatch's guard
// for an asset's own revision: ready, declared for display, display grant not
// revoked and no lost required grant. The asset row itself is the live owner,
// so the reference check holds by construction. Anything derived from content
// (body text, later colour and shape) must pass it before it can match or count.
func readableContent(revision string, args *sqlArgs) string {
	return `EXISTS(SELECT 1 FROM creative_content_revisions r JOIN creative_rights_declarations d ON d.account_id=r.account_id AND d.id=r.rights_declaration_id
 WHERE r.account_id=$1 AND r.id=` + revision + ` AND r.state='ready' AND d.source_class||':'||d.rights_basis=ANY(` + args.bind(displayRights()) + `::text[])
 AND EXISTS(SELECT 1 FROM creative_usage_grants g WHERE g.account_id=$1 AND g.declaration_id=r.rights_declaration_id AND g.purpose='display' AND g.revoked_at IS NULL)
 AND NOT EXISTS(SELECT 1 FROM creative_content_required_grants q WHERE q.account_id=$1 AND q.content_revision_id=r.id AND q.declaration_id NOT IN (SELECT declaration_id FROM creative_usage_grants WHERE account_id=$1 AND purpose='display' AND revoked_at IS NULL)))`
}

func ListAssets(ctx context.Context, s store.AccountScope, limit int, cursor string) (AssetPage, error) {
	return SearchAssets(ctx, s, Search{Limit: limit, Cursor: cursor})
}
func SearchAssets(ctx context.Context, sc store.AccountScope, query Search) (AssetPage, error) {
	return searchAssets(ctx, sc, query, nil)
}

func searchAssets(ctx context.Context, sc store.AccountScope, query Search, project func(store.ReadTxAccountScope, *AssetPage, Search, string) error) (AssetPage, error) {
	q, err := query.normalized()
	if err != nil {
		return AssetPage{}, err
	}
	key, err := searchKey(q)
	if err != nil {
		return AssetPage{}, err
	}
	var cursor searchCursor
	if q.Cursor != "" && !q.Snapshot {
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
		if q.Snapshot && q.Cursor != "" {
			if err := decodeLibraryCursor(ctx, tx, q.Cursor, &cursor); err != nil {
				return err
			}
			if cursor.Account != sc.AccountID() || cursor.Query != key || cursor.ID == "" || cursor.Created.IsZero() {
				return creativeops.ErrValidation
			}
			if cursor.Revision != settings.LibraryRevision {
				return ErrCursorStale
			}
		}
		filter, err := resolveFilter(ctx, tx, q)
		if err != nil {
			return err
		}
		var args sqlArgs
		cond := filter.condition(true, true, &args)
		result.TotalCount, err = tx.Count(ctx, "creative_assets", cond, args...)
		if err != nil {
			return err
		}
		if q.IncludeFacets {
			if result.Facets, err = readFacets(ctx, tx, filter); err != nil {
				return err
			}
		}
		order := []store.OrderBy{{Column: "created_at", Desc: q.Sort == "recent"}, {Column: "id", Desc: q.Sort == "recent"}}
		switch q.Sort {
		case "name":
			order = []store.OrderBy{{Column: "normalized_title"}, {Column: "id"}}
		case "rating":
			order = []store.OrderBy{{Column: "rating_halves", Desc: true, NullsLast: true}, {Column: "created_at", Desc: true}, {Column: "id", Desc: true}}
		}
		if q.Cursor != "" {
			cond += " AND " + cursorCondition(q.Sort, cursor, &args)
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
			next := searchCursor{Account: sc.AccountID(), Query: key, ID: last.ID, Name: textindex.Normalize(last.Title), Created: last.CreatedAt}
			if last.Rating != nil {
				h := ratingHalves(*last.Rating)
				next.Rating = &h
			}
			data, e := json.Marshal(next)
			if e != nil {
				return e
			}
			result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		}
		if err = projectAssets(ctx, tx, result.Items); err != nil {
			return err
		}
		if project != nil {
			return project(tx, &result, q, key)
		}
		return nil
	})
	return result, err
}

// projectAssets fills in what a listed asset shows: relations, a content
// preview where it may be displayed, and the palette.
func projectAssets(ctx context.Context, tx store.ReadTxAccountScope, items []Asset) error {
	if err := attachPageRelations(ctx, tx, items); err != nil {
		return err
	}
	revisionIDs := make([]string, 0, len(items))
	for _, a := range items {
		revisionIDs = append(revisionIDs, a.ContentRevisionID)
	}
	content, err := creativecontent.ReadDisplayBatch(ctx, tx, revisionIDs)
	if err != nil {
		return err
	}
	for i := range items {
		a := &items[i]
		item := content[a.ContentRevisionID]
		r, e := item.Revision, item.Err
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
	return attachPalettes(ctx, tx, items)
}

// resolveFilter checks the referenced group and tags and expands the subtree.
func resolveFilter(ctx context.Context, tx libraryReader, q Search) (searchFilter, error) {
	f := searchFilter{q: q}
	for _, id := range q.TagIDs {
		if err := requireEntity(ctx, tx, "creative_tags", id); err != nil {
			return f, err
		}
	}
	if q.GroupID == "" {
		return f, nil
	}
	if err := requireEntity(ctx, tx, "creative_asset_groups", q.GroupID); err != nil {
		return f, err
	}
	f.groups = []string{q.GroupID}
	if !q.Descendants {
		return f, nil
	}
	all, err := readGroups(ctx, tx)
	if err != nil {
		return f, err
	}
	seen := map[string]bool{q.GroupID: true}
	for i := 0; i < len(f.groups); i++ {
		for _, g := range all {
			if parentKey(g.ParentID) == f.groups[i] && !seen[g.ID] {
				seen[g.ID] = true
				f.groups = append(f.groups, g.ID)
			}
		}
	}
	return f, nil
}

func cursorCondition(sort string, c searchCursor, args *sqlArgs) string {
	switch sort {
	case "name":
		return "(normalized_title,id)>(" + args.bind(c.Name) + "," + args.bind(c.ID) + ")"
	case "rating":
		after := "(created_at,id)<(" + args.bind(c.Created) + "," + args.bind(c.ID) + ")"
		if c.Rating == nil {
			return "(rating_halves IS NULL AND " + after + ")"
		}
		r := args.bind(*c.Rating)
		return "(rating_halves<" + r + " OR rating_halves IS NULL OR (rating_halves=" + r + " AND " + after + "))"
	case "recent":
		return "(created_at,id)<(" + args.bind(c.Created) + "," + args.bind(c.ID) + ")"
	default:
		return "(created_at,id)>(" + args.bind(c.Created) + "," + args.bind(c.ID) + ")"
	}
}

// readFacets counts kinds without the kind filter, and tags either among the
// current results (all: what remains after also ticking t) or without the
// tag filter (any). Zero counts are listed so every chip shows a number.
func readFacets(ctx context.Context, tx store.ReadTxAccountScope, f searchFilter) (*Facets, error) {
	var args sqlArgs
	kinds, err := tx.GroupCount(ctx, "creative_assets", "kind", f.condition(false, true, &args), args...)
	if err != nil {
		return nil, err
	}
	facets := &Facets{Kinds: map[string]int64{}, Tags: []TagCount{}}
	for _, k := range assetKinds {
		facets.Kinds[k] = kinds[k]
	}
	args = nil
	cond := "asset_id IN (SELECT id FROM creative_assets WHERE account_id=$1 AND (" + f.condition(true, f.q.TagMode == "all", &args) + "))"
	counts, err := tx.GroupCount(ctx, "creative_asset_tags", "tag_id", cond, args...)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryPage(ctx, "creative_tags", "id", "", []store.OrderBy{{Column: "id"}}, 10001, 0)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		facets.Tags = append(facets.Tags, TagCount{TagID: id, Count: counts[id]})
	}
	return facets, rows.Err()
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

// attachPageRelations reads each relation once for the visible page.
func attachPageRelations(ctx context.Context, tx libraryReader, assets []Asset) error {
	if len(assets) == 0 {
		return nil
	}
	ids := make([]string, len(assets))
	index := make(map[string]*Asset, len(assets))
	for i := range assets {
		a := &assets[i]
		ids[i] = a.ID
		index[a.ID] = a
		a.GroupIDs = []string{}
		a.TagIDs = []string{}
	}
	for _, rel := range []struct{ table, column string }{{"creative_asset_group_members", "group_id"}, {"creative_asset_tags", "tag_id"}} {
		rows, err := tx.Query(ctx, rel.table, "asset_id,"+rel.column, "asset_id=ANY($2::text[])", ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var assetID, id string
			if err := rows.Scan(&assetID, &id); err != nil {
				rows.Close()
				return err
			}
			if rel.column == "group_id" {
				index[assetID].GroupIDs = append(index[assetID].GroupIDs, id)
			} else {
				index[assetID].TagIDs = append(index[assetID].TagIDs, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
