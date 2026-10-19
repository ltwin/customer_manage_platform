package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type editGraph struct {
	ctx       context.Context
	tx        store.TxAccountScope
	c         Canvas
	before, g graphState
	ids       map[string]identity
	required  map[string]map[string]bool
	changeID  string
	mapping   map[string]string
	omitted   []string
	staged    []placedUpload
	// actor writes the documents add_node creates; zero is the photographer.
	actor creativedocument.Actor
}

// placedUpload is a staged upload consumed by this batch; it is marked placed
// once the node's final data revision is known.
type placedUpload struct {
	staged creativestaging.Staged
	nodeID string
}

func ApplyCommands(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return ApplyCommandsAs(ctx, scope, creativedocument.Photographer, command)
}

// ApplyCommandsAs is ApplyCommands for a server-side writer: the documents
// its add_node actions create are written by actor, such as an assistant
// run. The actor never comes from the payload.
func ApplyCommandsAs(ctx context.Context, scope store.AccountScope, actor creativedocument.Actor, command creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.commands", command, validateBatch, func(ctx context.Context, tx store.TxAccountScope, v BatchInput) (creativeops.Outcome, error) {
		r, err := applyBatchAs(ctx, tx, v, command.OperationID, actor)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "canvas_change", r.ChangeID, r.ResultRevision, r)
	})
}
func validateBatch(v BatchInput) error {
	switch {
	case v.CanvasID == "":
		return creativeops.Explain(creativeops.ErrValidation, "缺少画布")
	case len(v.Actions) == 0 || len(v.Actions) > 100:
		return creativeops.Explain(creativeops.ErrValidation, "一次需要 1 到 100 个动作")
	case len(v.ReadSet) > 5000 || len(v.ChangeGroupID) > 100:
		return creativeops.Explain(creativeops.ErrValidation, "读取集合最多 5000 项，变更分组标识最长 100 个字符")
	}
	for i, a := range v.Actions {
		switch {
		case !validPoint(a.X, a.Y) || !validPoint(a.DX, a.DY):
			return creativeops.Explain(creativeops.ErrValidation, "第 %d 个动作：坐标和位移的绝对值不能超过 10000000", i+1)
		case len(a.NodeIDs) > 100:
			return creativeops.Explain(creativeops.ErrValidation, "第 %d 个动作：一次最多选 100 个节点", i+1)
		case utf8.RuneCountInString(a.Title) > 200:
			return creativeops.Explain(creativeops.ErrValidation, "第 %d 个动作：标题不能超过 200 字", i+1)
		case utf8.RuneCountInString(a.Intent) > 2000:
			return creativeops.Explain(creativeops.ErrValidation, "第 %d 个动作：意图说明不能超过 2000 字", i+1)
		}
	}
	return nil
}
func applyBatch(ctx context.Context, tx store.TxAccountScope, v BatchInput, operation string) (ChangeResult, error) {
	return applyBatchAs(ctx, tx, v, operation, creativedocument.Photographer)
}

func applyBatchAs(ctx context.Context, tx store.TxAccountScope, v BatchInput, operation string, actor creativedocument.Actor) (ChangeResult, error) {
	c, err := lockCanvas(ctx, tx, v.CanvasID, true)
	if err != nil {
		return ChangeResult{}, err
	}
	before, ids, err := loadGraph(ctx, tx, c.ID)
	if err != nil {
		return ChangeResult{}, err
	}
	e := editGraph{ctx: ctx, tx: tx, c: c, before: before, g: cloneGraph(before), ids: ids, required: map[string]map[string]bool{}, changeID: newChangeID(), mapping: map[string]string{}, omitted: []string{}, actor: actor}
	structural := false
	for _, a := range v.Actions {
		switch a.Type {
		case "replace_content", "fill_from_asset", "fill_from_upload", "update_metadata", "clear_content", "select_version", "delete_version", "move_nodes", "move_node", "resize_node":
		default:
			structural = true
		}
		if err = e.apply(a); err != nil {
			return ChangeResult{}, err
		}
	}
	if structural && v.ExpectedTopologyRevision != c.TopologyRevision {
		return ChangeResult{}, ErrVersionConflict
	}
	if err = e.expandGroups(); err != nil {
		return ChangeResult{}, err
	}
	if err = validateGraph(e.g); err != nil {
		return ChangeResult{}, err
	}
	changes, err := planChanges(before, e.g, ids)
	if err != nil {
		return ChangeResult{}, err
	}
	for _, f := range changes {
		if _, exists := before.Nodes[f.ID]; exists {
			field := f.Field
			if field == "relations" || field == "existence" {
				e.need(f.ID, "placement")
				field = "data"
			}
			e.need(f.ID, field)
		} else if _, exists := before.Edges[f.ID]; exists {
			e.need(f.ID, "data")
		} else if _, exists := before.Versions[f.ID]; exists {
			e.need(f.ID, "data")
		} else if _, exists := before.Inputs[f.ID]; exists {
			e.need(f.ID, "data")
		}
	}
	if err = validateReadSet(v.ReadSet, e.required, ids); err != nil {
		return ChangeResult{}, err
	}
	result, err := persistGraph(ctx, tx, c, before, e.g, ids, changes, operation, e.changeID, v.ChangeGroupID, "", e.mapping, e.omitted)
	if err != nil {
		return ChangeResult{}, err
	}
	for _, p := range e.staged {
		var revision creativeops.Revision
		for _, r := range result.ObjectResults {
			if r.ID == p.nodeID {
				revision = r.DataRevision
			}
		}
		if err = creativestaging.Place(ctx, tx, p.staged, result.ChangeID, p.nodeID, int64(revision)); err != nil {
			return ChangeResult{}, err
		}
	}
	// Successful move receipts acknowledge every submitted target, including no-ops.
	seen := map[string]bool{}
	for _, r := range result.ObjectResults {
		seen[r.ID] = true
	}
	for _, a := range v.Actions {
		var moved []string
		switch a.Type {
		case "move_node":
			moved = []string{a.NodeID}
		case "move_nodes":
			moved = a.NodeIDs
		}
		for _, id := range moved {
			if seen[id] {
				continue
			}
			if n, live := e.g.Nodes[id]; live {
				result.ObjectResults = append(result.ObjectResults, ObjectResult{Kind: "node", ID: id, IsLive: true, PlacementRevision: n.PlacementRevision, DataRevision: n.DataRevision})
				seen[id] = true
			}
		}
	}
	return result, nil
}
func (e *editGraph) need(id, field string) {
	if e.required[id] == nil {
		e.required[id] = map[string]bool{}
	}
	e.required[id][field] = true
}
func validateReadSet(reads []ObjectRead, required map[string]map[string]bool, ids map[string]identity) error {
	by := map[string]ObjectRead{}
	for _, r := range reads {
		if _, ok := by[r.ID]; ok {
			return creativeops.Explain(creativeops.ErrValidation, "读取集合里 %s 出现了不止一次", r.ID)
		}
		by[r.ID] = r
	}
	for id, fields := range required {
		ident, exists := ids[id]
		if !exists {
			continue
		}
		r, ok := by[id]
		if !ok || r.Kind != ident.Kind {
			return creativeops.Explain(creativeops.ErrValidation, "修改 %s 之前要先读取它，请重新读取画布", id)
		}
		if ident.Kind == "node" {
			if fields["placement"] {
				if r.PlacementRevision < 1 {
					return unreadRevision(id)
				}
				if r.PlacementRevision != ident.P {
					return ErrVersionConflict
				}
			}
			if fields["data"] {
				if r.DataRevision < 1 {
					return unreadRevision(id)
				}
				if r.DataRevision != ident.D {
					return ErrVersionConflict
				}
			}
		} else {
			if r.Revision < 1 {
				return unreadRevision(id)
			}
			if r.Revision != ident.D {
				return ErrVersionConflict
			}
		}
	}
	return nil
}
func unreadRevision(id string) error {
	return creativeops.Explain(creativeops.ErrValidation, "读取集合里 %s 的版本号无效，请重新读取画布", id)
}
func (e *editGraph) node(id, field string) (Node, error) {
	n, ok := e.g.Nodes[id]
	if !ok {
		return n, ErrNotFound
	}
	if d, ok := definition(n.TypeKey); !ok || d.SchemaVersion != n.TypeVersion {
		return n, unsupportedType(n)
	}
	e.need(id, field)
	return n, nil
}
func unsupportedType(n Node) error {
	return creativeops.Explain(creativeops.ErrValidation, "节点 %s 的类型 %s（第 %d 版）已不受支持，不能修改", n.ID, n.TypeKey, n.TypeVersion)
}
func (e *editGraph) ancestors(n Node) error {
	seen := map[string]bool{}
	for n.ParentID != nil {
		if seen[*n.ParentID] {
			return creativeops.Explain(creativeops.ErrValidation, "节点 %s 的分组层级绕成了环", n.ID)
		}
		seen[*n.ParentID] = true
		p, err := e.node(*n.ParentID, "placement")
		if err != nil {
			return err
		}
		n = p
	}
	return nil
}
func (e *editGraph) roots(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, creativeops.Explain(creativeops.ErrValidation, "至少要选一个节点")
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if _, err := e.node(id, "placement"); err != nil {
			return nil, err
		}
		selected[id] = true
	}
	out := []string{}
	for id := range selected {
		covered := false
		n := e.g.Nodes[id]
		if err := e.ancestors(n); err != nil {
			return nil, err
		}
		for p := n.ParentID; p != nil; {
			if selected[*p] {
				covered = true
				break
			}
			p = e.g.Nodes[*p].ParentID
		}
		if !covered {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}
func (e *editGraph) apply(a Action) error {
	switch a.Type {
	case "add_node":
		return e.add(a)
	case "move_nodes":
		roots, err := e.roots(a.NodeIDs)
		if err != nil {
			return err
		}
		for _, id := range roots {
			n := e.g.Nodes[id]
			n.X += a.DX
			n.Y += a.DY
			e.g.Nodes[id] = n
		}
		return nil
	case "move_node":
		n, err := e.node(a.NodeID, "placement")
		if err != nil {
			return err
		}
		if err = e.ancestors(n); err != nil {
			return err
		}
		n.X = a.X
		n.Y = a.Y
		e.g.Nodes[n.ID] = n
		return nil
	case "resize_node":
		n, err := e.node(a.NodeID, "placement")
		if err != nil {
			return err
		}
		if !validResize(n, a.Width, a.Height) {
			return creativeops.Explain(creativeops.ErrValidation, "节点 %s：宽度须在 %d 到 %d 之间、高度不小于 %d，分组不能缩放", n.ID, minNodeWidth, maxNodeWidth, minNodeHeight)
		}
		n.Width = a.Width
		n.Height = a.Height
		if n.ContentRevisionID != nil {
			r, err := creativecontent.RequireUsable(e.ctx, e.tx, *n.ContentRevisionID, "display")
			if err != nil {
				return err
			}
			fitMediaSize(&n, r)
		}
		e.g.Nodes[n.ID] = n
		return e.ancestors(n)
	case "update_metadata":
		n, err := e.node(a.NodeID, "data")
		if err != nil {
			return err
		}
		n.Title = a.Title
		n.Intent = a.Intent
		e.g.Nodes[n.ID] = n
		return nil
	case "select_version":
		return e.selectVersion(a)
	case "delete_version":
		return e.deleteVersion(a)
	case "clear_content":
		n, err := e.node(a.NodeID, "data")
		if err != nil {
			return err
		}
		if n.TypeKey == "core.group" {
			return creativeops.Explain(creativeops.ErrValidation, "%s 是分组，没有内容可清空", n.ID)
		}
		n.ContentID = nil
		n.ContentRevisionID = nil
		n.SelectedVersionID = nil
		e.g.Nodes[n.ID] = n
		return nil
	case "replace_content":
		return e.replace(a)
	case "fill_from_asset":
		return e.fill(a)
	case "fill_from_upload":
		return e.fillUpload(a)
	case "connect_reference":
		return e.connect(a)
	case "disconnect_reference":
		edge, ok := e.g.Edges[a.EdgeID]
		if !ok {
			return ErrNotFound
		}
		if _, err := e.node(edge.SourceNodeID, "data"); err != nil {
			return err
		}
		if _, err := e.node(edge.TargetNodeID, "data"); err != nil {
			return err
		}
		e.need(edge.ID, "data")
		delete(e.g.Edges, edge.ID)
		return nil
	case "set_node_inputs":
		return e.setInputs(a)
	case "group_nodes":
		return e.group(a)
	case "ungroup_nodes":
		return e.ungroup(a.NodeID)
	case "reparent_nodes":
		return e.reparent(a)
	case "duplicate_selection":
		return e.duplicate(a)
	case "remove_nodes":
		return e.remove(a)
	default:
		return creativeops.Explain(creativeops.ErrValidation, "不支持的动作类型 %q", a.Type)
	}
}

// 手动缩放的尺寸约束（PRD CV-17，owner 2026-10-06 定稿）；前端 outbox.nodeSizeLimits 与此一致。
const (
	minNodeWidth  = 160
	maxNodeWidth  = 1200
	minNodeHeight = 100
)

// validResize：组不能手动缩放，由子节点撑开。有内容的图片/视频高度随后按
// 内容比例重算，只校验宽度；其余节点宽高都要在约束内。
func validResize(n Node, width, height float64) bool {
	if n.TypeKey == "core.group" || width < minNodeWidth || width > maxNodeWidth {
		return false
	}
	media := (n.TypeKey == "core.image" || n.TypeKey == "core.video") && n.ContentRevisionID != nil
	return media || height >= minNodeHeight
}

// 新建节点的默认尺寸（PRD CV-14 / CV-B0 / CV-C0）；前端 outbox.defaultNodeSize 与此一致。
func defaultNodeSize(typeKey string) (width, height float64) {
	switch typeKey {
	case "core.image", "core.video":
		return 300, 300
	case "core.link":
		return 300, 140
	case "core.audio":
		return 300, 120
	default:
		return 300, 220
	}
}

// copyTitle 给复制的根节点加「 副本」；超出标题上限时截原标题，保留完整后缀。
func copyTitle(title string) string {
	const suffix = " 副本"
	if title == "" {
		return "副本"
	}
	limit := 200 - utf8.RuneCountInString(suffix)
	if r := []rune(title); len(r) > limit {
		title = string(r[:limit])
	}
	return title + suffix
}

func (e *editGraph) add(a Action) error {
	docType, isDocument := documentType(a.TypeKey)
	switch {
	case !validNodeID(a.NodeID):
		return creativeops.Explain(creativeops.ErrValidation, "新节点 ID %q 格式不对，须是 cwnode_ 加 UUID", a.NodeID)
	case !knownType(a.TypeKey):
		return creativeops.Explain(creativeops.ErrValidation, "不能新建 %q 类型的节点", a.TypeKey)
	case a.Asset != nil && a.Content != nil || a.ContentRevisionID != "" && (a.Asset != nil || a.Content != nil):
		return creativeops.Explain(creativeops.ErrValidation, "新建节点时素材、已有内容修订和正文只能选一项")
	case !isDocument && (a.DocumentID != "" || len(a.DocumentOptions) != 0):
		return creativeops.Explain(creativeops.ErrValidation, "%q 节点不能挂文档", a.TypeKey)
	case isDocument && (a.Asset != nil || a.Content != nil || a.ContentRevisionID != ""):
		return creativeops.Explain(creativeops.ErrValidation, "文档节点不能带素材、内容修订或正文")
	case a.DocumentID != "" && len(a.DocumentOptions) != 0:
		return creativeops.Explain(creativeops.ErrValidation, "放回已有文档时不能带 document_options")
	case isDocument && a.DocumentID == "" && !docType.Creatable():
		return creativeops.Explain(creativeops.ErrValidation, "不能新建 %q 类型的节点", a.TypeKey)
	}
	exists, err := e.tx.Exists(e.ctx, "creative_graph_identities", "id=$2", a.NodeID)
	if err != nil {
		return err
	}
	if _, ok := e.g.Nodes[a.NodeID]; ok || exists {
		return ErrVersionConflict
	}
	w, h := defaultNodeSize(a.TypeKey)
	n := Node{ID: a.NodeID, TypeKey: a.TypeKey, TypeVersion: 1, Title: a.Title, X: a.X, Y: a.Y, Width: w, Height: h, ParentID: a.ParentID, Config: json.RawMessage("{}"), PlacementRevision: 1, DataRevision: 1, StatusRevision: 1}
	if n.TypeKey == "core.group" && (a.Content != nil || a.Asset != nil || a.ContentRevisionID != "") {
		return creativeops.Explain(creativeops.ErrValidation, "分组不能带内容或素材")
	}
	if a.ParentID != nil {
		p, err := e.node(*a.ParentID, "placement")
		if err != nil {
			return err
		}
		if p.TypeKey != "core.group" {
			return notGroup(p)
		}
		if err = e.ancestors(p); err != nil {
			return err
		}
	}
	if isDocument {
		if err := e.attachDocument(&n, docType, a); err != nil {
			return err
		}
	}
	if a.Asset != nil {
		asset, err := creativelibrary.ResolveAssetsInTx(e.ctx, e.tx, *a.Asset)
		if err != nil {
			return err
		}
		r, err := creativecontent.RequireUsable(e.ctx, e.tx, asset.ContentRevisionID, "display")
		if err != nil {
			return err
		}
		if n.TypeKey != "core."+r.Kind {
			return kindMismatch(n, r.Kind)
		}
		n.ContentID = &r.ContentID
		n.ContentRevisionID = &r.ID
		fitMediaSize(&n, r)
		if err = e.manualVersion(&n, r, "asset"); err != nil {
			return err
		}
	}
	if a.ContentRevisionID != "" {
		r, err := creativecontent.RequireUsable(e.ctx, e.tx, a.ContentRevisionID, "display")
		if err != nil {
			return err
		}
		if n.TypeKey != "core."+r.Kind {
			return kindMismatch(n, r.Kind)
		}
		n.ContentID, n.ContentRevisionID = &r.ContentID, &r.ID
		fitMediaSize(&n, r)
		if err := e.manualVersion(&n, r, "conversation"); err != nil {
			return err
		}
	}
	if a.Content != nil {
		if n.TypeKey != "core."+a.Content.Kind {
			return kindMismatch(n, a.Content.Kind)
		}
		r, err := creativecontent.WriteAndRetain(e.ctx, e.tx, *a.Content, "", &n.ID, func(r creativecontent.Revision) error { return retainChange(e.ctx, e.tx, e.changeID, r.ID) })
		if err != nil {
			return err
		}
		n.ContentID = &r.ContentID
		n.ContentRevisionID = &r.ID
	}
	e.g.Nodes[n.ID] = n
	return nil
}

// attachDocument creates the node's document in this transaction, or checks
// that an existing one may go on the canvas. Undoing the change later removes
// only the node; the document keeps its own history.
func (e *editGraph) attachDocument(n *Node, t creativedocument.Type, a Action) error {
	var (
		doc creativedocument.Document
		err error
	)
	if a.DocumentID != "" {
		doc, err = creativedocument.RequirePlaceable(e.ctx, e.tx, a.DocumentID, a.TypeKey)
	} else {
		actor := e.actor
		if actor.Kind == "" {
			actor = creativedocument.Photographer
		}
		doc, err = creativedocument.CreateInTx(e.ctx, e.tx, t, a.Title, a.DocumentOptions, actor)
	}
	if err != nil {
		return err
	}
	n.DocumentID = &doc.ID
	if a.DocumentID == "" || n.Title == "" {
		n.Title = doc.Title
	}
	n.Width, n.Height = 380, 260
	return nil
}

func notGroup(n Node) error {
	return creativeops.Explain(creativeops.ErrValidation, "%s 不是分组，不能作为上级", n.ID)
}

// kindMismatch is content of one kind going into a node of another.
func kindMismatch(n Node, kind string) error {
	return creativeops.Explain(creativeops.ErrValidation, "%s 内容不能放进 %s 节点 %s", kind, strings.TrimPrefix(n.TypeKey, "core."), n.ID)
}

// fillable is a node that can take its first content.
func fillable(n Node) error {
	switch {
	case n.TypeKey == "core.group":
		return creativeops.Explain(creativeops.ErrValidation, "%s 是分组，不能放内容", n.ID)
	case n.ContentRevisionID != nil:
		return creativeops.Explain(creativeops.ErrValidation, "节点 %s 已有内容，只能往空节点里放；要换内容请重新生成", n.ID)
	}
	return nil
}

func (e *editGraph) manualVersion(n *Node, r creativecontent.Revision, origin string) error {
	return manualVersion(e.ctx, e.tx, &e.g, n, r, origin)
}

// manualVersion registers media a person put into a node as one of its
// versions, so generating on the node later never drops it from the list.
// Text bodies are edited and saved continuously and are not versioned.
func manualVersion(ctx context.Context, tx store.TxAccountScope, g *graphState, n *Node, r creativecontent.Revision, origin string) error {
	n.SelectedVersionID = nil
	if !creativecontent.IsMediaKind(r.Kind) {
		return nil
	}
	v, err := makeVersion(ctx, tx, n.ID, r.ID, origin, json.RawMessage(`{}`), []VersionInput{})
	if err != nil {
		return err
	}
	g.Versions[v.ID] = v
	n.SelectedVersionID = &v.ID
	return nil
}

// fill puts a library asset into an existing empty node of the same kind;
// nodes with content change through generation and versions instead.
func (e *editGraph) fill(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	if a.Asset == nil {
		return creativeops.Explain(creativeops.ErrValidation, "需要指定要放进节点 %s 的素材", n.ID)
	}
	if err = fillable(n); err != nil {
		return err
	}
	asset, err := creativelibrary.ResolveAssetsInTx(e.ctx, e.tx, *a.Asset)
	if err != nil {
		return err
	}
	r, err := creativecontent.RequireUsable(e.ctx, e.tx, asset.ContentRevisionID, "display")
	if err != nil {
		return err
	}
	if n.TypeKey != "core."+r.Kind {
		return kindMismatch(n, r.Kind)
	}
	n.ContentID = &r.ContentID
	n.ContentRevisionID = &r.ID
	if err = e.manualVersion(&n, r, "asset"); err != nil {
		return err
	}
	if strings.TrimSpace(n.Title) == "" {
		n.Title = asset.Title
	}
	fitMediaSize(&n, r)
	e.g.Nodes[n.ID] = n
	return nil
}

// fillUpload places a staged upload into an empty node of the same kind. It is
// an ordinary user edit: ordered with the rest of the batch and undoable.
func (e *editGraph) fillUpload(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	if a.UploadID == "" {
		return creativeops.Explain(creativeops.ErrValidation, "需要指定要放进节点 %s 的上传文件", n.ID)
	}
	if err = fillable(n); err != nil {
		return err
	}
	for _, p := range e.staged {
		if p.staged.UploadID == a.UploadID || p.nodeID == n.ID {
			return creativeops.Explain(creativeops.ErrValidation, "一批动作里，同一个上传文件或同一个节点只能用一次")
		}
	}
	s, err := creativestaging.Lock(e.ctx, e.tx, a.UploadID)
	if errors.Is(err, creativestaging.ErrUnavailable) {
		return ErrVersionConflict
	}
	if err != nil {
		return err
	}
	r, err := creativecontent.RequireUsable(e.ctx, e.tx, s.ContentRevisionID, "display")
	if err != nil {
		return err
	}
	if n.TypeKey != "core."+r.Kind || s.Kind != r.Kind {
		return kindMismatch(n, r.Kind)
	}
	n.ContentID = &r.ContentID
	n.ContentRevisionID = &r.ID
	if err = e.manualVersion(&n, r, "upload"); err != nil {
		return err
	}
	if strings.TrimSpace(n.Title) == "" {
		n.Title = creativestaging.Title(s.FileName)
	}
	fitMediaSize(&n, r)
	e.g.Nodes[n.ID] = n
	e.staged = append(e.staged, placedUpload{staged: s, nodeID: n.ID})
	return nil
}
func (e *editGraph) replace(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	if n.TypeKey == "core.group" {
		return creativeops.Explain(creativeops.ErrValidation, "%s 是分组，没有内容可改", n.ID)
	}
	if a.Payload == nil {
		return creativeops.Explain(creativeops.ErrValidation, "缺少节点 %s 的新内容", n.ID)
	}
	d := creativecontent.Draft{Kind: strings.TrimPrefix(n.TypeKey, "core."), Payload: *a.Payload}
	source := ""
	if n.ContentRevisionID == nil {
		if a.Rights != nil {
			d.Rights = *a.Rights
		}
	} else {
		if a.Rights != nil {
			return creativeops.Explain(creativeops.ErrValidation, "节点 %s 已有内容，修改时不能再改授权信息", n.ID)
		}
		source = *n.ContentRevisionID
	}
	r, err := creativecontent.WriteAndRetain(e.ctx, e.tx, d, source, &n.ID, func(r creativecontent.Revision) error { return retainChange(e.ctx, e.tx, e.changeID, r.ID) })
	if err != nil {
		return err
	}
	n.ContentID = &r.ContentID
	n.ContentRevisionID = &r.ID
	n.SelectedVersionID = nil
	e.g.Nodes[n.ID] = n
	return nil
}
func (e *editGraph) connect(a Action) error {
	source, err := e.node(a.SourceNodeID, "data")
	if err != nil {
		return err
	}
	target, err := e.node(a.TargetNodeID, "data")
	if err != nil {
		return err
	}
	switch {
	case !hasPorts(source.TypeKey):
		return noPorts(source)
	case !hasPorts(target.TypeKey):
		return noPorts(target)
	case a.SourcePort != "output" || a.TargetPort != "reference" || a.Role != "reference":
		return creativeops.Explain(creativeops.ErrValidation, "连线只能从 output 端口连到 reference 端口，角色为 reference")
	case source.ID == target.ID:
		return creativeops.Explain(creativeops.ErrValidation, "节点 %s 不能连到自己", source.ID)
	}
	for _, edge := range e.g.Edges {
		if edge.SourceNodeID == source.ID && edge.TargetNodeID == target.ID {
			return creativeops.Explain(creativeops.ErrValidation, "%s 到 %s 已经有连线", source.ID, target.ID)
		}
	}
	id := "cwedge_" + uuid.NewString()
	ordinal := 0
	for _, edge := range e.g.Edges {
		if edge.TargetNodeID == target.ID && edge.Ordinal >= ordinal {
			ordinal = edge.Ordinal + 1
		}
	}
	e.g.Edges[id] = Edge{ID: id, SourceNodeID: source.ID, TargetNodeID: target.ID, SourcePort: a.SourcePort, TargetPort: a.TargetPort, Role: a.Role, Ordinal: ordinal, Revision: 1}
	return nil
}
func (e *editGraph) setInputs(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	if !hasPorts(n.TypeKey) {
		return noPorts(n)
	}
	if len(a.Inputs) > 50 {
		return creativeops.Explain(creativeops.ErrValidation, "一个节点最多 50 个输入")
	}
	for id, i := range e.g.Inputs {
		if i.NodeID == n.ID && i.DraftID == nil {
			e.need(id, "data")
			delete(e.g.Inputs, id)
		}
	}
	slots := map[string]bool{}
	for _, source := range a.Inputs {
		i := inputFromSource(source)
		if i.DraftID != nil {
			return creativeops.Explain(creativeops.ErrValidation, "提示词草稿的参考要通过保存提示词修改")
		}
		if err := validReference(i, slots); err != nil {
			return err
		}
		if i.SourceNodeID != nil {
			source, err := e.node(*i.SourceNodeID, "data")
			if err != nil {
				return err
			}
			if !hasPorts(source.TypeKey) {
				return noPorts(source)
			}
		} else {
			if _, err := creativecontent.RequireUsable(e.ctx, e.tx, *i.ContentRevisionID, "display"); err != nil {
				return err
			}
		}
		i.ID = "cwinp_" + uuid.NewString()
		i.NodeID = n.ID
		i.Revision = 1
		e.g.Inputs[i.ID] = i
	}
	return nil
}

func noPorts(n Node) error {
	return creativeops.Explain(creativeops.ErrValidation, "%s 节点 %s 没有端口，不能连线或作为参考", strings.TrimPrefix(n.TypeKey, "core."), n.ID)
}

// validReference checks one reference slot and claims its position in slots.
func validReference(i NodeInput, slots map[string]bool) error {
	switch {
	case i.Slot != "reference" || i.Role != "reference":
		return creativeops.Explain(creativeops.ErrValidation, "参考的 slot 和 role 都须是 reference")
	case i.Ordinal < 0:
		return creativeops.Explain(creativeops.ErrValidation, "参考的序号不能为负")
	case (i.SourceNodeID == nil) == (i.ContentRevisionID == nil):
		return creativeops.Explain(creativeops.ErrValidation, "每个参考须且只能指定来源节点或内容版本之一")
	}
	key := string(creativegraph.Canonical([]any{i.Slot, i.Ordinal}))
	if slots[key] {
		return creativeops.Explain(creativeops.ErrValidation, "参考序号 %d 重复了", i.Ordinal)
	}
	slots[key] = true
	return nil
}
func (e *editGraph) group(a Action) error {
	roots, err := e.roots(a.NodeIDs)
	if err != nil {
		return err
	}
	positions := WorldPositions(nodeSlice(e.g.Nodes))
	var parent *string
	// Common ancestor keeps nested groups within their current layout owner.
	chain := []string{}
	for p := e.g.Nodes[roots[0]].ParentID; p != nil; p = e.g.Nodes[*p].ParentID {
		chain = append(chain, *p)
	}
	for _, candidate := range chain {
		all := true
		for _, id := range roots {
			found := false
			for p := e.g.Nodes[id].ParentID; p != nil; p = e.g.Nodes[*p].ParentID {
				if *p == candidate {
					found = true
					break
				}
			}
			all = all && found
		}
		if all {
			v := candidate
			parent = &v
			break
		}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, id := range roots {
		n := e.g.Nodes[id]
		p := positions[id]
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X+n.Width)
		maxY = math.Max(maxY, p.Y+n.Height)
	}
	origin := Point{X: minX - 24, Y: minY - 52}
	relative := origin
	if parent != nil {
		relative.X -= positions[*parent].X
		relative.Y -= positions[*parent].Y
	}
	id := "cwnode_" + uuid.NewString()
	if err = e.add(Action{Type: "add_node", NodeID: id, TypeKey: "core.group", Title: "未命名分组", X: relative.X, Y: relative.Y, ParentID: parent}); err != nil {
		return err
	}
	g := e.g.Nodes[id]
	g.Width = maxX - minX + 48
	g.Height = maxY - minY + 76
	e.g.Nodes[id] = g
	for _, child := range roots {
		n := e.g.Nodes[child]
		n.ParentID = &id
		n.X = positions[child].X - origin.X
		n.Y = positions[child].Y - origin.Y
		e.g.Nodes[child] = n
	}
	return nil
}
func (e *editGraph) ungroup(id string) error {
	g, err := e.node(id, "placement")
	if err != nil {
		return err
	}
	if g.TypeKey != "core.group" {
		return creativeops.Explain(creativeops.ErrValidation, "%s 不是分组，不能取消分组", g.ID)
	}
	for child, n := range e.g.Nodes {
		if n.ParentID != nil && *n.ParentID == id {
			e.need(child, "placement")
			n.ParentID = g.ParentID
			n.X += g.X
			n.Y += g.Y
			e.g.Nodes[child] = n
		}
	}
	delete(e.g.Nodes, id)
	return e.ancestors(g)
}
func (e *editGraph) reparent(a Action) error {
	roots, err := e.roots(a.NodeIDs)
	if err != nil {
		return err
	}
	pos := WorldPositions(nodeSlice(e.g.Nodes))
	origin := Point{}
	if a.ParentID != nil {
		p, err := e.node(*a.ParentID, "placement")
		if err != nil {
			return err
		}
		if p.TypeKey != "core.group" {
			return notGroup(p)
		}
		origin = pos[p.ID]
		if err = e.ancestors(p); err != nil {
			return err
		}
	}
	for _, id := range roots {
		n := e.g.Nodes[id]
		n.ParentID = a.ParentID
		n.X = pos[id].X - origin.X
		n.Y = pos[id].Y - origin.Y
		e.g.Nodes[id] = n
	}
	return validateGraph(e.g)
}
func (e *editGraph) closure(roots []string) map[string]bool {
	closed := map[string]bool{}
	for _, id := range roots {
		closed[id] = true
	}
	for changed := true; changed; {
		changed = false
		for id, n := range e.g.Nodes {
			if !closed[id] && n.ParentID != nil && closed[*n.ParentID] {
				closed[id] = true
				changed = true
			}
		}
	}
	return closed
}
func (e *editGraph) duplicate(a Action) error {
	roots, err := e.roots(a.NodeIDs)
	if err != nil {
		return err
	}
	closure := e.closure(roots)
	rootSet := map[string]bool{}
	for _, id := range roots {
		rootSet[id] = true
	}
	copies := map[string]Node{}
	for id := range closure {
		n, err := e.node(id, "data")
		if err != nil {
			return err
		}
		e.need(id, "placement")
		e.mapping[id] = "cwnode_" + uuid.NewString()
		copies[id] = n
	}
	for id, n := range copies {
		n.ID = e.mapping[id]
		if n.ParentID != nil && closure[*n.ParentID] {
			mapped := e.mapping[*n.ParentID]
			n.ParentID = &mapped
		}
		if rootSet[id] {
			n.X += a.DX
			n.Y += a.DY
			n.Title = copyTitle(n.Title)
		}
		n.ActiveExecutionID = nil
		n.LatestExecutionID = nil
		n.SelectedVersionID = nil
		n.StatusRevision = 1
		n.DataRevision = 1
		n.PlacementRevision = 1
		if n.ContentRevisionID != nil {
			if _, err := creativecontent.RequireUsable(e.ctx, e.tx, *n.ContentRevisionID, "display"); err != nil {
				return err
			}
		}
		if n.ContentRevisionID != nil {
			v, err := makeVersion(e.ctx, e.tx, n.ID, *n.ContentRevisionID, "copy", json.RawMessage(`{}`), []VersionInput{})
			if err != nil {
				return err
			}
			if source := copies[id]; source.SelectedVersionID != nil {
				sv, ok := e.g.Versions[*source.SelectedVersionID]
				if !ok {
					return ErrGraphIntegrity
				}
				e.need(sv.ID, "data")
				v.Provenance = sv.Provenance
				v.Inputs = sv.Inputs
			}
			e.g.Versions[v.ID] = v
			n.SelectedVersionID = &v.ID
		}
		e.g.Nodes[n.ID] = n
	}
	oldEdges := make([]Edge, 0, len(e.g.Edges))
	for _, edge := range e.g.Edges {
		oldEdges = append(oldEdges, edge)
	}
	for _, edge := range oldEdges {
		if closure[edge.SourceNodeID] && closure[edge.TargetNodeID] {
			e.need(edge.ID, "data")
			edge.ID = "cwedge_" + uuid.NewString()
			edge.SourceNodeID = e.mapping[edge.SourceNodeID]
			edge.TargetNodeID = e.mapping[edge.TargetNodeID]
			edge.Revision = 1
			e.g.Edges[edge.ID] = edge
		} else if closure[edge.TargetNodeID] {
			e.omitted = append(e.omitted, edge.ID)
		}
	}
	oldInputs := make([]NodeInput, 0, len(e.g.Inputs))
	for _, i := range e.g.Inputs {
		oldInputs = append(oldInputs, i)
	}
	for _, i := range oldInputs {
		if i.DraftID != nil {
			continue
		}
		if !closure[i.NodeID] {
			continue
		}
		e.need(i.ID, "data")
		if i.SourceNodeID != nil && !closure[*i.SourceNodeID] {
			e.omitted = append(e.omitted, i.ID)
			continue
		}
		if i.ContentRevisionID != nil {
			if _, err := creativecontent.RequireUsable(e.ctx, e.tx, *i.ContentRevisionID, "display"); err != nil {
				return err
			}
		}
		i.ID = "cwinp_" + uuid.NewString()
		i.NodeID = e.mapping[i.NodeID]
		if i.SourceNodeID != nil {
			mapped := e.mapping[*i.SourceNodeID]
			i.SourceNodeID = &mapped
		}
		i.Revision = 1
		e.g.Inputs[i.ID] = i
	}
	sort.Strings(e.omitted)
	return nil
}
func (e *editGraph) remove(a Action) error {
	roots, err := e.roots(a.NodeIDs)
	if err != nil {
		return err
	}
	for _, id := range roots {
		if e.g.Nodes[id].TypeKey == "core.group" && a.GroupMode != "subtree" {
			return creativeops.Explain(creativeops.ErrValidation, "删除分组 %s 要连同其中的节点一起删除（group_mode 为 subtree）", id)
		}
	}
	closure := e.closure(roots)
	for id, v := range e.g.Versions {
		if closure[v.NodeID] {
			e.need(id, "data")
			delete(e.g.Versions, id)
		}
	}
	for id := range closure {
		if _, err := e.node(id, "data"); err != nil {
			return err
		}
		e.need(id, "placement")
		delete(e.g.Nodes, id)
	}
	for id, edge := range e.g.Edges {
		if closure[edge.SourceNodeID] || closure[edge.TargetNodeID] {
			e.need(id, "data")
			delete(e.g.Edges, id)
		}
	}
	for id, i := range e.g.Inputs {
		if closure[i.NodeID] || i.SourceNodeID != nil && closure[*i.SourceNodeID] {
			e.need(id, "data")
			delete(e.g.Inputs, id)
		}
	}
	return nil
}
func validateGraph(g graphState) error {
	if len(g.Nodes) > maxNodes || len(g.Edges) > 2000 || len(g.Inputs) > 2000 {
		return creativeops.Explain(ErrLimit, "画布最多 %d 个节点、2000 条连线和 2000 个输入引用", maxNodes)
	}
	for _, n := range g.Nodes {
		if !validPoint(n.X, n.Y) || !validPoint(n.Width, n.Height) || n.Width <= 0 || n.Height <= 0 || n.Width > 100000 || n.Height > 100000 {
			return creativeops.Explain(creativeops.ErrValidation, "节点 %s 的位置或尺寸超出范围：坐标绝对值不超过 10000000，宽高须大于 0 且不超过 100000", n.ID)
		}
		seen := map[string]bool{n.ID: true}
		for p := n.ParentID; p != nil; {
			parent, ok := g.Nodes[*p]
			if !ok || parent.TypeKey != "core.group" || seen[*p] {
				return creativeops.Explain(creativeops.ErrValidation, "节点 %s 的上级不是有效的分组", n.ID)
			}
			seen[*p] = true
			p = parent.ParentID
		}
	}
	for _, n := range g.Nodes {
		if n.SelectedVersionID != nil {
			v, ok := g.Versions[*n.SelectedVersionID]
			if !ok || v.NodeID != n.ID || n.ContentRevisionID == nil || v.ContentRevisionID != *n.ContentRevisionID {
				return creativeops.Explain(creativeops.ErrValidation, "节点 %s 选中的版本与它当前的内容对不上", n.ID)
			}
		}
	}
	for _, v := range g.Versions {
		if _, ok := g.Nodes[v.NodeID]; !ok {
			return creativeops.Explain(creativeops.ErrValidation, "版本 %s 所属的节点 %s 已不在画布上", v.ID, v.NodeID)
		}
	}
	graph := map[string][]string{}
	for _, edge := range g.Edges {
		graph[edge.SourceNodeID] = append(graph[edge.SourceNodeID], edge.TargetNodeID)
	}
	for _, i := range g.Inputs {
		if i.SourceNodeID != nil {
			graph[*i.SourceNodeID] = append(graph[*i.SourceNodeID], i.NodeID)
		}
	}
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if color[id] == 1 {
			return false
		}
		if color[id] == 2 {
			return true
		}
		if _, ok := g.Nodes[id]; !ok {
			return false
		}
		color[id] = 1
		for _, next := range graph[id] {
			if !visit(next) {
				return false
			}
		}
		color[id] = 2
		return true
	}
	for id := range graph {
		if !visit(id) {
			return creativeops.Explain(creativeops.ErrValidation, "连线不能形成环，从节点 %s 出发的连线会绕回起点或指向不存在的节点", id)
		}
	}
	return nil
}
func (e *editGraph) expandGroups() error {
	// Bottom-up expansion compensates every direct child, keeping world positions.
	nodes := nodeSlice(e.g.Nodes)
	depth := func(n Node) int {
		d := 0
		seen := map[string]bool{}
		for p := n.ParentID; p != nil; {
			if seen[*p] {
				return maxNodes
			}
			seen[*p] = true
			parent, ok := e.g.Nodes[*p]
			if !ok {
				return maxNodes
			}
			d++
			p = parent.ParentID
		}
		return d
	}
	sort.Slice(nodes, func(i, j int) bool { return depth(nodes[i]) > depth(nodes[j]) })
	for _, old := range nodes {
		g := e.g.Nodes[old.ID]
		if g.TypeKey != "core.group" {
			continue
		}
		minX, minY, maxX, maxY := 0.0, 0.0, g.Width, g.Height
		for _, n := range e.g.Nodes {
			if n.ParentID != nil && *n.ParentID == g.ID {
				minX = math.Min(minX, n.X-24)
				minY = math.Min(minY, n.Y-52)
				maxX = math.Max(maxX, n.X+n.Width+24)
				maxY = math.Max(maxY, n.Y+n.Height+24)
			}
		}
		if minX == 0 && minY == 0 && maxX == g.Width && maxY == g.Height {
			continue
		}
		e.need(g.ID, "placement")
		g.X += minX
		g.Y += minY
		g.Width = maxX - minX
		g.Height = maxY - minY
		e.g.Nodes[g.ID] = g
		for id, n := range e.g.Nodes {
			if n.ParentID != nil && *n.ParentID == g.ID {
				e.need(id, "placement")
				n.X -= minX
				n.Y -= minY
				e.g.Nodes[id] = n
			}
		}
	}
	return nil
}
