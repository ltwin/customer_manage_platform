package creativeimageedit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// QualityDefault is the balanced preset, used when a caller names no quality.
const QualityDefault = 80

// agentPoll matches GenerateNode: the tool waits inside one call instead of
// letting the model poll.
const agentPoll = 2 * time.Second

// AgentCompress is one compress_image call. Seed is the call's stable
// identity: a recovered call finds its candidate and its adoption instead of
// encoding or adopting twice.
type AgentCompress struct {
	Seed     string
	CanvasID string
	NodeID   string
	// SourceRevisionID is the image the assistant read. Empty means the image
	// the node shows when the call first runs; a replay never re-reads it.
	SourceRevisionID string
	Quality          int
	// Wait bounds queueing plus encoding. When it runs out before adoption
	// was sent, the candidate is discarded and nothing changes.
	Wait time.Duration
}

// Agent outcomes. Only AgentApplied means the node now shows the result.
const (
	AgentApplied     = "applied"
	AgentNotApplied  = "not_applied"
	AgentNotReduced  = "not_reduced"
	AgentFailed      = "failed"
	AgentUnconfirmed = "unconfirmed"
)

// AgentCompressResult reports facts from the encoded file, never estimates.
type AgentCompressResult struct {
	NodeID           string `json:"node_id"`
	RenderID         string `json:"render_id,omitempty"`
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
	Quality          int    `json:"quality"`
	SourceRevisionID string `json:"source_revision_id"`
	SourceSize       int64  `json:"source_size,omitempty"`
	OutputMIME       string `json:"output_mime,omitempty"`
	OutputSize       int64  `json:"output_size,omitempty"`
	Width            int    `json:"width,omitempty"`
	Height           int    `json:"height,omitempty"`
	// ReductionPercent is (source - output) / source, one decimal.
	ReductionPercent  float64 `json:"reduction_percent,omitempty"`
	VersionID         string  `json:"version_id,omitempty"`
	ContentRevisionID string  `json:"content_revision_id,omitempty"`
}

// Refusal is a reason the assistant can relay as is; nothing was changed.
type Refusal struct{ Message string }

func (r *Refusal) Error() string         { return r.Message }
func (r *Refusal) PublicMessage() string { return r.Message }

func refuse(format string, args ...any) error {
	return &Refusal{Message: fmt.Sprintf(format, args...)}
}

func agentOperation(seed, step string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("creative-agent/"+seed+"/image-edit/"+step)).String()
}

// CompressForAgent runs the photographer's path for the assistant: accept a
// candidate on a fixed source, wait for it within the call, then adopt it
// with the same synchronous command the panel sends. No worker adopts on the
// assistant's behalf, so a call that stops waiting leaves the canvas alone.
func (s *Service) CompressForAgent(ctx context.Context, scope store.AccountScope, in AgentCompress) (AgentCompressResult, error) {
	if in.Quality == 0 {
		in.Quality = QualityDefault
	}
	if in.Quality < qualityMin || in.Quality > qualityMax {
		return AgentCompressResult{}, refuse("质量须是 %d 到 %d 的整数", qualityMin, qualityMax)
	}
	create, apply := agentOperation(in.Seed, "create"), agentOperation(in.Seed, "apply")
	r, err := s.byOperation(ctx, scope, create)
	if errors.Is(err, ErrNotFound) {
		r, err = s.createForAgent(ctx, scope, in, create)
	}
	if err != nil {
		return AgentCompressResult{}, agentRefusal(err)
	}
	result := AgentCompressResult{NodeID: r.NodeID, RenderID: r.ID, Quality: in.Quality, SourceRevisionID: r.SourceRevisionID}
	deadline := time.Now().Add(in.Wait)
	for r.State == "queued" || r.State == "running" {
		if !time.Now().Before(deadline) {
			return s.abandon(ctx, scope, r, result, "压缩未在等待时间内完成，已放弃，图片没有改动")
		}
		select {
		case <-ctx.Done():
			s.discardQuietly(ctx, scope, r.ID)
			return result, ctx.Err()
		case <-time.After(min(agentPoll, max(time.Until(deadline), 0))):
		}
		if r, err = s.Get(ctx, scope, r.ID); err != nil {
			return result, err
		}
	}
	switch r.State {
	case "applied":
		return applied(result, r), nil
	case "not_reduced":
		result.State, result.Reason, result.SourceSize = AgentNotReduced, "按这个质量压缩后文件没有变小，没有创建新版本；可以换更低的质量再试", r.SourceSize
		return result, nil
	case "failed":
		result.State, result.Reason = AgentFailed, failureReason(r.ErrorCode)
		return result, nil
	case "discarded", "expired":
		result.State, result.Reason = AgentNotApplied, "压缩结果已被丢弃或过期，图片没有改动"
		return result, nil
	}
	return s.adoptForAgent(ctx, scope, r, result, apply)
}

func (s *Service) createForAgent(ctx context.Context, scope store.AccountScope, in AgentCompress, operation string) (Render, error) {
	canvas, err := creativecanvas.GetCanvas(ctx, scope, in.CanvasID)
	if err != nil {
		return Render{}, err
	}
	if canvas.Archived {
		return Render{}, refuse("项目已归档，恢复后才能修改")
	}
	var node *creativecanvas.Node
	for i := range canvas.Nodes {
		if canvas.Nodes[i].ID == in.NodeID {
			node = &canvas.Nodes[i]
		}
	}
	switch {
	case node == nil:
		return Render{}, refuse("画布上没有节点 %s", in.NodeID)
	case node.TypeKey != creativecanvas.MediaTypeKey("image"):
		return Render{}, refuse("节点 %s 不是图片节点，只能压缩图片", in.NodeID)
	case node.ContentRevisionID == nil:
		return Render{}, refuse("节点 %s 还没有图片", in.NodeID)
	}
	source := in.SourceRevisionID
	if source == "" {
		source = *node.ContentRevisionID
	}
	return s.Create(ctx, scope, CreateInput{OperationID: operation, CanvasID: in.CanvasID, NodeID: in.NodeID, SourceRevisionID: source, Action: ActionCompress, Quality: in.Quality, CallerKind: "agent"})
}

// adoptForAgent sends adoption once the candidate is ready. Only the node's
// data revision is read again: a title edit must not cost the result, while
// a changed image is still refused by the source check.
func (s *Service) adoptForAgent(ctx context.Context, scope store.AccountScope, r Render, result AgentCompressResult, operation string) (AgentCompressResult, error) {
	result.SourceSize = r.SourceSize
	for attempt := 0; ; attempt++ {
		revision, err := nodeDataRevision(ctx, scope, r.CanvasID, r.NodeID)
		if err != nil {
			return s.abandon(ctx, scope, r, result, adoptionReason(err))
		}
		payload, err := json.Marshal(ApplyInput{CanvasID: r.CanvasID, NodeID: r.NodeID, RenderID: r.ID, ExpectedSourceRevisionID: r.SourceRevisionID, ExpectedDataRevision: revision})
		if err != nil {
			return result, err
		}
		_, err = s.Apply(ctx, scope, creativeops.Command{OperationID: operation, CreatedAt: time.Now().UTC(), Payload: payload})
		if err == nil {
			break
		}
		if errors.Is(err, creativecanvas.ErrVersionConflict) && attempt < 3 {
			continue
		}
		var refusal *Refusal
		if errors.As(agentRefusal(err), &refusal) {
			return s.abandon(ctx, scope, r, result, adoptionReason(err))
		}
		// The command may have committed before the answer was lost; the
		// candidate's own state says which.
		return s.confirm(ctx, scope, r, result)
	}
	return s.confirm(ctx, scope, r, result)
}

// confirm reads back whether adoption committed, without sending it again.
func (s *Service) confirm(ctx context.Context, scope store.AccountScope, r Render, result AgentCompressResult) (AgentCompressResult, error) {
	got, err := s.Get(context.WithoutCancel(ctx), scope, r.ID)
	if err == nil && got.State == "applied" {
		return applied(result, got), nil
	}
	result.State, result.Reason = AgentUnconfirmed, "已提交采用，但还没确认是否生效；请稍后读取画布核实，不要重复压缩"
	if err == nil && !got.active() {
		result.State, result.Reason = AgentNotApplied, "压缩结果没有被采用，图片没有改动"
	}
	return result, nil
}

// abandon discards a candidate this call will not adopt, so it does not keep
// the node busy for the photographer.
func (s *Service) abandon(ctx context.Context, scope store.AccountScope, r Render, result AgentCompressResult, reason string) (AgentCompressResult, error) {
	got, err := s.Discard(context.WithoutCancel(ctx), scope, r.ID)
	if err == nil && got.State == "applied" {
		return applied(result, got.effective(time.Now())), nil
	}
	result.State, result.Reason = AgentNotApplied, reason
	return result, nil
}

func (s *Service) discardQuietly(ctx context.Context, scope store.AccountScope, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _ = s.Discard(ctx, scope, id)
}

func applied(result AgentCompressResult, r Render) AgentCompressResult {
	result.State, result.SourceSize = AgentApplied, r.SourceSize
	if r.Output != nil {
		result.OutputMIME, result.OutputSize, result.Width, result.Height = r.Output.Mime, r.Output.Size, r.Output.Width, r.Output.Height
		if r.SourceSize > 0 {
			result.ReductionPercent = math.Round(float64(r.SourceSize-r.Output.Size)/float64(r.SourceSize)*1000) / 10
		}
	}
	result.VersionID, result.ContentRevisionID = deref(r.AppliedVersionID), deref(r.ResultRevisionID)
	return result
}

func nodeDataRevision(ctx context.Context, scope store.AccountScope, canvasID, nodeID string) (creativeops.Revision, error) {
	canvas, err := creativecanvas.GetCanvas(ctx, scope, canvasID)
	if err != nil {
		return 0, err
	}
	for _, n := range canvas.Nodes {
		if n.ID == nodeID {
			return n.DataRevision, nil
		}
	}
	return 0, ErrSourceChanged
}

func (s *Service) byOperation(ctx context.Context, scope store.AccountScope, operation string) (Render, error) {
	var out Render
	var now time.Time
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		var err error
		if now, err = tx.CreativeNow(ctx); err != nil {
			return err
		}
		out, err = scanRender(tx.QueryRow(ctx, "creative_image_edit_renders", renderColumns, "operation_id=$2", operation))
		return err
	})
	return out.effective(now), err
}

// agentRefusal words the same errors the HTTP API maps to status codes, so
// both callers refuse for the same reasons.
func agentRefusal(err error) error {
	var refusal *Refusal
	switch {
	case errors.As(err, &refusal):
		return err
	case errors.Is(err, ErrSourceChanged):
		return refuse("节点上的图片已经变化，请重新读取画布后再决定是否压缩")
	case errors.As(err, new(BusyError)):
		return refuse("这个节点上有一项尚未完成的图片编辑，等它完成或被取消后再试")
	case errors.Is(err, ErrNodeBusy):
		return refuse("这个节点正在生成，完成后再试")
	case errors.Is(err, ErrBusy):
		return refuse("图片编辑任务过多，请稍后再试")
	case errors.Is(err, creativecanvas.ErrArchived):
		return refuse("项目已归档，恢复后才能修改")
	case errors.Is(err, creativecanvas.ErrNotFound):
		return refuse("节点不存在，请重新读取画布")
	case errors.Is(err, creativecontent.ErrUsageDenied):
		return refuse("这张图片不允许再加工（授权素材或已失效）")
	case errors.Is(err, creativemedia.ErrQuota):
		return refuse("存储空间不足，压缩结果没有采用；释放空间后再试")
	case errors.Is(err, ErrExpired):
		return refuse("压缩结果已过期，图片没有改动")
	case errors.Is(err, creativeops.ErrValidation):
		return refuse("压缩参数不符合要求：质量须是 %d 到 %d 的整数", qualityMin, qualityMax)
	}
	return err
}

func adoptionReason(err error) string {
	var refusal *Refusal
	if errors.As(agentRefusal(err), &refusal) {
		return refusal.Message
	}
	return "压缩结果没有采用，图片没有改动"
}

func failureReason(code string) string {
	switch code {
	case "unsupported":
		return "图片格式不支持压缩（只支持 JPEG、PNG 和静态 WebP）"
	case "colour_unknown":
		return "图片的色彩信息无法可靠识别，没有压缩"
	case "too_large":
		return "图片超出可处理的尺寸或大小上限"
	}
	return "压缩失败，图片没有改动"
}
