package creativeagent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	searchAssetsTool       = "search_assets"
	readLibraryTool        = "read_library"
	saveLibraryTool        = "save_to_library"
	conversationAssetsTool = "list_conversation_assets"
)

// requireLibrary lets a library tool through. A consent that no longer stands
// ends the run, as at any dispatch; a consent that stands with library search
// switched off only refuses this call — the photographer can turn it back on
// in 外发说明, and the run carries on with the canvas and the conversation.
func (h *runtimeHandler) requireLibrary(ctx context.Context, metadata bool) error {
	return h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := effectiveConsentInTx(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		if c.Scope.Mode != ScopeAccountLibrary || metadata && !consentHas(c, "media_metadata") {
			return refuseTool("素材库检索已关闭，查不了账号里的素材；内置材质和内置模型（素体）用 source: \"builtin\" 查")
		}
		return nil
	})
}

// libraryWithheld stands in for a library tool's result once library search
// is closed: the titles, tags and groups it returned are the account's library
// data, and closing the library stops them leaving with every later request.
const libraryWithheld = "素材库检索已关闭，这次素材库工具的结果不再发送；需要时可以请创作者在「外发说明」里重新打开素材库检索"

// libraryResultTool reports whether a tool's result carries the account's
// library data. args is the call's arguments when known; a search whose
// arguments are lost is taken to have searched the account.
func libraryResultTool(name, args string) bool {
	switch name {
	case searchAssetsTool:
		return args == "" || stringArgument(args, "source") != sourceBuiltin
	case readLibraryTool, readLibraryResultTool, organizeAssetsTool, manageLibraryTool,
		trashAssetsTool, restoreAssetsTool, saveLibraryTool:
		return true
	}
	return false
}

type assetSearchArgs struct {
	Source        string   `json:"source"`
	AssetID       string   `json:"asset_id"`
	Untagged      bool     `json:"untagged"`
	Query         string   `json:"query"`
	Kind          string   `json:"kind"`
	CharacterIP   string   `json:"character_ip"`
	CharacterWork string   `json:"character_work"`
	GroupID       string   `json:"group_id"`
	Subgroups     bool     `json:"include_subgroups"`
	TagIDs        []string `json:"tag_ids"`
	TagMode       string   `json:"tag_mode"`
	MinRating     *float64 `json:"min_rating"`
	Unrated       bool     `json:"unrated"`
	Color         string   `json:"color"`
	Shape         string   `json:"shape"`
	View          string   `json:"view"`
	Sort          string   `json:"sort"`
	Limit         int      `json:"limit"`
	Cursor        string   `json:"cursor"`
}

type characterSearchArguments struct {
	Message    string                                   `json:"message"`
	Candidates []creativelibrary.CharacterWorkCandidate `json:"candidates,omitempty"`
}

func (e *characterSearchArguments) Error() string { return e.Message }

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
	return h.service.searchAssets(ctx, h.scope, runReadAccess{h}, raw)
}

func (h *runtimeHandler) readLibrary(ctx context.Context, raw string) (string, error) {
	return h.service.readLibrary(ctx, h.scope, runReadAccess{h}, raw)
}

func (s *Service) searchAssets(ctx context.Context, scope store.AccountScope, access readAccess, raw string) (string, error) {
	var a assetSearchArgs
	if err := strictArguments(raw, &a); err != nil {
		return "", err
	}
	if a.Limit < 1 || a.Limit > 20 || len(a.TagIDs) > 50 {
		return "", refuseTool("每页 1 到 20 项、最多 50 个标签")
	}
	if (a.CharacterIP != "" || a.CharacterWork != "") && (a.Kind != "character" || a.Source == sourceBuiltin) {
		return "", &characterSearchArguments{Message: "character_ip 和 character_work 仅用于账号资产库的 kind=character"}
	}
	switch a.Source {
	case sourceBuiltin:
		// The platform's catalog, not the account's data: no grant asked.
		return builtinSearch(a)
	case "", "account":
	default:
		return "", refuseTool("source 只能是 account 或 builtin")
	}
	if err := access.libraryAllowed(ctx, true); err != nil {
		return "", err
	}
	rawPage, err := creativelibrary.DiscoverAssets(ctx, scope, creativelibrary.Search{AssetID: a.AssetID, Untagged: a.Untagged, Q: a.Query, Kind: a.Kind, CharacterIP: a.CharacterIP, CharacterWork: a.CharacterWork, GroupID: a.GroupID, Descendants: a.Subgroups, TagIDs: a.TagIDs, TagMode: a.TagMode, MinRating: a.MinRating, Unrated: a.Unrated, Color: a.Color, Shape: a.Shape, View: a.View, Sort: a.Sort, Limit: a.Limit, Cursor: a.Cursor}, s.limits.ToolResultBytes)
	var ambiguous *creativelibrary.AmbiguousCharacterWorkError
	if errors.As(err, &ambiguous) {
		return "", &characterSearchArguments{Message: "作品简称匹配多部作品，请补充 IP", Candidates: ambiguous.Candidates}
	}
	return string(rawPage), asRefusal(err)
}

const sourceBuiltin = "builtin"

// builtinSearch lists the built-in textured materials and models matching
// the query, all at once in catalog order: there are few, and nothing else
// narrows them. Without a kind it lists both, materials first.
func builtinSearch(a assetSearchArgs) (string, error) {
	items := []any{}
	if a.Kind == "" || a.Kind == "material" {
		for _, m := range creativescene.BuiltinMaterials(a.Query) {
			items = append(items, m)
		}
	}
	if a.Kind == "" || a.Kind == "model" {
		for _, m := range creativescene.FindBuiltinModels(a.Query) {
			items = append(items, m)
		}
	}
	out, err := json.Marshal(map[string]any{"items": items, "total_count": len(items), "has_more": false})
	return string(out), err
}

func (s *Service) readLibrary(ctx context.Context, scope store.AccountScope, access readAccess, raw string) (string, error) {
	var q creativelibrary.LibraryQuery
	if err := strictArguments(raw, &q); err != nil {
		return "", err
	}
	if err := access.libraryAllowed(ctx, true); err != nil {
		return "", err
	}
	page, err := creativelibrary.BrowseLibrary(ctx, scope, q, s.limits.ToolResultBytes)
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
			scope, err := readScopeInTx(ctx, tx)
			if err != nil {
				return err
			}
			readable, err := readableRevisionInTx(ctx, tx, h.held.RunID, scope, a.ContentRevisionID, a.AssetID != "")
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
