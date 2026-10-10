package creativegeneration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// AgentGeneration 是创作助手的一次生成：按节点上已经写好的提示词、模型和参数生成。
// Seed 是这次工具调用的稳定身份，同一 Seed 重放时不会再提交或再扣费。
type AgentGeneration struct {
	Seed     string
	CanvasID string
	NodeID   string
	// DraftRevision 是助手写入或读到的提示词版本；节点上的提示词已被改动时拒绝，
	// 避免助手花钱生成了别人的版本却以为是自己的。
	DraftRevision int64
	// Wait 是提交后等待结果的最长时间；到时仍未完成则返回进行中。
	Wait time.Duration
}

type AgentGenerationResult struct {
	NodeID      string `json:"node_id"`
	ExecutionID string `json:"execution_id"`
	// State 取 succeeded / failed / running。
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// ContentRevisionIDs 是本次落下的作品版本，可用 read_asset 查看。
	ContentRevisionIDs []string `json:"content_revision_ids,omitempty"`
}

// Refusal 是可以原样告诉模型的拒绝原因，不是系统故障。
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return r.Message }

func refuse(format string, args ...any) error { return &Refusal{Message: fmt.Sprintf(format, args...)} }

// Models 返回已接线的生成模型；生成未启用时为空。
func (p *Product) Models() []Model {
	if p == nil || p.runtime == nil || p.runtime.queue == nil {
		return nil
	}
	return append([]Model(nil), p.models...)
}

func (p *Product) model(key string) (Model, bool) {
	for _, m := range p.Models() {
		if m.Key == key {
			return m, true
		}
	}
	return Model{}, false
}

func agentOperation(seed, step string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("creative-agent/"+seed+"/"+step)).String()
}

// DraftParameters 按模型的参数定义校验并规整取值，未给出的参数沿用规则默认值。
// nodeType 是目标节点类型，模型必须生成这一类内容。
func (p *Product) DraftParameters(modelKey, nodeType string, given map[string]any) (json.RawMessage, error) {
	m, ok := p.model(modelKey)
	if !ok {
		return nil, refuse("没有名为 %s 的生成模型", modelKey)
	}
	if "core."+m.Kind != nodeType {
		return nil, refuse("模型 %s 生成%s，不能用于%s节点", m.Key, m.Kind, strings.TrimPrefix(nodeType, "core."))
	}
	out := map[string]any{}
	for key, value := range given {
		i := slices.IndexFunc(m.Parameters, func(p Parameter) bool { return p.Key == key })
		if i < 0 {
			return nil, refuse("模型 %s 没有参数 %s", m.Key, key)
		}
		param := m.Parameters[i]
		text := fmt.Sprint(value)
		if f, ok := value.(float64); ok {
			text = strconv.FormatFloat(f, 'f', -1, 64)
		}
		if len(param.Options) > 0 && !slices.Contains(param.Options, text) {
			return nil, refuse("参数 %s 只能取 %s", key, strings.Join(param.Options, "、"))
		}
		switch param.Type {
		case "integer":
			n, err := strconv.Atoi(text)
			if err != nil {
				return nil, refuse("参数 %s 需要整数", key)
			}
			out[key] = n
		case "number":
			n, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, refuse("参数 %s 需要数字", key)
			}
			out[key] = n
		case "boolean":
			b, err := strconv.ParseBool(text)
			if err != nil {
				return nil, refuse("参数 %s 需要 true 或 false", key)
			}
			out[key] = b
		default:
			out[key] = text
		}
	}
	return json.Marshal(out)
}

// GenerateNode 走与摄影师点「生成」相同的入口：按节点当前的提示词提交，然后在
// Wait 内等待结果。节点的创建与编辑由画布工具负责，这里只生成。
func (p *Product) GenerateNode(ctx context.Context, scope store.AccountScope, in AgentGeneration) (AgentGenerationResult, error) {
	operation := agentOperation(in.Seed, "generate")
	execution := "cwexec_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(scope.AccountID()+"/"+operation)).String()
	result := AgentGenerationResult{NodeID: in.NodeID, ExecutionID: execution, State: "running"}
	target := creativecanvas.ExecutionTarget{CanvasID: in.CanvasID, ExecutionID: execution}
	// 重放时执行已经存在，只等结果。
	if _, err := creativecanvas.GetNodeExecution(ctx, scope, target); errors.Is(err, creativecanvas.ErrNotFound) {
		if err = p.submitNode(ctx, scope, in, operation); err != nil {
			return AgentGenerationResult{}, AgentRefusal(err)
		}
	} else if err != nil {
		return AgentGenerationResult{}, err
	}
	deadline := time.Now().Add(in.Wait)
	for {
		e, err := creativecanvas.GetNodeExecution(ctx, scope, target)
		if err != nil {
			return result, err
		}
		if e.State == "succeeded" || e.State == "failed" || e.State == "cancelled" {
			result.State, result.Error = e.State, e.Error
			if e.State == "cancelled" {
				result.State = "failed"
			}
			break
		}
		if !time.Now().Before(deadline) {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	versions, err := creativecanvas.ListNodeVersions(ctx, scope, in.CanvasID, in.NodeID)
	if err != nil {
		return result, err
	}
	for _, v := range versions.Items {
		if v.ExecutionID != nil && *v.ExecutionID == execution {
			result.ContentRevisionIDs = append(result.ContentRevisionIDs, v.ContentRevisionID)
		}
	}
	return result, nil
}

func (p *Product) submitNode(ctx context.Context, scope store.AccountScope, in AgentGeneration, operation string) error {
	canvas, err := creativecanvas.GetCanvas(ctx, scope, in.CanvasID)
	if err != nil {
		return err
	}
	if canvas.Archived {
		return refuse("项目已归档，恢复后才能生成")
	}
	var node *creativecanvas.Node
	for i := range canvas.Nodes {
		if canvas.Nodes[i].ID == in.NodeID {
			node = &canvas.Nodes[i]
		}
	}
	switch {
	case node == nil:
		return refuse("画布上没有节点 %s", in.NodeID)
	case node.Prompt == nil || node.Prompt.ActionKey != creativecanvas.GenerationAction || node.Prompt.ModelKey == "":
		return refuse("节点 %s 还没有写提示词和模型，先用 set_node_prompt 写入", in.NodeID)
	case int64(node.Prompt.Revision) != in.DraftRevision:
		return refuse("节点 %s 的提示词已被改动（当前版本 %d），请重新读取后再决定是否生成", in.NodeID, node.Prompt.Revision)
	case node.ActiveExecutionID != nil:
		return refuse("节点 %s 正在生成，完成后再试", in.NodeID)
	}
	m, ok := p.model(node.Prompt.ModelKey)
	if !ok {
		return refuse("节点上的模型 %s 当前不可用", node.Prompt.ModelKey)
	}
	if "core."+m.Kind != node.TypeKey {
		return refuse("节点 %s 的模型不能生成这类内容", in.NodeID)
	}
	request := creativecanvas.RequestExecutionInput{CanvasID: canvas.ID, NodeID: node.ID, ActionKey: creativecanvas.GenerationAction, ExpectedDataRevision: node.DataRevision,
		DraftRevision: node.Prompt.Revision, ReadSet: creativecanvas.ReadSetFor(canvas), ConfigurationRevision: m.ConfigurationRevision}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	_, err = p.Request(ctx, scope, creativeops.Command{OperationID: operation, CreatedAt: time.Now().UTC(), Payload: raw})
	return err
}

// AgentRefusal 把模型可以理解并转述的业务失败翻成拒绝原因；其余错误原样上抛。
func AgentRefusal(err error) error {
	var refusal *Refusal
	switch {
	case errors.As(err, &refusal):
		return err
	case errors.Is(err, creativecanvas.ErrVersionConflict), errors.Is(err, gw.ErrConflict), errors.Is(err, creativeops.ErrConflict):
		return refuse("画布刚被改动，请重新读取画布后再试")
	case errors.Is(err, creativeops.ErrValidation), errors.Is(err, gw.ErrValidation):
		return refuse("提示词、参数或参考不符合该模型的要求（图片参考最多 9 张，仅支持 JPEG/PNG/WebP，单张不超过 10MB）")
	case errors.Is(err, gw.ErrBudget):
		return refuse("本月生成额度不足")
	case errors.Is(err, creativecanvas.ErrActionUnavailable), errors.Is(err, gw.ErrUnavailable):
		return refuse("生成暂时不可用")
	case errors.Is(err, creativecontent.ErrUsageDenied):
		return refuse("参考素材不能用于生成（授权素材或已失效）")
	case errors.Is(err, creativecanvas.ErrArchived):
		return refuse("项目已归档，恢复后才能修改")
	case errors.Is(err, creativecanvas.ErrNotFound):
		return refuse("节点或连线不存在，请重新读取画布")
	case errors.Is(err, creativecanvas.ErrLimit):
		return refuse("画布节点数已达上限")
	}
	return err
}
