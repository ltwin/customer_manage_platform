package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	searchAssetsTool       = "search_assets"
	readLibraryTool        = "read_library"
	saveLibraryTool        = "save_to_library"
	conversationAssetsTool = "list_conversation_assets"
)

func (h *runtimeHandler) requireLibrary(ctx context.Context, metadata bool) error {
	return h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		if err := coversRun(c, h.held.ConversationID, nil); err != nil {
			return err
		}
		if c.Scope.Mode != ScopeAccountLibrary || metadata && !consentHas(c, "media_metadata") {
			return ErrEgressRequired
		}
		return nil
	})
}

type assetSearchArgs struct {
	Query     string   `json:"query"`
	Kind      string   `json:"kind"`
	GroupID   string   `json:"group_id"`
	Subgroups bool     `json:"include_subgroups"`
	TagIDs    []string `json:"tag_ids"`
	TagMode   string   `json:"tag_mode"`
	MinRating *float64 `json:"min_rating"`
	Unrated   bool     `json:"unrated"`
	Color     string   `json:"color"`
	Shape     string   `json:"shape"`
	View      string   `json:"view"`
	Sort      string   `json:"sort"`
	Limit     int      `json:"limit"`
	Cursor    string   `json:"cursor"`
}

type libraryName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *runtimeHandler) libraryNames(ctx context.Context, table string, ids []string) ([]libraryName, error) {
	out := []libraryName{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := h.scope.QueryPage(ctx, table, "id,name", "id=ANY($2::text[])", []store.OrderBy{{Column: "id"}}, len(ids), 0, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n libraryName
		if err := rows.Scan(&n.ID, &n.Name); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func excerpt(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit])
	}
	return text
}

func (h *runtimeHandler) searchAssets(ctx context.Context, raw string) (string, error) {
	var a assetSearchArgs
	if err := strictArguments(raw, &a); err != nil {
		return "", err
	}
	if a.Limit < 1 || a.Limit > 20 || len(a.TagIDs) > 50 || a.View == "trash" {
		return "", refuseTool("每页 1 到 20 项、最多 50 个标签，不读取回收站")
	}
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	page, err := creativelibrary.SearchAssets(ctx, h.scope, creativelibrary.Search{Q: a.Query, Kind: a.Kind, GroupID: a.GroupID, Descendants: a.Subgroups, TagIDs: a.TagIDs, TagMode: a.TagMode, MinRating: a.MinRating, Unrated: a.Unrated, Color: a.Color, Shape: a.Shape, View: a.View, Sort: a.Sort, Limit: a.Limit, Cursor: a.Cursor})
	if err != nil {
		return "", err
	}
	items := []map[string]any{}
	for _, asset := range page.Items {
		tags, err := h.libraryNames(ctx, "creative_tags", asset.TagIDs)
		if err != nil {
			return "", err
		}
		groups, err := h.libraryNames(ctx, "creative_asset_groups", asset.GroupIDs)
		if err != nil {
			return "", err
		}
		item := map[string]any{"asset_id": asset.ID, "content_revision_id": asset.ContentRevisionID, "title": asset.Title, "description": excerpt(asset.Description, 200), "kind": asset.Kind, "tags": tags, "groups": groups, "rating": asset.Rating, "is_favorite": asset.Favorite, "unavailable": asset.Unavailable}
		if asset.Content != nil {
			for _, m := range asset.Content.Media {
				if m.Width != nil && m.Height != nil {
					item["width"], item["height"] = *m.Width, *m.Height
					break
				}
			}
		}
		items = append(items, item)
	}
	projection := map[string]any{"items": items, "total_count": page.TotalCount, "next_cursor": page.NextCursor, "has_more": page.NextCursor != ""}
	out, err := json.Marshal(projection)
	if err != nil || len(out) <= h.service.limits.ToolResultBytes {
		return string(out), err
	}
	// Keep every asset and the library's cursor together. Trimming the page
	// without rebuilding its cursor would skip assets on the next request.
	for keep := 8; ; keep /= 2 {
		for _, item := range items {
			for _, field := range []string{"tags", "groups"} {
				names, _ := item[field].([]libraryName)
				if len(names) > keep {
					omitted, _ := item[field+"_omitted"].(int)
					item[field+"_omitted"] = omitted + len(names) - keep
					item[field] = names[:keep]
				}
			}
		}
		out, err = json.Marshal(projection)
		if err != nil || len(out) <= h.service.limits.ToolResultBytes {
			return string(out), err
		}
		if keep == 0 {
			return "", refuseTool("搜索结果超过读取预算，请减小 limit 或缩小筛选范围")
		}
	}
}

func (h *runtimeHandler) readLibrary(ctx context.Context, raw string) (string, error) {
	if err := strictArguments(raw, &struct{}{}); err != nil {
		return "", err
	}
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	groups, err := creativelibrary.ListGroups(ctx, h.scope)
	if err != nil {
		return "", err
	}
	categories, err := creativelibrary.ListCategories(ctx, h.scope)
	if err != nil {
		return "", err
	}
	tags, err := creativelibrary.ListTags(ctx, h.scope)
	if err != nil {
		return "", err
	}
	// Keep the projection bounded; classifications carry IDs rather than content.
	var tagCounts map[string]int64
	if err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		var err error
		tagCounts, err = tx.GroupCount(ctx, "creative_asset_tags", "tag_id", "asset_id IN (SELECT id FROM creative_assets WHERE account_id=$1 AND deleted_at IS NULL)")
		return err
	}); err != nil {
		return "", err
	}
	remaining := 500
	groupItems := []map[string]any{}
	categoryItems := []map[string]any{}
	unclassified := []map[string]any{}
	for _, g := range groups.Items {
		if remaining == 0 {
			break
		}
		remaining--
		groupItems = append(groupItems, map[string]any{"id": g.ID, "name": g.Name, "parent_id": g.ParentID, "asset_count": groups.AssetCounts[g.ID]})
	}
	byCategory := map[string][]map[string]any{}
	for _, c := range categories.Items {
		if remaining == 0 {
			break
		}
		remaining--
		categoryItems = append(categoryItems, map[string]any{"id": c.ID, "name": c.Name})
		byCategory[c.ID] = []map[string]any{}
	}
	for _, t := range tags.Items {
		if remaining == 0 {
			break
		}
		if t.CategoryID != nil {
			if _, ok := byCategory[*t.CategoryID]; !ok {
				continue
			}
		}
		remaining--
		item := map[string]any{"id": t.ID, "name": t.Name, "asset_count": tagCounts[t.ID]}
		if t.CategoryID == nil {
			unclassified = append(unclassified, item)
		} else {
			byCategory[*t.CategoryID] = append(byCategory[*t.CategoryID], item)
		}
	}
	for _, c := range categoryItems {
		id, _ := c["id"].(string)
		c["tags"] = byCategory[id]
	}
	truncated := len(groups.Items)+len(categories.Items)+len(tags.Items) > 500
	for {
		out, err := json.Marshal(map[string]any{"groups": groupItems, "categories": categoryItems, "uncategorized_tags": unclassified, "truncated": truncated})
		if err != nil || len(out) <= h.service.limits.ToolResultBytes {
			return string(out), err
		}
		truncated = true
		switch {
		case len(unclassified) > 0:
			unclassified = unclassified[:len(unclassified)-1]
		case len(categoryItems) > 0:
			categoryItems = categoryItems[:len(categoryItems)-1]
		case len(groupItems) > 0:
			groupItems = groupItems[:len(groupItems)-1]
		default:
			return "", ErrLimit
		}
	}
}

func (h *runtimeHandler) saveToLibrary(ctx context.Context, canvasID, seed, raw string) (any, error) {
	var args struct {
		NodeID      string          `json:"node_id"`
		Title       string          `json:"title"`
		Description string          `json:"description"`
		GroupIDs    []string        `json:"group_ids"`
		TagIDs      []string        `json:"tag_ids"`
		NewTags     json.RawMessage `json:"new_tags"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	if args.NewTags != nil {
		return nil, refuseTool("新建标签属于整理资产库，请使用已有标签")
	}
	if args.NodeID == "" || strings.TrimSpace(args.Title) == "" || utf8.RuneCountInString(args.Title) > 200 {
		return nil, refuseTool("需要节点 ID 和 1 到 200 字的标题")
	}
	operation := toolOperation(seed)
	_, _, err := h.applyOnce(ctx, canvasID, operation, func(canvas creativecanvas.Canvas) (any, error) {
		for _, n := range canvas.Nodes {
			if n.ID == args.NodeID {
				return creativecanvas.SaveNodeInput{CanvasID: canvasID, NodeID: n.ID, ExpectedDataRevision: n.DataRevision, Target: creativelibrary.ImportTarget{Title: args.Title, Description: args.Description, GroupIDs: args.GroupIDs, TagIDs: args.TagIDs}}, nil
			}
		}
		return nil, refuseTool("节点已不在画布上")
	}, creativecanvas.SaveNodeToLibrary)
	if err != nil {
		return nil, err
	}
	var result []byte
	if err := h.scope.QueryRow(ctx, "creative_operation_receipts", "response", "operation_id=$2", operation).Scan(&result); err != nil {
		return nil, err
	}
	var asset creativelibrary.AssetResult
	if err := json.Unmarshal(result, &asset); err != nil {
		return nil, err
	}
	return map[string]any{"asset_id": asset.ID, "content_revision_id": asset.ContentRevisionID, "reused": asset.Reused}, nil
}

func (h *runtimeHandler) resolveCanvasAssets(ctx context.Context, actions []canvasAction, commands []creativecanvas.Action) error {
	for i, a := range actions {
		if a.AssetID == "" && a.ContentRevisionID == "" {
			continue
		}
		if a.Type != "add_node" && a.Type != "fill_node" {
			return refuseTool("只有新建或填空节点能指定素材")
		}
		if a.ContentRevisionID == "" || a.Text != nil {
			return refuseTool("素材必须指定内容修订，且不能同时写正文")
		}
		if a.AssetID != "" {
			if err := h.requireLibrary(ctx, false); err != nil {
				return err
			}
			asset, err := creativelibrary.GetAsset(ctx, h.scope, a.AssetID)
			if err != nil {
				return err
			}
			if asset.DeletedAt != nil || asset.ContentRevisionID != a.ContentRevisionID {
				return refuseTool("素材已更新或进入回收站，重新查找")
			}
			commands[i].ContentRevisionID = ""
			commands[i].Asset = &creativelibrary.AssetReference{ID: asset.ID, Revision: asset.Revision, ContentRevisionID: asset.ContentRevisionID}
		}
		err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			readable, err := readableRevisionInTx(ctx, tx, h.held.RunID, h.held.ConsentID, a.ContentRevisionID, a.AssetID != "")
			if err != nil {
				return err
			}
			if !readable {
				return errReadRefused
			}
			_, err = creativecontent.RequireUsable(ctx, tx, a.ContentRevisionID, "display")
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// A separate type keeps discovery tools independent of canvas commands.
type libraryTool struct {
	handler *runtimeHandler
	name    string
}

var _ tool.InvokableTool = (*libraryTool)(nil)

func (t *libraryTool) Info(context.Context) (*schema.ToolInfo, error) {
	return assetToolInfo(t.name), nil
}
func (t *libraryTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	switch t.name {
	case searchAssetsTool:
		return t.handler.searchAssets(ctx, raw)
	case readLibraryTool:
		return t.handler.readLibrary(ctx, raw)
	case conversationAssetsTool:
		return t.handler.listConversationAssets(ctx, raw)
	}
	return "", errors.New("unknown library tool")
}
