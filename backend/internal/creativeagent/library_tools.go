package creativeagent

import (
	"context"
	"errors"

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
	AssetID   string   `json:"asset_id"`
	Untagged  bool     `json:"untagged"`
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
	if a.Limit < 1 || a.Limit > 20 || len(a.TagIDs) > 50 {
		return "", refuseTool("每页 1 到 20 项、最多 50 个标签")
	}
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	rawPage, err := creativelibrary.DiscoverAssets(ctx, h.scope, creativelibrary.Search{AssetID: a.AssetID, Untagged: a.Untagged, Q: a.Query, Kind: a.Kind, GroupID: a.GroupID, Descendants: a.Subgroups, TagIDs: a.TagIDs, TagMode: a.TagMode, MinRating: a.MinRating, Unrated: a.Unrated, Color: a.Color, Shape: a.Shape, View: a.View, Sort: a.Sort, Limit: a.Limit, Cursor: a.Cursor}, h.service.limits.ToolResultBytes)
	return string(rawPage), asRefusal(err)
}

func (h *runtimeHandler) readLibrary(ctx context.Context, raw string) (string, error) {
	var q creativelibrary.LibraryQuery
	if err := strictArguments(raw, &q); err != nil {
		return "", err
	}
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	page, err := creativelibrary.BrowseLibrary(ctx, h.scope, q, h.service.limits.ToolResultBytes)
	return string(page), asRefusal(err)
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
	case organizeAssetsTool, manageLibraryTool, trashAssetsTool, restoreAssetsTool, saveLibraryTool:
		return t.handler.runLibraryWrite(ctx, t.name, raw)
	case readLibraryResultTool:
		return t.handler.readLibraryResult(ctx, raw)
	case searchAssetsTool:
		return t.handler.searchAssets(ctx, raw)
	case readLibraryTool:
		return t.handler.readLibrary(ctx, raw)
	case conversationAssetsTool:
		return t.handler.listConversationAssets(ctx, raw)
	}
	return "", errors.New("unknown library tool")
}
