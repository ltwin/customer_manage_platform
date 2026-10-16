package creativegeneration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// 生图通常1–3分钟完成，中转给出的预计时间最长10分钟。截止含供应商排队与路由
// 切换，超过即判超时（owner 2026-10-16：30分钟太长，10分钟足矣）。
const imageDeadline = 10 * time.Minute

// 内置部署在账号控制面里的有效期；剩余不足四分之一时由下一次生成续期。
const imageDeploymentValidity = 30 * 24 * time.Hour

var imageLabels = map[string]string{
	"gpt-image-2":            "GPT Image 2",
	"gpt-image-2.5-sunburst": "GPT Image 2.5 Sunburst",
	"gpt-image-2.5-flare":    "GPT Image 2.5 Flare",
	"nano-banana-pro":        "Nano Banana Pro",
	"nano-banana-2":          "Nano Banana 2",
	"nano-banana-2.1":        "Nano Banana 2.1",
}

// 共同描述只表达生成用途；各模型的参数与能力由各自策略投影。
const imageDescription = "文生图；连上参考图即按参考生成"

// ImageParameters 是GPT Image面板的表单投影，取值与规则schema一致。
func ImageParameters() []Parameter {
	return []Parameter{
		{Key: "aspect_ratio", Label: "比例", Type: "string", Options: gw.AICodeWithImageRatios, Default: "1:1"},
		{Key: "quality", Label: "画质", Type: "string", Options: []string{"low", "medium", "high"}, Default: "medium"},
		{Key: "resolution", Label: "清晰度", Type: "string", Options: []string{"1K", "2K", "4K"}, Default: "2K"},
		{Key: "background", Label: "背景", Type: "string", Options: []string{"auto", "opaque", "transparent"}, Default: "auto"},
		{Key: "count", Label: "生成数量", Type: "integer", Options: []string{"1", "2", "4"}, Default: "1"},
	}
}

// ImageRegistrations 为内置GPT Image规则构建产品注册。每次生成前先在当前账号
// 控制面登记部署（独立短事务），准入事务里只读取修订号，避免反转网关锁序。
func ImageRegistrations(svc *Service, policies []gw.ImagePolicy) []Registration {
	groups := map[string][]gw.ImagePolicy{}
	var keys []string
	for _, p := range policies {
		model := p.Target.ModelKey
		if len(groups[model]) == 0 {
			keys = append(keys, model)
		}
		groups[model] = append(groups[model], p)
	}
	out := make([]Registration, 0, len(keys))
	for _, key := range keys {
		candidates := groups[key]
		first := candidates[0]
		out = append(out, Registration{
			Model: Model{ConfigurationRevision: first.Definition.Version, Key: key, Label: imageLabels[key], Description: imageDescription, Kind: "image", Parameters: imagePolicyParameters(first)},
			Ensure: func(ctx context.Context, scope store.AccountScope) error {
				for _, p := range candidates {
					if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
						_, err := svc.generation.EnsureDeploymentInTx(ctx, tx, p.Definition, imageDeploymentValidity)
						return err
					}); err != nil {
						return err
					}
				}
				return nil
			},
			PrepareInTx: func(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
				return svc.prepareImageRoute(ctx, tx, in, candidates)
			},
		})
	}
	return out
}

// 参数表单来自部署schema，避免Nano Banana继承GPT专属参数。
func imagePolicyParameters(p gw.ImagePolicy) []Parameter {
	mode := p.Definition.Modes[0]
	var schema struct {
		Properties struct {
			Parameters struct {
				Properties map[string]struct {
					Type string            `json:"type"`
					Enum []json.RawMessage `json:"enum"`
				} `json:"properties"`
			} `json:"parameters"`
		} `json:"properties"`
	}
	// 已编译schema由可信构造器产生；解析失败只返回空目录参数，准入仍会校验。
	if json.Unmarshal(mode.Definition.InputSchema, &schema) != nil {
		return nil
	}
	var out []Parameter
	for _, base := range ImageParameters() {
		field, ok := schema.Properties.Parameters.Properties[base.Key]
		if !ok {
			continue
		}
		base.Options = nil
		for _, v := range field.Enum {
			var text string
			if json.Unmarshal(v, &text) != nil {
				text = string(v)
			}
			base.Options = append(base.Options, text)
		}
		v := mode.Defaults[base.Key]
		if json.Unmarshal(v, &base.Default) != nil {
			base.Default = string(v)
		}
		out = append(out, base)
	}
	return out
}

func (s *Service) prepareImageRoute(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput, policies []gw.ImagePolicy) (gw.GenerationAdmission, int64, error) {
	var selected gw.GenerationAdmission
	var revision int64
	var compatible bool
	var frozenParameters json.RawMessage
	for _, p := range policies {
		admission, rev, err := s.prepareImage(ctx, tx, in, p, frozenParameters)
		if errors.Is(err, gw.ErrCapability) || errors.Is(err, creativeops.ErrValidation) {
			continue
		}
		if err != nil {
			return gw.GenerationAdmission{}, 0, err
		}
		compatible = true
		if frozenParameters == nil {
			frozenParameters = admission.Prepared.Generation().Snapshot().Request.Input.Parameters
		}
		if err = s.generation.CheckDeploymentInTx(ctx, tx, p.Definition, rev); err != nil {
			if errors.Is(err, gw.ErrUnavailable) || errors.Is(err, gw.ErrConflict) {
				continue
			}
			return gw.GenerationAdmission{}, 0, err
		}
		if len(selected.Route) == 0 {
			selected = admission
			revision = rev
		}
		selected.Route = append(selected.Route, gw.GenerationRouteCandidate{Prepared: admission.Prepared, ControlRevision: rev})
	}
	if len(selected.Route) == 0 {
		if compatible {
			return gw.GenerationAdmission{}, 0, gw.ErrUnavailable
		}
		return gw.GenerationAdmission{}, 0, fmt.Errorf("%w: no image deployment supports this input", creativeops.ErrValidation)
	}
	return selected, revision, nil
}

func (s *Service) prepareImage(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput, p gw.ImagePolicy, frozenParameters json.RawMessage) (gw.GenerationAdmission, int64, error) {
	draft, err := creativecanvas.GenerationDraftInTx(ctx, tx, in)
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	if draft.ModelKey != p.Target.ModelKey {
		return gw.GenerationAdmission{}, 0, creativecanvas.ErrVersionConflict
	}
	sources := make([]creativemedia.GenerationSource, 0, len(draft.References))
	for _, ref := range draft.References {
		sources = append(sources, creativemedia.GenerationSource{RevisionID: ref.RevisionID, Role: ref.Role})
	}
	refs, err := s.media.GenerationReferencesInTx(ctx, tx, sources)
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	parameters := draft.Parameters
	if frozenParameters != nil {
		parameters = frozenParameters
	}
	if len(parameters) == 0 || string(parameters) == "null" {
		parameters = json.RawMessage(`{}`)
	}
	request := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: draft.ModelKey, Kind: gw.GenerationImage, Mode: gw.AICodeWithImageMode,
		Input: gw.GenerationInput{Prompt: draft.Prompt, References: refs, Parameters: parameters}}
	facts, err := gw.ReadGenerationFacts(ctx, request, s.media.GenerationReaderInTx(tx))
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	result, err := p.Policy.Prepare(request, facts)
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	switch result.Verdict {
	case gw.GenerationAllow:
	case gw.GenerationDeny:
		return gw.GenerationAdmission{}, 0, creativeops.ErrValidation
	default:
		return gw.GenerationAdmission{}, 0, gw.ErrUnavailable
	}
	revision, err := s.generation.DeploymentRevisionInTx(ctx, tx, p.Definition.DeploymentID)
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return gw.GenerationAdmission{}, 0, err
	}
	deadline := now.Add(imageDeadline)
	// 组与账号额度取0即沿用服务端月度上限；预留按规则给出的上界。
	return gw.GenerationAdmission{Prepared: result.Prepared, Deadline: deadline, GroupDeadline: deadline}, revision, nil
}
