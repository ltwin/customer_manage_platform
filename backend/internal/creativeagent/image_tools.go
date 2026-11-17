package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const compressImageTool = "compress_image"

// ImageCompressor is the image edit entry the assistant shares with the
// photographer's panel: same candidate, same adoption command, same refusals.
type ImageCompressor interface {
	// AcceptAgentCompress accepts a candidate on a fixed source; a replay
	// finds the one it already has.
	AcceptAgentCompress(context.Context, store.AccountScope, creativeimageedit.AgentCompress) (string, error)
	// AgentCompressPendingInTx reads and locks a candidate: pending while it
	// is queued or encoding, recheck when it lapses unwritten.
	AgentCompressPendingInTx(ctx context.Context, tx store.TxAccountScope, renderID string) (pending bool, recheck time.Time, err error)
	// ConcludeAgentCompress finishes a candidate that stopped encoding:
	// adopts a ready one if admitted, or discards it when adoption is refused.
	ConcludeAgentCompress(ctx context.Context, scope store.AccountScope, in creativeimageedit.AgentCompress, renderID string, admit creativeimageedit.CompressAdmit) (creativeimageedit.AgentCompressResult, error)
	// DiscardAgentCompressInTx discards a candidate whatever its stage, for a
	// background compress cancelled from its card; one adopted first stays.
	DiscardAgentCompressInTx(ctx context.Context, tx store.TxAccountScope, in creativeimageedit.AgentCompress, renderID string) (creativeimageedit.AgentCompressResult, error)
}

// SetImageCompressor binds image editing at composition time. A deployment
// with editing switched off passes nil and never offers the tool.
func (s *Service) SetImageCompressor(c ImageCompressor) {
	s.compressor = c
}

func (h *runtimeHandler) compressImageInfo() *schema.ToolInfo {
	params := map[string]*schema.ParameterInfo{
		"node_id":             {Type: schema.String, Required: true, Desc: "要压缩的图片节点 ID"},
		"quality":             {Type: schema.Integer, Desc: "输出质量，1 到 100 的整数；高质量 90、均衡 80、小文件 60；省略时为 80。质量值不是保留画质的百分比，也不是缩小比例"},
		"content_revision_id": {Type: schema.String, Desc: "read_canvas 读到的该节点图片的内容修订 ID；给出时，图片在此之后被改动就拒绝，避免压错版本"},
	}
	return &schema.ToolInfo{
		Name:        compressImageTool,
		Desc:        h.offerBackground("按输出质量压缩一张图片节点，像素尺寸不变，结果作为该节点的新版本并切换显示，原图保留在版本列表里，创作者可以撤销。只按质量压缩，不能保证压到指定大小；创作者要求目标大小时如实说明。JPEG 输出 JPEG；不透明 PNG 转 JPEG；透明 PNG 和 WebP 输出 WebP；输出不保留拍摄信息（EXIF/GPS）。不知道要压哪张图、或选中了多张图时先问清楚，不要自行挑选。调用会等待结果：state 为 applied 才表示节点已更新，并带实际的原始大小、输出大小和减少比例；not_reduced 表示文件没有变小、没有改动；not_applied 和 failed 表示没有改动，reason 说明原因；unconfirmed 表示采用已提交但结果未确认，不要重复调用，请读取画布核实。", params),
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}
}

func (h *runtimeHandler) compressImage(ctx context.Context, canvasID, seed, raw string) (any, error) {
	raw, background, err := h.backgroundArgument(ctx, raw)
	if err != nil {
		return nil, err
	}
	in, err := compressInput(canvasID, seed, raw)
	if err != nil {
		return nil, err
	}
	render, err := h.service.compressor.AcceptAgentCompress(ctx, h.scope, in)
	if err != nil {
		return nil, err
	}
	step := toolStepFrom(ctx)
	task := newToolTask{StepID: step, ToolKey: compressImageTool, SourceID: render, Mode: modeForeground, Due: true}
	if background {
		task.Mode = modeBackground
		return h.acceptBackground(ctx, task)
	}
	var pending bool
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		if err := h.registerToolTaskInTx(ctx, tx, task); err != nil {
			return err
		}
		var err error
		pending, _, err = h.service.compressor.AgentCompressPendingInTx(ctx, tx, render)
		return err
	})
	if err != nil {
		return nil, err
	}
	if pending {
		// The finisher adopts the candidate once it is encoded and writes
		// the answer back (agent-tool-background 1.5).
		return nil, errParked{jobID: toolTaskID(step)}
	}
	// Already encoded (a fast encode, or a replay): finish without waiting
	// for the worker, using the same adoption guard and publication path.
	ready := toolTask{ID: toolTaskID(step), CanvasID: canvasID, SourceRunID: h.held.RunID, SourceID: render}
	outcome, err := h.service.concludeCompress(ctx, h.scope, in, ready)
	if err != nil {
		return nil, err
	}
	if err := h.service.finishToolTask(ctx, h.scope, ready.ID, &outcome); err != nil {
		return nil, err
	}
	return json.RawMessage(outcome.Result), nil
}

// compressInput is the call's arguments as the image edit entry takes them.
func compressInput(canvasID, seed, raw string) (creativeimageedit.AgentCompress, error) {
	var args struct {
		NodeID            string `json:"node_id"`
		Quality           *int   `json:"quality"`
		ContentRevisionID string `json:"content_revision_id"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return creativeimageedit.AgentCompress{}, err
	}
	in := creativeimageedit.AgentCompress{Seed: seed, CanvasID: canvasID, NodeID: args.NodeID, SourceRevisionID: args.ContentRevisionID}
	if args.Quality != nil {
		if *args.Quality < 1 || *args.Quality > 100 {
			return in, refuseTool("质量须是 1 到 100 的整数")
		}
		in.Quality = *args.Quality
	}
	return in, nil
}

// concludeCompress is the tool answer and task state for a candidate that
// stopped encoding.
func (s *Service) concludeCompress(ctx context.Context, scope store.AccountScope, in creativeimageedit.AgentCompress, task toolTask) (toolTaskOutcome, error) {
	// RunGone classifies a candidate that already ended. The guard decides
	// whether a ready candidate can be adopted using the current task mode.
	adopt := !task.RunGone
	result, err := s.compressor.ConcludeAgentCompress(ctx, scope, in, task.SourceID,
		func(ctx context.Context, tx store.TxAccountScope) (bool, error) {
			allowed, err := s.admitCompressInTx(ctx, tx, task)
			if err == nil {
				adopt = allowed
			}
			return allowed, err
		})
	if err != nil {
		return toolTaskOutcome{}, err
	}
	return compressOutcome(result, adopt)
}

// The project lock serializes mode changes. If the task is still in the
// foreground, hold the source run's admission locks through the image write.
func (s *Service) admitCompressInTx(ctx context.Context, tx store.TxAccountScope, task toolTask) (bool, error) {
	if err := lockTaskProject(ctx, tx, task.CanvasID); err != nil {
		return false, err
	}
	var mode, state string
	err := tx.QueryRow(ctx, "creative_agent_tool_tasks", "mode,state", "id=$2", task.ID).Scan(&mode, &state)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	if err != nil || state != taskRunning {
		return false, err
	}
	if mode == modeBackground {
		return true, nil
	}
	_, err = (sceneWaiter{s: s}).admitRun(ctx, tx, task.SourceRunID)
	if errors.Is(err, creativescene.ErrRunGate) {
		return false, nil
	}
	return err == nil, err
}

// compressOutcome is the tool answer and task state for a candidate's final
// state; a candidate not adopted because the call was stopped or cancelled
// reads cancelled.
func compressOutcome(result creativeimageedit.AgentCompressResult, adopt bool) (toolTaskOutcome, error) {
	raw, err := json.Marshal(struct {
		Success bool `json:"success"`
		creativeimageedit.AgentCompressResult
	}{result.State == creativeimageedit.AgentApplied, result})
	if err != nil {
		return toolTaskOutcome{}, err
	}
	state := taskSucceeded
	switch result.State {
	case creativeimageedit.AgentFailed:
		state = taskFailed
	case creativeimageedit.AgentNotApplied:
		state = taskFailed
		if !adopt {
			state = taskCancelled
		}
	}
	return toolTaskOutcome{Done: true, State: state, Result: string(raw)}, nil
}

// compressionTaskSource finishes compress_image tasks: it waits while the
// candidate encodes, then concludes it outside the finishing transaction,
// because adoption is the panel's own command with its own locks.
type compressionTaskSource struct{ s *Service }

func (c compressionTaskSource) settle(ctx context.Context, tx store.TxAccountScope, task toolTask) (toolTaskOutcome, error) {
	compressor := c.s.compressor
	if compressor == nil {
		return toolTaskOutcome{}, errors.New("creative agent: image editing is not bound")
	}
	pending, recheck, err := compressor.AgentCompressPendingInTx(ctx, tx, task.SourceID)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	if pending {
		return toolTaskOutcome{RecheckAt: recheck}, nil
	}
	raw, err := stepArgs(ctx, tx, task.ToolStepID)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	in, err := compressInput(task.CanvasID, task.SourceRunID+"/"+task.ToolStepID, raw)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	return toolTaskOutcome{Conclude: func(ctx context.Context, scope store.AccountScope) (toolTaskOutcome, error) {
		return c.s.concludeCompress(ctx, scope, in, task)
	}}, nil
}
