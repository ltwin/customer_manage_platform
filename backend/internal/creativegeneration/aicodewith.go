package creativegeneration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// 生图通常1–3分钟完成，中转给出的预计时间最长10分钟；截止留出排队与取件余量。
const imageDeadline = 30 * time.Minute

// 内置部署在账号控制面里的有效期；剩余不足四分之一时由下一次生成续期。
const imageDeploymentValidity = 30 * 24 * time.Hour

var imageLabels = map[string]string{
	"gpt-image-2":            "GPT Image 2",
	"gpt-image-2.5-sunburst": "GPT Image 2.5 Sunburst",
	"gpt-image-2.5-flare":    "GPT Image 2.5 Flare",
}

// 三个模型参数与能力相同；说明只讲摄影师能用它做什么，不写未核实的差异。
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
func ImageRegistrations(svc *Service, policies []gw.AICodeWithImagePolicy) []Registration {
	out := make([]Registration, 0, len(policies))
	for _, p := range policies {
		policy := p
		model := policy.Target.ModelKey
		out = append(out, Registration{
			Model: Model{ConfigurationRevision: policy.Definition.Version, Key: model, Label: imageLabels[model], Description: imageDescription, Kind: "image", Parameters: ImageParameters()},
			Ensure: func(ctx context.Context, scope store.AccountScope) error {
				return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
					_, err := svc.generation.EnsureDeploymentInTx(ctx, tx, policy.Definition, imageDeploymentValidity)
					return err
				})
			},
			PrepareInTx: func(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
				return svc.prepareImage(ctx, tx, in, policy)
			},
		})
	}
	return out
}

func (s *Service) prepareImage(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput, p gw.AICodeWithImagePolicy) (gw.GenerationAdmission, int64, error) {
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
