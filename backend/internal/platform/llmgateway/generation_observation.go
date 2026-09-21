package llmgateway

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// GenerationObservationState 表示一次供应商回答的含义，不替代持久请求、作品或费用状态。
type GenerationObservationState string

const (
	GenerationAccepted  GenerationObservationState = "accepted"
	GenerationRunning   GenerationObservationState = "running"
	GenerationCompleted GenerationObservationState = "completed"
	// GenerationRejected 表示明确拒绝受理，不是已受理后的任务失败，也不自动授权重试。
	GenerationRejected  GenerationObservationState = "rejected"
	GenerationFailed    GenerationObservationState = "failed"
	GenerationCancelled GenerationObservationState = "cancelled"
	// GenerationUnknown 不能证明是否受理；没有TaskID也不能据此重新提交。
	GenerationUnknown GenerationObservationState = "unknown"
)

// GeneratedMedia 是待受控取件的描述，不是已验证的媒体资产。
// URL语法校验不代替取件白名单、DNS/重定向校验和实际内容校验。
// 保护字段不进入普通JSON投影；后续持久层须显式保存并按账号保护，不能直接序列化本类型恢复。
type GeneratedMedia struct {
	MIME      string    `json:"mime"`
	ByteSize  int64     `json:"byte_size"`
	FetchURL  string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GenerationOutput.ID 必须在同一逻辑请求中稳定，不能使用会轮换的签名URL。
// Index固定作品顺序；一份输出只能包含文本或媒体描述。
type GenerationOutput struct {
	ID    string          `json:"id"`
	Index int             `json:"index"`
	Kind  GenerationKind  `json:"kind"`
	Text  string          `json:"text,omitempty"`
	Media *GeneratedMedia `json:"media,omitempty"`
}

// GenerationObservation 是内部协议值；供应商原始响应/错误不能直接放进这些字段。
// Progress为空表示没有可信百分比。RetryAfter是有界建议，实际唤醒由持久调度器决定。
// 费用证据仍归Gateway账本；completed/failed/cancelled均不能据此推断费用为零。
type GenerationObservation struct {
	State      GenerationObservationState `json:"state"`
	TaskID     string                     `json:"-"`
	Progress   *float64                   `json:"progress,omitempty"`
	RetryAfter time.Duration              `json:"retry_after_ns,omitempty"`
	Outputs    []GenerationOutput         `json:"outputs,omitempty"`
	ErrorCode  string                     `json:"error_code,omitempty"`
}

// Validate 对同步完成和异步观察采用相同结果规则，不执行网络或状态写入。
// 通过校验只证明单次回答结构合法；跨观察的任务归属、顺序、终态及费用证据仍须持久层核验。
func (o GenerationObservation) Validate(p PreparedGeneration) error {
	if p.fingerprint == "" {
		return fmt.Errorf("%w: prepared generation required", ErrValidation)
	}
	if o.TaskID != "" && strings.TrimSpace(o.TaskID) == "" || len(o.TaskID) > 1024 || !utf8.ValidString(o.TaskID) || strings.IndexFunc(o.TaskID, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: generation task identity", ErrProtocol)
	}
	if o.Progress != nil && (math.IsNaN(*o.Progress) || math.IsInf(*o.Progress, 0) || *o.Progress < 0 || *o.Progress > 1) {
		return fmt.Errorf("%w: generation progress", ErrProtocol)
	}
	if o.RetryAfter < 0 || o.RetryAfter > time.Minute {
		return fmt.Errorf("%w: generation retry delay", ErrProtocol)
	}
	if o.ErrorCode != "" && !toolNamePattern(o.ErrorCode) {
		return fmt.Errorf("%w: generation error code", ErrProtocol)
	}
	switch o.State {
	case GenerationAccepted, GenerationRunning:
		if strings.TrimSpace(o.TaskID) == "" || o.ErrorCode != "" {
			return fmt.Errorf("%w: pending generation requires task identity without error", ErrProtocol)
		}
	case GenerationCompleted:
		if o.ErrorCode != "" || o.RetryAfter != 0 || o.Progress != nil && *o.Progress != 1 {
			return fmt.Errorf("%w: completed generation metadata", ErrProtocol)
		}
		return o.validateOutputs(p.snapshot.Output)
	case GenerationRejected, GenerationFailed, GenerationCancelled, GenerationUnknown:
		if o.ErrorCode == "" || o.Progress != nil {
			return fmt.Errorf("%w: generation failure evidence", ErrProtocol)
		}
		if o.State == GenerationRejected && o.TaskID != "" {
			return fmt.Errorf("%w: rejected generation has accepted task", ErrProtocol)
		}
		if o.State != GenerationUnknown && o.RetryAfter != 0 {
			return fmt.Errorf("%w: terminal generation retry delay", ErrProtocol)
		}
	default:
		return fmt.Errorf("%w: generation observation state", ErrProtocol)
	}
	if len(o.Outputs) != 0 {
		return fmt.Errorf("%w: incomplete generation has outputs", ErrProtocol)
	}
	return nil
}

func (o GenerationObservation) validateOutputs(p GenerationOutputPolicy) error {
	if len(o.Outputs) < p.MinOutputs || len(o.Outputs) > p.MaxOutputs {
		return fmt.Errorf("%w: generation output count", ErrProtocol)
	}
	seen := make(map[string]struct{}, len(o.Outputs))
	remaining := p.MaxBytes
	for i, out := range o.Outputs {
		if !generationToken(out.ID) || out.Index != i || out.Kind != p.Kind {
			return fmt.Errorf("%w: generation output identity, order or kind", ErrProtocol)
		}
		if _, exists := seen[out.ID]; exists {
			return fmt.Errorf("%w: duplicate generation output", ErrProtocol)
		}
		seen[out.ID] = struct{}{}
		if p.Kind == GenerationText {
			if out.Media != nil || out.Text == "" || !utf8.ValidString(out.Text) || int64(len(out.Text)) > remaining {
				return fmt.Errorf("%w: generated text", ErrProtocol)
			}
			remaining -= int64(len(out.Text))
			continue
		}
		if out.Text != "" || out.Media == nil {
			return fmt.Errorf("%w: generated media payload", ErrProtocol)
		}
		media := out.Media
		if !slices.Contains(p.MIMEs, media.MIME) || media.ByteSize <= 0 || media.ByteSize > remaining || media.ExpiresAt.IsZero() {
			return fmt.Errorf("%w: generated media metadata", ErrProtocol)
		}
		remaining -= media.ByteSize
		if len(media.FetchURL) == 0 || len(media.FetchURL) > 8192 {
			return fmt.Errorf("%w: generated media location", ErrProtocol)
		}
		location, err := url.Parse(media.FetchURL)
		if err != nil || location.Scheme != "https" || location.Hostname() == "" || location.User != nil || location.Fragment != "" || location.Opaque != "" {
			// 不把URL解析错误原样输出，其中可能包含签名或其他保护字段。
			return fmt.Errorf("%w: generated media location", ErrProtocol)
		}
	}
	return nil
}
