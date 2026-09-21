package llmgateway

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"slices"
	"strings"
	"unicode/utf8"
)

// GenerationContractVersion 独立于Chat演进；此契约尚未进入持久请求或生产调用。
const GenerationContractVersion = "generation-v1"

type GenerationKind string

const (
	GenerationText  GenerationKind = "text"
	GenerationImage GenerationKind = "image"
	GenerationVideo GenerationKind = "video"
	GenerationAudio GenerationKind = "audio"
)

// GenerationReference 固定素材身份；Role的含义与组合规则由具体模型模式定义。
// 这些元数据不代替素材读取授权、摘要核验或外发同意。
type GenerationReference struct {
	Role       string         `json:"role"`
	RevisionID string         `json:"revision_id"`
	Digest     string         `json:"digest"`
	Kind       GenerationKind `json:"kind"`
	MIME       string         `json:"mime"`
	ByteSize   int64          `json:"byte_size"`
}

// GenerationInput 的参数由模式schema与校验器解释，不是供应商原始请求透传。
// Prompt是否必填、参考角色/数量、参数默认值均不在公共层决定。
type GenerationInput struct {
	Prompt     string                `json:"prompt,omitempty"`
	References []GenerationReference `json:"references,omitempty"`
	Parameters json.RawMessage       `json:"parameters"`
}

// GenerationRequest 是稳定调用信封；模式为空时仅允许模型得到唯一匹配。
// 账号、预算、外发许可与请求身份始终属于受信控制面。
type GenerationRequest struct {
	Version  string          `json:"version"`
	ModelKey string          `json:"model_key"`
	Kind     GenerationKind  `json:"kind"`
	Mode     string          `json:"mode,omitempty"`
	Input    GenerationInput `json:"input"`
}

// GenerationOutputPolicy 由模型模式根据已规范化输入生成并冻结。
// 公共观察器只执行这份约束，不推断图片数量、视频格式等模型规则。
type GenerationOutputPolicy struct {
	Kind       GenerationKind `json:"kind"`
	MinOutputs int            `json:"min_outputs"`
	MaxOutputs int            `json:"max_outputs"`
	MIMEs      []string       `json:"mimes,omitempty"`
	MaxBytes   int64          `json:"max_bytes"`
}

// GenerationSnapshot 是调用方需要原子保存的内部事实，不是外部提交DTO。
// 同时固定模式、实现版本、schema和输出约束，恢复不再匹配模式或补默认值。
type GenerationSnapshot struct {
	Request        GenerationRequest      `json:"request"`
	ModelRevision  string                 `json:"model_revision"`
	ProfileVersion string                 `json:"profile_version"`
	ModeVersion    string                 `json:"mode_version"`
	InputSchema    json.RawMessage        `json:"input_schema"`
	Output         GenerationOutputPolicy `json:"output"`
}

// PreparedGeneration 只持有已验证、已复制的快照；它仍不是收费派发许可。
type PreparedGeneration struct {
	snapshot    GenerationSnapshot
	fingerprint string
}

func (p PreparedGeneration) Fingerprint() string { return p.fingerprint }
func (p PreparedGeneration) Snapshot() GenerationSnapshot {
	s := p.snapshot
	s.Request = cloneGenerationRequest(s.Request)
	s.InputSchema = slices.Clone(s.InputSchema)
	s.Output.MIMEs = slices.Clone(s.Output.MIMEs)
	return s
}

func cloneGenerationRequest(r GenerationRequest) GenerationRequest {
	r.Input.References = slices.Clone(r.Input.References)
	r.Input.Parameters = slices.Clone(r.Input.Parameters)
	return r
}

// validateGenerationEnvelope 只检查固定信封、JSON完整性和平台解析上限。
// 一份schema/JSON不得借用这些公共检查冒充模型参数许可。
func validateGenerationEnvelope(r GenerationRequest) error {
	if r.Version != GenerationContractVersion || !generationToken(r.ModelKey) || !generationKind(r.Kind) || r.Mode != "" && !generationToken(r.Mode) {
		return fmt.Errorf("%w: generation envelope identity", ErrValidation)
	}
	if !utf8.ValidString(r.Input.Prompt) || len(r.Input.Prompt) > maxTextBytes || len(r.Input.References) > 64 {
		return fmt.Errorf("%w: generation input bounds", ErrValidation)
	}
	for _, ref := range r.Input.References {
		digest, err := hex.DecodeString(ref.Digest)
		if !generationToken(ref.Role) || !generationToken(ref.RevisionID) || err != nil || len(digest) != 32 || strings.ToLower(ref.Digest) != ref.Digest || ref.ByteSize <= 0 || !generationMIME(ref.Kind, ref.MIME) {
			return fmt.Errorf("%w: generation reference identity", ErrValidation)
		}
	}
	if len(r.Input.Parameters) > 64<<10 {
		return fmt.Errorf("%w: generation parameter size", ErrValidation)
	}
	return nil
}

func (p GenerationOutputPolicy) validate() error {
	if !generationKind(p.Kind) || p.MinOutputs < 1 || p.MaxOutputs < p.MinOutputs || p.MaxOutputs > 64 || p.MaxBytes <= 0 || len(p.MIMEs) > 32 {
		return fmt.Errorf("%w: generation output policy bounds", ErrValidation)
	}
	if p.Kind != GenerationText && len(p.MIMEs) == 0 {
		return fmt.Errorf("%w: generation output policy formats", ErrValidation)
	}
	for i, m := range p.MIMEs {
		if !generationMIME(p.Kind, m) || slices.Contains(p.MIMEs[:i], m) {
			return fmt.Errorf("%w: generation output policy format", ErrValidation)
		}
	}
	return nil
}

func generationKind(k GenerationKind) bool {
	return k == GenerationText || k == GenerationImage || k == GenerationVideo || k == GenerationAudio
}

func generationToken(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

// generationMIME 只核对规范媒体类型与模态；具体格式支持由模式schema/输出约束决定。
func generationMIME(kind GenerationKind, value string) bool {
	if !generationKind(kind) || len(value) > 128 {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(value)
	return err == nil && len(params) == 0 && mediaType == value && strings.HasPrefix(value, string(kind)+"/")
}
