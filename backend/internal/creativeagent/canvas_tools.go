package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The assistant edits the canvas with the same commands a photographer's
// hands send. Every change lands in the canvas history, so the photographer
// can undo it there. Until permission modes exist, writes need no approval.
const (
	editCanvasTool    = "edit_canvas"
	setNodePromptTool = "set_node_prompt"
	generateNodeTool  = "generate_node"
)

// generationWait is how long one generate_node call waits for its result. A
// generation that outlasts it keeps going on the canvas.
const generationWait = 150 * time.Second

// conflictRetries re-reads the canvas when someone else moved it between the
// assistant's read and its write. The intent is absolute, so it is reapplied.
const conflictRetries = 3

// Generator is the node generation entry the assistant shares with the canvas.
// Admission, the hold, settlement and version keeping are the same path a
// photographer's own click takes.
type Generator interface {
	Models() []creativegeneration.Model
	DraftParameters(modelKey, nodeType string, given map[string]any) (json.RawMessage, error)
	GenerateNode(context.Context, store.AccountScope, creativegeneration.AgentGeneration) (creativegeneration.AgentGenerationResult, error)
	// StopAgentGeneration ends the generation a call with this seed submitted,
	// if the gateway has not sent it yet. Sent ones cannot be stopped.
	StopAgentGeneration(ctx context.Context, scope store.AccountScope, canvasID, seed string) (bool, error)
}

// SetGenerator binds generation at composition time. Without it, or while no
// model is configured, the prompt and generate tools are not offered.
func (s *Service) SetGenerator(g Generator) {
	s.generator = g
}

func (s *Service) generationModels() []creativegeneration.Model {
	if s.generator == nil {
		return nil
	}
	return s.generator.Models()
}

// toolRefusal is a business "no" the model should read and relay. It fails the
// step without failing the run.
type toolRefusal struct{ message string }

func (r *toolRefusal) Error() string         { return r.message }
func (r *toolRefusal) PublicMessage() string { return r.message }

func refuseTool(format string, args ...any) error {
	return &toolRefusal{message: fmt.Sprintf(format, args...)}
}

// errToolArguments is a call whose arguments do not fit the tool's schema.
var errToolArguments = fmt.Errorf("%w: tool arguments do not match the schema", creativeops.ErrValidation)

// asRefusal turns a failure the model can act on into words. Canvas commands
// run in one transaction, so a refused command changed nothing.
func asRefusal(err error) error {
	var public creativeops.Public
	switch {
	case errors.As(err, &public):
		// Explained where it happened; that is better than any guess from here.
		return err
	case errors.Is(err, creativelibrary.ErrCursorStale):
		return refuseTool("cursor_stale：资产库已变化，请保持筛选重新从第一页读取")
	case errors.Is(err, creativelibrary.ErrReadBudget):
		return refuseTool("完整单项超过读取预算，请缩小查询或调整读取预算")
	case errors.Is(err, creativelibrary.ErrGroupNameTaken):
		return refuseTool("同一层已有同名分组")
	case errors.Is(err, creativelibrary.ErrGroupDepth):
		return refuseTool("分组最多两层")
	case errors.Is(err, creativelibrary.ErrTrashed):
		return refuseTool("素材在回收站里")
	case errors.Is(err, errToolArguments):
		return refuseTool("参数不符合工具说明：须是一个 JSON 对象，且只能使用说明里列出的字段")
	case errors.Is(err, creativelibrary.ErrNotFound), errors.Is(err, creativecanvas.ErrNotFound):
		return refuseTool("素材、节点或归类已不存在，请重新查找")
	case errors.Is(err, creativelibrary.ErrVersionConflict):
		return refuseTool("素材已更新，重新查找")
	case errors.Is(err, creativecontent.ErrUsageDenied), errors.Is(err, creativecontent.ErrMissingRoot), errors.Is(err, creativecontent.ErrNotFound):
		return refuseTool("内容已删除、失去引用或不可展示，请重新选择")
	case errors.Is(err, creativecanvas.ErrGraphIntegrity):
		return refuseTool("画布数据校验未通过，本次修改没有生效。这不是参数问题，重试也不会成功；请如实告诉创作者，并请其联系我们处理")
	}
	var generation *creativegeneration.Refusal
	if errors.As(creativegeneration.AgentRefusal(err), &generation) {
		return &toolRefusal{message: generation.Message}
	}
	return err
}

type canvasTool struct {
	handler *runtimeHandler
	name    string
}

var _ tool.InvokableTool = (*canvasTool)(nil)

var nodeTypes = []string{"text", "image", "video", "audio", "link"}

func (t *canvasTool) Info(context.Context) (*schema.ToolInfo, error) {
	str := func(desc string) *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.String, Desc: desc} }
	num := func(desc string) *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.Number, Desc: desc} }
	switch t.name {
	case saveLibraryTool:
		return assetToolInfo(saveLibraryTool), nil
	case editCanvasTool:
		action := map[string]*schema.ParameterInfo{
			"type":                {Type: schema.String, Required: true, Desc: "动作类型", Enum: []string{"add_node", "fill_node", "update_node", "move_node", "resize_node", "set_text", "connect", "disconnect", "remove_nodes"}},
			"asset_id":            str("add_node / fill_node：资产库素材 ID，必须同时传搜索结果的 content_revision_id"),
			"content_revision_id": str("add_node：只有此字段时使用本对话或画布内容；fill_node 必须与 asset_id 一起提供"),
			"key":                 str("add_node 必填：本次调用内给新节点起的临时名，后续动作可用它代替节点 ID，结果里返回真实 ID"),
			"node_type":           {Type: schema.String, Desc: "add_node 必填：节点类型", Enum: nodeTypes},
			"node_id":             str("update_node / move_node / resize_node / set_text 的目标节点 ID 或本次新建节点的临时名"),
			"node_ids":            {Type: schema.Array, Desc: "remove_nodes 要删除的节点 ID", ElemInfo: &schema.ParameterInfo{Type: schema.String}},
			"title":               str("add_node / update_node：节点标题"),
			"text":                str("add_node（文字节点）/ set_text：文字节点的完整正文"),
			"x":                   num("add_node / move_node：左上角横坐标；新建时省略则自动放在画布右侧"),
			"y":                   num("add_node / move_node：左上角纵坐标"),
			"width":               num("resize_node：宽度，160 到 1200"),
			"height":              num("resize_node：高度，不小于 100；有内容的图片和视频按比例自动计算"),
			"source_node_id":      str("connect / disconnect：参考来源节点"),
			"target_node_id":      str("connect / disconnect：使用参考的节点"),
		}
		return &schema.ToolInfo{
			Name: editCanvasTool,
			Desc: "按顺序执行一组画布修改，全部成功或全部不生效：新建节点、改标题、移动、缩放、改写文字节点正文、连接或断开参考、删除节点。连接参考表示 target 生成时把 source 的内容作为参考。修改会进入画布历史，创作者可以撤销。改动前先用 read_canvas 了解现状。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"actions": {Type: schema.Array, Required: true, Desc: "1 到 100 个动作", ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: action}},
			}),
		}, nil
	case setNodePromptTool:
		models := t.handler.service.generationModels()
		keys := make([]string, 0, len(models))
		var catalog strings.Builder
		for _, m := range models {
			keys = append(keys, m.Key)
			fmt.Fprintf(&catalog, "\n- %s（%s，生成%s）：", m.Key, m.Label, m.Kind)
			for i, p := range m.Parameters {
				if i > 0 {
					catalog.WriteString("；")
				}
				fmt.Fprintf(&catalog, "%s（%s）可取 %s，默认 %s", p.Key, p.Label, strings.Join(p.Options, "/"), p.Default)
			}
		}
		return &schema.ToolInfo{
			Name: setNodePromptTool,
			Desc: "写入节点的生成设置：提示词、模型和参数，不会生成。结果里的 draft_revision 交给 generate_node。参考只来自连到该节点的上游节点（用 edit_canvas 连接），不包括节点自己当前的内容；提示词里写 @[节点ID] 可点名某个参考。可用模型与参数：" + catalog.String(),
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"node_id":    {Type: schema.String, Required: true, Desc: "目标节点 ID，节点类型要与模型生成的类型一致"},
				"prompt":     {Type: schema.String, Required: true, Desc: "完整的提示词"},
				"model":      {Type: schema.String, Required: true, Desc: "模型标识", Enum: keys},
				"parameters": {Type: schema.Object, Required: true, Desc: "该模型的参数，键和取值见工具说明；不指定的参数省略，全部用默认时给空对象"},
			}),
		}, nil
	case compressImageTool:
		return compressImageInfo(), nil
	default:
		return &schema.ToolInfo{
			Name: generateNodeTool,
			Desc: "按节点上已写好的提示词、模型和参数生成，结果作为该节点的新版本并切换显示，原有内容保留在版本列表里。生成不参考节点自己当前的内容：要修改或优化一张已有的图，用 edit_canvas 新建节点并把原节点连为参考，在新节点上生成，不要在原节点上直接生成。会消耗创作者的生成额度，只在对方明确要生成时调用。调用会等待结果：state 说生成本身，apply_state 说结果是否放到了节点上。只有 apply_state 为 applied 才表示节点已切换到新版本，此时返回新版本的内容修订 ID，可用 read_asset 查看；conflicted、discarded、expired 表示生成完成但没有采用（期间节点被改动、清空或删除），如实告诉创作者；state 为 running 或 apply_state 为 pending 表示仍在进行，结果稍后出现在画布上，不要重复调用。多个节点要生成时，在同一次回复里一起调用：互不相连的节点会同时生成，连在一起的按先后顺序生成。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"node_id":        {Type: schema.String, Required: true, Desc: "要生成的节点 ID"},
				"draft_revision": {Type: schema.Integer, Required: true, Desc: "set_node_prompt 返回或 read_canvas 读到的提示词版本；节点被别人改过时拒绝"},
			}),
		}, nil
	}
}

func (t *canvasTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	h := t.handler
	step := toolStepFrom(ctx)
	if step == "" {
		return "", ErrRunState
	}
	var canvasID string
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "creative_agent_conversations", "canvas_id", "id=$2", h.held.ConversationID).Scan(&canvasID)
	})
	if err != nil {
		return "", err
	}
	// The step is this call's identity, so a recovered call finds what it
	// already did instead of doing it, or paying for it, twice.
	seed := h.held.RunID + "/" + step
	var out any
	switch t.name {
	case saveLibraryTool:
		return h.runLibraryWrite(ctx, saveLibraryTool, raw)
	case editCanvasTool:
		out, err = h.editCanvas(ctx, canvasID, seed, raw)
	case setNodePromptTool:
		out, err = h.setNodePrompt(ctx, canvasID, seed, raw)
	case compressImageTool:
		out, err = h.compressImage(ctx, canvasID, seed, raw)
	default:
		out, err = h.generateNode(ctx, canvasID, seed, raw)
	}
	if errors.Is(err, creativecanvas.ErrGraphIntegrity) {
		// The model is told; whoever repairs the canvas has to hear it too.
		h.service.logger.ErrorContext(ctx, "creative canvas failed its integrity check",
			slog.String("run_id", h.held.RunID), slog.String("canvas_id", canvasID), slog.String("tool", t.name))
	}
	if err != nil {
		return "", asRefusal(err)
	}
	result, err := json.Marshal(out)
	return string(result), err
}

// applyOnce runs one canvas command under an identity derived from the call.
// A receipt means a previous attempt committed; a conflict means the canvas
// moved, so the command is rebuilt from a fresh read.
func (h *runtimeHandler) applyOnce(ctx context.Context, canvasID, operation string, build func(creativecanvas.Canvas) (any, error), run func(context.Context, store.AccountScope, creativeops.Command) (creativeops.Receipt, error)) (creativecanvas.Canvas, bool, error) {
	var done bool
	if err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		done, err = tx.Exists(ctx, "creative_operation_receipts", "operation_id=$2", operation)
		return err
	}); err != nil {
		return creativecanvas.Canvas{}, false, err
	}
	for attempt := 0; ; attempt++ {
		canvas, err := creativecanvas.GetCanvas(ctx, h.scope, canvasID)
		if err != nil || done {
			return canvas, done, err
		}
		if canvas.Archived {
			return canvas, false, refuseTool("项目已归档，恢复后才能修改")
		}
		payload, err := build(canvas)
		if err != nil {
			return canvas, false, err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return canvas, false, err
		}
		_, err = run(ctx, h.scope, creativeops.Command{OperationID: operation, CreatedAt: time.Now().UTC(), Payload: raw})
		if errors.Is(err, creativecanvas.ErrVersionConflict) && attempt < conflictRetries {
			continue
		}
		if err != nil {
			return canvas, false, err
		}
		canvas, err = creativecanvas.GetCanvas(ctx, h.scope, canvasID)
		return canvas, false, err
	}
}

func toolOperation(seed string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("creative-agent/"+seed)).String()
}

type canvasAction struct {
	AssetID           string   `json:"asset_id"`
	ContentRevisionID string   `json:"content_revision_id"`
	Type              string   `json:"type"`
	Key               string   `json:"key"`
	NodeType          string   `json:"node_type"`
	NodeID            string   `json:"node_id"`
	NodeIDs           []string `json:"node_ids"`
	Title             *string  `json:"title"`
	Text              *string  `json:"text"`
	X                 *float64 `json:"x"`
	Y                 *float64 `json:"y"`
	Width             float64  `json:"width"`
	Height            float64  `json:"height"`
	SourceNodeID      string   `json:"source_node_id"`
	TargetNodeID      string   `json:"target_node_id"`
}

func (h *runtimeHandler) editCanvas(ctx context.Context, canvasID, seed, raw string) (any, error) {
	var args struct {
		Actions []canvasAction `json:"actions"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Actions) == 0 || len(args.Actions) > 100 {
		return nil, refuseTool("一次需要 1 到 100 个动作")
	}
	// New nodes get IDs derived from the call, so a retried call names the
	// same nodes and the model can use its own keys within one call.
	created := map[string]string{}
	for i, a := range args.Actions {
		if a.Type != "add_node" {
			continue
		}
		if a.Key == "" || created[a.Key] != "" {
			return nil, refuseTool("第 %d 个动作：新建节点需要本次调用内唯一的 key", i+1)
		}
		created[a.Key] = "cwnode_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte("creative-agent/"+seed+"/"+a.Key)).String()
	}
	build := func(canvas creativecanvas.Canvas) (any, error) {
		actions, err := canvasActions(canvas, args.Actions, created)
		if err != nil {
			return nil, err
		}
		if err := h.resolveCanvasAssets(ctx, args.Actions, actions); err != nil {
			return nil, err
		}
		return creativecanvas.BatchInput{CanvasID: canvas.ID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas), Actions: actions}, nil
	}
	_, replayed, err := h.applyOnce(ctx, canvasID, toolOperation(seed), build, creativecanvas.ApplyCommands)
	if err != nil {
		return nil, editRefusal(err)
	}
	return map[string]any{"success": true, "created": created, "replayed": replayed}, nil
}

// editRefusal says why the canvas turned a batch down. The batch is one
// command, so none of its actions were applied.
func editRefusal(err error) error {
	var public creativeops.Public
	switch {
	case errors.As(err, &public):
		return refuseTool("这批动作全部未生效：%s", public.PublicMessage())
	case errors.Is(err, creativeops.ErrValidation):
		return refuseTool("画布拒绝了这批动作，全部未生效：缩放时宽度须在 160 到 1200 之间、高度不小于 100，分组不能缩放；坐标绝对值不超过 10000000；连线不能成环。请重新读取画布核对后修正")
	case errors.Is(err, creativecanvas.ErrLimit):
		return refuseTool("超出画布上限，这批动作全部未生效：画布最多 500 个节点、2000 条连线，一次修改最多牵动 100 个对象")
	}
	return err
}

// canvasActions translates the model's vocabulary into canvas commands against
// the current snapshot. Problems are reported in words, by action number.
func canvasActions(canvas creativecanvas.Canvas, in []canvasAction, created map[string]string) ([]creativecanvas.Action, error) {
	nodes := map[string]creativecanvas.Node{}
	right, top := 0.0, 0.0
	for _, n := range canvas.Nodes {
		nodes[n.ID] = n
		if n.ParentID == nil && (len(nodes) == 1 || n.X+n.Width > right) {
			right, top = n.X+n.Width, n.Y
		}
	}
	if len(canvas.Nodes) > 0 {
		right += 80
	}
	resolve := func(i int, id string) (string, error) {
		if real, ok := created[id]; ok {
			return real, nil
		}
		if _, ok := nodes[id]; ok {
			return id, nil
		}
		return "", refuseTool("第 %d 个动作：画布上没有节点 %q", i, id)
	}
	text := func(i int, s *string) (*creativecontent.Payload, error) {
		if s == nil {
			return nil, refuseTool("第 %d 个动作需要 text", i)
		}
		if utf8.RuneCountInString(*s) > 32000 {
			return nil, refuseTool("第 %d 个动作：正文超过 32000 字", i)
		}
		body := *s
		return &creativecontent.Payload{Body: &body}, nil
	}
	var out []creativecanvas.Action
	added := map[string]string{} // nodes this call adds, by type key
	placed := 0
	for index, a := range in {
		i := index + 1
		switch a.Type {
		case "add_node":
			if !slices.Contains(nodeTypes, a.NodeType) {
				return nil, refuseTool("第 %d 个动作：不支持的节点类型 %q", i, a.NodeType)
			}
			action := creativecanvas.Action{Type: "add_node", NodeID: created[a.Key], TypeKey: "core." + a.NodeType, ContentRevisionID: a.ContentRevisionID}
			added[action.NodeID] = action.TypeKey
			if a.Title != nil {
				action.Title = *a.Title
			}
			if a.X != nil && a.Y != nil {
				action.X, action.Y = *a.X, *a.Y
			} else {
				action.X, action.Y = right, top+float64(placed)*360
				placed++
			}
			if a.Text != nil {
				if a.NodeType != "text" {
					return nil, refuseTool("第 %d 个动作：只有文字节点能直接写正文", i)
				}
				payload, err := text(i, a.Text)
				if err != nil {
					return nil, err
				}
				action.Content = &creativecontent.Draft{Kind: "text", Payload: *payload}
			}
			out = append(out, action)
		case "fill_node":
			if a.AssetID == "" || a.ContentRevisionID == "" {
				return nil, refuseTool("填空节点需要 asset_id 和 content_revision_id")
			}
			id, err := resolve(i, a.NodeID)
			if err != nil {
				return nil, err
			}
			out = append(out, creativecanvas.Action{Type: "fill_from_asset", NodeID: id})
		case "update_node":
			id, err := resolve(i, a.NodeID)
			if err != nil {
				return nil, err
			}
			if a.Title == nil {
				return nil, refuseTool("第 %d 个动作需要 title", i)
			}
			out = append(out, creativecanvas.Action{Type: "update_metadata", NodeID: id, Title: *a.Title, Intent: nodes[id].Intent})
		case "move_node":
			id, err := resolve(i, a.NodeID)
			if err != nil {
				return nil, err
			}
			if a.X == nil || a.Y == nil {
				return nil, refuseTool("第 %d 个动作需要 x 和 y", i)
			}
			out = append(out, creativecanvas.Action{Type: "move_node", NodeID: id, X: *a.X, Y: *a.Y})
		case "resize_node":
			id, err := resolve(i, a.NodeID)
			if err != nil {
				return nil, err
			}
			out = append(out, creativecanvas.Action{Type: "resize_node", NodeID: id, Width: a.Width, Height: a.Height})
		case "set_text":
			id, err := resolve(i, a.NodeID)
			if err != nil {
				return nil, err
			}
			kind, ok := added[id]
			if !ok {
				kind = nodes[id].TypeKey
			}
			if kind != "" && kind != "core.text" {
				return nil, refuseTool("第 %d 个动作：节点 %s 不是文字节点", i, id)
			}
			payload, err := text(i, a.Text)
			if err != nil {
				return nil, err
			}
			out = append(out, creativecanvas.Action{Type: "replace_content", NodeID: id, Payload: payload})
		case "connect", "disconnect":
			source, err := resolve(i, a.SourceNodeID)
			if err != nil {
				return nil, err
			}
			target, err := resolve(i, a.TargetNodeID)
			if err != nil {
				return nil, err
			}
			if a.Type == "connect" {
				out = append(out, creativecanvas.Action{Type: "connect_reference", SourceNodeID: source, TargetNodeID: target, SourcePort: "output", TargetPort: "reference", Role: "reference"})
				continue
			}
			edge := slices.IndexFunc(canvas.Edges, func(e creativecanvas.Edge) bool { return e.SourceNodeID == source && e.TargetNodeID == target })
			if edge < 0 {
				return nil, refuseTool("第 %d 个动作：%s 和 %s 之间没有连线", i, source, target)
			}
			out = append(out, creativecanvas.Action{Type: "disconnect_reference", EdgeID: canvas.Edges[edge].ID})
		case "remove_nodes":
			if len(a.NodeIDs) == 0 {
				return nil, refuseTool("第 %d 个动作需要 node_ids", i)
			}
			ids := make([]string, 0, len(a.NodeIDs))
			for _, raw := range a.NodeIDs {
				id, err := resolve(i, raw)
				if err != nil {
					return nil, err
				}
				if nodes[id].TypeKey == "core.group" {
					return nil, refuseTool("第 %d 个动作：暂不支持删除分组 %s", i, id)
				}
				ids = append(ids, id)
			}
			out = append(out, creativecanvas.Action{Type: "remove_nodes", NodeIDs: ids})
		default:
			return nil, refuseTool("第 %d 个动作：不支持的类型 %q", i, a.Type)
		}
	}
	return out, nil
}

func (h *runtimeHandler) setNodePrompt(ctx context.Context, canvasID, seed, raw string) (any, error) {
	var args struct {
		NodeID     string         `json:"node_id"`
		Prompt     string         `json:"prompt"`
		Model      string         `json:"model"`
		Parameters map[string]any `json:"parameters"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Prompt) == "" || utf8.RuneCountInString(args.Prompt) > 32000 {
		return nil, refuseTool("提示词不能为空，也不能超过 32000 字")
	}
	build := func(canvas creativecanvas.Canvas) (any, error) {
		i := slices.IndexFunc(canvas.Nodes, func(n creativecanvas.Node) bool { return n.ID == args.NodeID })
		if i < 0 {
			return nil, refuseTool("画布上没有节点 %s", args.NodeID)
		}
		n := canvas.Nodes[i]
		parameters, err := h.service.generator.DraftParameters(args.Model, n.TypeKey, args.Parameters)
		if err != nil {
			return nil, err
		}
		save := creativecanvas.SavePromptInput{CanvasID: canvas.ID, NodeID: n.ID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas),
			ActionKey: creativecanvas.GenerationAction, Text: args.Prompt, ModelKey: args.Model, Parameters: parameters, References: []creativecanvas.InputSource{}}
		// Attachments the photographer pinned to this draft stay where they are.
		if n.Prompt != nil {
			save.ExpectedRevision = &n.Prompt.Revision
			for _, ref := range n.Prompt.References {
				save.References = append(save.References, creativecanvas.InputSource{Slot: ref.Slot, Ordinal: ref.Ordinal, Role: ref.Role, SourceNodeID: ref.SourceNodeID, ContentRevisionID: ref.ContentRevisionID})
			}
		}
		return save, nil
	}
	canvas, _, err := h.applyOnce(ctx, canvasID, toolOperation(seed), build, creativecanvas.SaveNodePrompt)
	if err != nil {
		return nil, err
	}
	for _, n := range canvas.Nodes {
		if n.ID == args.NodeID && n.Prompt != nil {
			return map[string]any{"success": true, "node_id": n.ID, "draft_revision": int64(n.Prompt.Revision)}, nil
		}
	}
	return nil, refuseTool("节点 %s 已不在画布上", args.NodeID)
}

func (h *runtimeHandler) generateNode(ctx context.Context, canvasID, seed, raw string) (any, error) {
	var args struct {
		NodeID        string `json:"node_id"`
		DraftRevision int64  `json:"draft_revision"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	// The wait never outlives the run: the answer still has to be written.
	wait := min(h.service.generationWait, time.Until(h.held.Deadline)-45*time.Second)
	result, err := h.service.generator.GenerateNode(ctx, h.scope, creativegeneration.AgentGeneration{Seed: seed, CanvasID: canvasID, NodeID: args.NodeID, DraftRevision: args.DraftRevision, Wait: wait,
		// A stop that commits first keeps the generation from being accepted;
		// one that commits after finds it and withdraws it while it is queued.
		Admit: func(ctx context.Context, tx store.TxAccountScope) error {
			return h.service.admitRunInTx(ctx, tx, h.held)
		}})
	if err != nil {
		return nil, err
	}
	return struct {
		Success bool `json:"success"`
		creativegeneration.AgentGenerationResult
	}{result.Adopted(), result}, nil
}
