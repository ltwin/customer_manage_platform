package creativegeneration

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Parameter 是产品表单投影；真正的校验、默认值和价格始终由冻结规则负责。
type Parameter struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
	// Default 与Options同为字符串表示；空表示没有默认值。
	Default string `json:"default,omitempty"`
}
type Model struct {
	ConfigurationRevision string      `json:"configuration_revision"`
	Key                   string      `json:"key"`
	Label                 string      `json:"label"`
	Kind                  string      `json:"kind"`
	Parameters            []Parameter `json:"parameters"`
}
type Catalog struct {
	Models         []Model `json:"models"`
	DisabledReason string  `json:"disabled_reason"`
}

// Registration 仅由组合根构建。PrepareInTx只读数据库、编译规则及验证素材授权，
// 不执行网络或反向取得资源锁；返回的期限/预算/价格不得来自客户端。
// 同意与引用在Service.Authorization的最终派发事务中再次复核。
type Registration struct {
	Model       Model
	PrepareInTx func(context.Context, store.TxAccountScope, creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error)
	// Ensure 可选：在受理事务之外为当前账号登记部署，必须幂等。
	Ensure func(context.Context, store.AccountScope) error
}
type Product struct {
	runtime       *Service
	models        []Model
	registrations map[string]Registration
}

// NewProduct 没有注册时提供空目录；不会用内部合成器冒充已配置模型。
func NewProduct(runtime *Service, registrations []Registration) (*Product, error) {
	p := &Product{runtime: runtime, models: []Model{}, registrations: map[string]Registration{}}
	if len(registrations) > 100 {
		return nil, gw.ErrValidation
	}
	for _, r := range registrations {
		m := r.Model
		if runtime == nil || r.PrepareInTx == nil || m.Key == "" || m.Label == "" || m.ConfigurationRevision == "" || len(m.Key) > 64 || (m.Kind != "text" && m.Kind != "image" && m.Kind != "video" && m.Kind != "audio") {
			return nil, gw.ErrValidation
		}
		if _, ok := p.registrations[m.Key]; ok {
			return nil, gw.ErrConflict
		}
		m.Parameters = append([]Parameter{}, m.Parameters...)
		keys := map[string]bool{}
		for i, param := range m.Parameters {
			if param.Key == "" || param.Label == "" || keys[param.Key] || param.Type != "string" && param.Type != "number" && param.Type != "integer" && param.Type != "boolean" || len(param.Options) > 100 {
				return nil, gw.ErrValidation
			}
			keys[param.Key] = true
			m.Parameters[i].Options = append([]string{}, param.Options...)
		}
		r.Model = m
		p.registrations[m.Key] = r
		p.models = append(p.models, m)
	}
	return p, nil
}

func (p *Product) Catalog(ctx context.Context, scope store.AccountScope, canvasID, nodeID string) (Catalog, error) {
	canvas, err := creativecanvas.GetCanvas(ctx, scope, canvasID)
	if err != nil {
		return Catalog{}, err
	}
	var node *creativecanvas.Node
	for i := range canvas.Nodes {
		if canvas.Nodes[i].ID == nodeID {
			node = &canvas.Nodes[i]
			break
		}
	}
	if node == nil {
		return Catalog{}, creativecanvas.ErrNotFound
	}
	out := Catalog{Models: []Model{}}
	if node.TypeVersion != 1 {
		out.DisabledReason = "此节点版本暂不支持生成"
		return out, nil
	}
	if canvas.Archived {
		out.DisabledReason = "项目已归档，恢复后可生成"
		return out, nil
	}
	caps, err := scope.CreativeCapabilities(ctx)
	if err != nil {
		return Catalog{}, err
	}
	if !caps.NodeGenerate || !caps.GenerationApply {
		out.DisabledReason = "账号当前不可用"
		return out, nil
	}
	if p != nil && p.runtime != nil && p.runtime.queue != nil {
		for _, m := range p.models {
			if "core."+m.Kind == node.TypeKey {
				out.Models = append(out.Models, m)
			}
		}
	}
	if len(out.Models) == 0 {
		out.DisabledReason = "此节点类型尚未配置生成模型，仍可保存提示词和查看作品版本"
	}
	// 不允许调用方修改服务内配置。
	raw, err := json.Marshal(out)
	if err != nil {
		return Catalog{}, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func (p *Product) Request(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	if p != nil && p.runtime != nil {
		for _, m := range p.models {
			if ensure := p.registrations[m.Key].Ensure; ensure != nil {
				if err := ensure(ctx, scope); err != nil {
					return creativeops.Receipt{}, err
				}
			}
		}
	}
	// 公共操作只以不可变客户端意图作hash；重放在查目录、预算和当前草稿前返回。
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "canvas.submit_generation", Capability: "node_generate", Validate: func(raw json.RawMessage) error {
		var in creativecanvas.RequestExecutionInput
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.ActionKey != creativecanvas.GenerationAction || in.CanvasID == "" || in.NodeID == "" || in.DraftRevision < 1 || in.ExpectedDataRevision < 1 {
			return creativeops.ErrValidation
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
		if p == nil || p.runtime == nil || p.runtime.queue == nil {
			return creativeops.Outcome{}, creativecanvas.ErrActionUnavailable
		}
		var in creativecanvas.RequestExecutionInput
		if err := creativeops.Decode(raw, &in); err != nil {
			return creativeops.Outcome{}, err
		}
		var key string
		err := tx.QueryRow(ctx, "creative_node_prompt_drafts", "model_key", "canvas_id=$2 AND node_id=$3 AND revision=$4", in.CanvasID, in.NodeID, int64(in.DraftRevision)).Scan(&key)
		if errors.Is(err, store.ErrNoRows) {
			return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
		}
		if err != nil {
			return creativeops.Outcome{}, err
		}
		r, ok := p.registrations[key]
		if !ok {
			return creativeops.Outcome{}, creativecanvas.ErrActionUnavailable
		}
		if r.Model.ConfigurationRevision != in.ConfigurationRevision {
			return creativeops.Outcome{}, gw.ErrConflict
		}
		admission, revision, err := r.PrepareInTx(ctx, tx, in)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		request := admission.Prepared.Generation().Snapshot().Request
		if admission.Prepared.PolicyDefinition().Version != r.Model.ConfigurationRevision || revision < 1 || request.ModelKey != key || string(request.Kind) != r.Model.Kind {
			return creativeops.Outcome{}, gw.ErrConflict
		}
		return p.runtime.requestInTx(ctx, tx, in, c.OperationID, admission, revision)
	}}, c)
}
