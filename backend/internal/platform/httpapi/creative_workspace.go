package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	cw "github.com/samson/customer-manage-platform/backend/internal/creativeworkspace"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type creativeHandlers struct {
	service   *cw.Service
	pilot     *cw.Pilot
	factory   ScopeFactory
	idem      *idempotency.Executor
	canEnroll func(string) bool
}

type creativeCommand struct {
	Operation        string  `json:"operation"`
	ExpectedRevision int64   `json:"expected_revision,omitempty"`
	Name             *string `json:"name,omitempty"`
	Link             *struct {
		Kind cw.LinkKind `json:"kind"`
		ID   string      `json:"id"`
	} `json:"link,omitempty"`
	Archived      *bool           `json:"archived,omitempty"`
	IDs           []string        `json:"ids,omitempty"`
	ID            string          `json:"id,omitempty"`
	GroupID       *string         `json:"group_id,omitempty"`
	ToWorkspaceID string          `json:"to_workspace_id,omitempty"`
	Text          *string         `json:"text,omitempty"`
	Caption       *string         `json:"caption,omitempty"`
	Title         *string         `json:"title,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Checked       *bool           `json:"checked,omitempty"`
	Result        *cw.ShootResult `json:"result,omitempty"`
	ResultNote    *string         `json:"result_note,omitempty"`
	ClearResult   bool            `json:"clear_result,omitempty"`
}
type creativeImport struct {
	RawText  string   `json:"raw_text"`
	AssetIDs []string `json:"asset_ids"`
	GroupID  *string  `json:"group_id,omitempty"`
}
type creativeShots struct {
	CardIDs []string `json:"card_ids"`
	Merge   bool     `json:"merge"`
}

func registerCreativeWorkspace(r gin.IRouter, deps RouterDeps) {
	h := &creativeHandlers{service: deps.CreativeWorkspace, pilot: deps.CreativePilot, factory: deps.ScopeFactory, idem: deps.Idempotency, canEnroll: deps.CreativeEnrollmentAllowed}
	r.GET("/creative-pilot", h.state)
	r.GET("/creative-pilot/preflight", h.preflight)
	r.POST("/creative-pilot/enroll", h.enroll)
	r.POST("/creative-pilot/stop", h.stop)
	r.GET("/creative-workspaces", h.list)
	r.POST("/creative-workspaces", h.create)
	r.GET("/creative-workspaces/:id", h.detail)
	r.POST("/creative-workspaces/:id/observations", h.observe)
	r.PATCH("/creative-workspaces/:id", h.command)
	r.POST("/creative-workspaces/:id/cards", h.importCards)
	r.POST("/creative-workspaces/:id/shoot-items", h.createShots)
	r.POST("/creative-workspaces/:id/memos", h.createMemos)
	r.POST("/creative-workspaces/:id/assets", h.upload)
	r.GET("/creative-workspaces/:id/assets/:assetId/content", h.content)
}
func (h *creativeHandlers) scope(c *gin.Context) store.AccountScope {
	ac, _ := auth.AccountContextFrom(c.Request.Context())
	return h.factory.ScopeFor(ac)
}
func (h *creativeHandlers) fail(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, cw.ErrNotFound), errors.Is(err, cw.ErrLinkTargetNotFound), errors.Is(err, cw.ErrCardNotInWorkspace):
		abortError(c, 404, "not_found", "内容不存在或已移走")
	case errors.Is(err, cw.ErrValidation):
		abortError(c, 400, "validation_failed", err.Error())
	case errors.Is(err, idempotency.ErrValidation):
		abortError(c, 400, "validation_failed", "请求或幂等键不合法")
	case errors.Is(err, idempotency.ErrConflict):
		abortError(c, 409, "idempotency_conflict", "这次操作的内容已改变，请重新发起")
	case errors.Is(err, cw.ErrPilotRequired):
		abortError(c, 409, "pilot_required", "创意空间当前不可编辑")
	case errors.Is(err, cw.ErrWorkspaceArchived):
		abortError(c, 409, "workspace_archived", "空间已归档，请先恢复")
	case errors.Is(err, cw.ErrInboxImmutable):
		abortError(c, 409, "inbox_immutable", "未归类空间不能改名、关联或归档")
	case errors.Is(err, cw.ErrRevisionConflict):
		abortError(c, 409, "workspace_revision_conflict", "内容已更新，请刷新后重试")
	case errors.Is(err, cw.ErrMediaUnsupported):
		abortError(c, 400, "media_unsupported", "请使用支持的图片格式，暂不支持视频")
	default:
		_ = c.Error(err)
	}
	return true
}
func (h *creativeHandlers) state(c *gin.Context) {
	v, e := h.pilot.State(c.Request.Context(), h.scope(c))
	v.CanEnroll = h.canEnroll != nil && h.canEnroll(h.scope(c).AccountID())
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) preflight(c *gin.Context) {
	v, e := h.pilot.Preflight(c.Request.Context(), h.scope(c))
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) enroll(c *gin.Context) {
	if h.canEnroll == nil || !h.canEnroll(h.scope(c).AccountID()) {
		abortError(c, 403, CodeForbidden, "本账号尚未加入创意空间先导试用")
		return
	}

	var b struct {
		WindowID string `json:"window_id"`
	}
	if e := decodeStrictRequest(c, &b); e != nil {
		abortError(c, 400, CodeValidationFailed, "请求体格式错误")
		return
	}
	v, pre, e := h.pilot.Enroll(c.Request.Context(), h.scope(c), b.WindowID)
	if errors.Is(e, cw.ErrPreflightFailed) {
		c.JSON(409, struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Preflight cw.PreflightResult `json:"preflight"`
		}{
			Error: struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{Code: "pilot_preflight_failed", Message: "请先完成旧策划的清场事项"}, Preflight: pre,
		})
		return
	}
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) stop(c *gin.Context) {
	v, e := h.pilot.Stop(c.Request.Context(), h.scope(c))
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) list(c *gin.Context) {
	var v []cw.Workspace
	var e error
	if kind := c.Query("link_kind"); kind != "" {
		v, e = h.service.WorkspacesLinkedTo(c.Request.Context(), h.scope(c), cw.LinkInput{Kind: cw.LinkKind(kind), ID: c.Query("link_id")})
	} else {
		v, e = h.service.ListWorkspaces(c.Request.Context(), h.scope(c), c.Query("archived") == "true")
	}
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) detail(c *gin.Context) {
	v, e := h.service.GetWorkspaceDetail(c.Request.Context(), h.scope(c), c.Param("id"), false)
	if !h.fail(c, e) {
		c.JSON(200, v)
	}
}
func (h *creativeHandlers) create(c *gin.Context) {
	var b struct {
		Name string `json:"name"`
	}
	if e := decodeStrictRequest(c, &b); e != nil {
		abortError(c, 400, CodeValidationFailed, "空间名称格式错误")
		return
	}
	h.createOnce(c, "creative-workspace.create.v1", b, func(tx store.TxAccountScope) (any, error) {
		return h.service.CreateWorkspaceInScope(c.Request.Context(), tx, cw.CreateWorkspaceInput{Name: b.Name})
	})
}
func (h *creativeHandlers) importCards(c *gin.Context) {
	var b creativeImport
	if e := decodeStrictRequest(c, &b); e != nil {
		abortError(c, 400, CodeValidationFailed, "搬入内容格式错误")
		return
	}
	h.createOnce(c, "creative-workspace.cards.import.v1", b, func(tx store.TxAccountScope) (any, error) {
		return h.service.ImportCardsInScope(c.Request.Context(), tx, cw.ImportCardsInput{WorkspaceID: c.Param("id"), RawText: b.RawText, AssetIDs: b.AssetIDs, GroupID: b.GroupID})
	})
}
func (h *creativeHandlers) createShots(c *gin.Context) {
	var b creativeShots
	if e := decodeStrictRequest(c, &b, "card_ids"); e != nil {
		abortError(c, 400, CodeValidationFailed, "请选择素材")
		return
	}
	h.createOnce(c, "creative-workspace.shoot-items.create.v1", b, func(tx store.TxAccountScope) (any, error) {
		return h.service.CreateShootItemsInScope(c.Request.Context(), tx, cw.CreateShootItemsInput{WorkspaceID: c.Param("id"), CardIDs: b.CardIDs, Merge: b.Merge})
	})
}
func (h *creativeHandlers) createMemos(c *gin.Context) {
	var b struct {
		Text string `json:"text"`
	}
	if e := decodeStrictRequest(c, &b, "text"); e != nil {
		abortError(c, 400, CodeValidationFailed, "请输入拍摄备忘")
		return
	}
	h.createOnce(c, "creative-workspace.memos.create.v1", b, func(tx store.TxAccountScope) (any, error) {
		return h.service.CreateMemosInScope(c.Request.Context(), tx, cw.CreateMemosInput{WorkspaceID: c.Param("id"), Text: b.Text})
	})
}
func (h *creativeHandlers) createOnce(c *gin.Context, op idempotency.Operation, body any, fn func(store.TxAccountScope) (any, error)) {
	sc := h.scope(c)
	ctx := c.Request.Context()
	if h.fail(c, h.pilot.CanWriteNew(ctx, sc)) {
		return
	}
	canonical, err := json.Marshal(struct {
		Path string `json:"path"`
		Body any    `json:"body"`
	}{Path: c.Request.URL.Path, Body: body})
	if h.fail(c, err) {
		return
	}
	if h.idem == nil {
		h.fail(c, errors.New("creative idempotency dependency missing"))
		return
	}
	result, err := h.idem.ExecuteCreate(ctx, sc, op, c.GetHeader("Idempotency-Key"), canonical, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		v, e := fn(tx)
		if e != nil {
			return idempotency.StoredResponse{}, e
		}
		data, e := json.Marshal(v)
		return idempotency.StoredResponse{Status: 201, Body: data}, e
	})
	if !h.fail(c, err) {
		c.Data(result.Status, "application/json", result.Body)
	}
}
func (h *creativeHandlers) command(c *gin.Context) {
	var b creativeCommand
	if e := decodeStrictRequest(c, &b, "operation"); e != nil {
		abortError(c, 400, CodeValidationFailed, "操作格式错误")
		return
	}
	if h.fail(c, h.apply(c.Request.Context(), h.scope(c), c.Param("id"), b)) {
		return
	}
	d, e := h.service.GetWorkspaceDetail(c.Request.Context(), h.scope(c), c.Param("id"), false)
	if !h.fail(c, e) {
		c.JSON(200, d)
	}
}
func (h *creativeHandlers) apply(ctx context.Context, sc store.AccountScope, id string, b creativeCommand) error {
	switch b.Operation {
	case "update_workspace":
		in := cw.UpdateWorkspaceInput{ExpectedRevision: b.ExpectedRevision, Name: b.Name, Archived: b.Archived}
		if b.Link != nil {
			in.Link = &cw.LinkInput{Kind: b.Link.Kind, ID: b.Link.ID}
		}
		_, e := h.service.UpdateWorkspace(ctx, sc, id, in)
		return e
	case "reorder_cards":
		return h.service.ReorderCards(ctx, sc, cw.ReorderInput{WorkspaceID: id, CardIDs: b.IDs})
	case "update_card":
		_, e := h.service.UpdateCard(ctx, sc, id, b.ID, cw.UpdateCardInput{Text: b.Text, Caption: b.Caption})
		return e
	case "remove_card":
		return h.service.RemoveCard(ctx, sc, id, b.ID)
	case "move_card":
		_, e := h.service.MoveCard(ctx, sc, cw.MoveCardInput{FromWorkspaceID: id, CardID: b.ID, ToWorkspaceID: b.ToWorkspaceID, ToGroupID: b.GroupID})
		return e

	case "create_group":
		name := ""
		if b.Name != nil {
			name = *b.Name
		}
		_, e := h.service.CreateGroup(ctx, sc, cw.CreateGroupInput{WorkspaceID: id, Name: name, CardIDs: b.IDs})
		return e
	case "rename_group":
		if b.Name == nil {
			return cw.ValidationError{Message: "请输入分组名称"}
		}
		return h.service.RenameGroup(ctx, sc, id, b.ID, *b.Name)
	case "delete_group":
		return h.service.DeleteGroup(ctx, sc, id, b.ID)
	case "set_group":
		return h.service.SetCardGroup(ctx, sc, cw.SetCardGroupInput{WorkspaceID: id, CardIDs: b.IDs, GroupID: b.GroupID})
	case "update_shoot_item", "remove_shoot_item":
		_, e := h.service.UpdateShootItem(ctx, sc, id, b.ID, cw.UpdateShootItemInput{ExpectedRevision: b.ExpectedRevision, Title: b.Title, Description: b.Description, Result: b.Result, ResultNote: b.ResultNote, ClearResult: b.ClearResult, Tombstone: b.Operation == "remove_shoot_item"})
		return e
	case "reorder_shoot_items":
		return h.service.ReorderShootItems(ctx, sc, cw.ReorderShootItemsInput{WorkspaceID: id, ItemIDs: b.IDs})
	case "update_memo":
		return h.service.UpdateMemo(ctx, sc, id, b.ID, cw.UpdateMemoInput{Text: b.Text, Checked: b.Checked})
	case "delete_memo":
		return h.service.DeleteMemo(ctx, sc, id, b.ID)
	case "reorder_memos":
		return h.service.ReorderMemos(ctx, sc, cw.ReorderMemosInput{WorkspaceID: id, MemoIDs: b.IDs})
	default:
		return cw.ValidationError{Message: "不支持的操作"}
	}
}
func (h *creativeHandlers) upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, planningmedia.MaxUploadBytes+2*1024*1024)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		abortError(c, 400, CodeValidationFailed, "图片过大或格式错误")
		return
	}
	defer func() { _ = file.Close() }() // Multipart reader cleanup cannot affect the committed response.
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }() // Best-effort temporary upload cleanup.
	}
	data, err := io.ReadAll(io.LimitReader(file, planningmedia.MaxUploadBytes+1))
	if err != nil || len(data) > planningmedia.MaxUploadBytes {
		abortError(c, 400, CodeValidationFailed, "图片超过大小限制")
		return
	}
	v, err := h.service.UploadAsset(c.Request.Context(), h.scope(c), cw.UploadAssetInput{WorkspaceID: c.Param("id"), DeclaredMediaType: header.Header.Get("Content-Type"), Bytes: data})
	if !h.fail(c, err) {
		c.JSON(201, v)
	}
}
func (h *creativeHandlers) content(c *gin.Context) {
	v, err := h.service.OpenDisplay(c.Request.Context(), h.scope(c), c.Param("id"), c.Param("assetId"), c.Query("v"))
	if h.fail(c, err) {
		return
	}
	defer func() { _ = v.Body.Close() }() // Response stream is read-only.
	c.Header("ETag", v.ETag)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, v.Size, v.MediaType, v.Body, nil)
}

// observe stores raw evidence only. Online timestamps are server-owned; offline
// reports remain explicitly unverified even after they are submitted online.
func (h *creativeHandlers) observe(c *gin.Context) {
	var b struct {
		EventID   string    `json:"event_id"`
		SessionID string    `json:"session_id"`
		Kind      string    `json:"kind"`
		ClientAt  time.Time `json:"client_at"`
	}
	if err := decodeStrictRequest(c, &b, "event_id", "session_id", "kind", "client_at"); err != nil || len(b.EventID) < 8 || len(b.EventID) > 128 || len(b.SessionID) < 8 || len(b.SessionID) > 128 || (b.Kind != "space_open" && b.Kind != "live_open" && b.Kind != "live_unverified") || b.ClientAt.IsZero() {
		abortError(c, 400, CodeValidationFailed, "打开记录格式错误")
		return
	}
	ctx := c.Request.Context()
	sc := h.scope(c)
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		state, err := cw.NewPostgresRepository().GetPilot(ctx, tx)
		if err != nil {
			return err
		}
		if state.State != cw.PilotNewWrite {
			return cw.ErrPilotRequired
		}
		if _, err := cw.NewPostgresRepository().GetWorkspace(ctx, tx, c.Param("id")); err != nil {
			return err
		}
		now := time.Now().UTC()
		if b.Kind != "live_unverified" {
			b.ClientAt = now
		}
		var eventID string
		err = tx.InsertOnConflictDoNothingReturning(ctx, "creative_observation_events", []string{"event_id", "workspace_id", "session_id", "kind", "client_at", "received_at"}, []string{"account_id", "event_id"}, []string{"event_id"}, b.EventID, c.Param("id"), b.SessionID, b.Kind, b.ClientAt, now).Scan(&eventID)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		return err
	})
	if !h.fail(c, err) {
		c.Status(204)
	}
}
